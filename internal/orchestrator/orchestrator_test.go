// Package orchestrator_test tests Orchestrator unit behaviour (eligibility,
// fan-out, partial results, deduplication) and integration/concurrency
// scenarios (goroutine safety, CB/limiter interaction, hedging).
//
// REQ-ORCH-001 through REQ-ORCH-006, REQ-HEDGE-003 through REQ-HEDGE-005.
package orchestrator_test

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rechedev9/riskforge/internal/adapter"
	"github.com/rechedev9/riskforge/internal/circuitbreaker"
	"github.com/rechedev9/riskforge/internal/domain"
	"github.com/rechedev9/riskforge/internal/orchestrator"
	"github.com/rechedev9/riskforge/internal/ratelimiter"
	"github.com/rechedev9/riskforge/internal/testutil"
)

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

func makeCarrier(id string, capabilities []domain.CoverageLine, cfg domain.CarrierConfig) domain.Carrier {
	return domain.Carrier{
		ID:           id,
		Name:         id,
		Capabilities: capabilities,
		Config:       cfg,
	}
}

func defaultCfg() domain.CarrierConfig {
	return domain.CarrierConfig{
		TimeoutHint:           100 * time.Millisecond,
		OpenTimeout:           30 * time.Second,
		FailureThreshold:      5,
		SuccessThreshold:      2,
		HedgeMultiplier:       1.5,
		EMAAlpha:              0.1,
		EMAWarmupObservations: 10,
		RateLimit:             domain.RateLimitConfig{TokensPerSecond: 100, Burst: 100},
		Priority:              1,
	}
}

func preWarmedCfg() domain.CarrierConfig {
	cfg := defaultCfg()
	cfg.EMAWarmupObservations = 0
	return cfg
}

type orchestratorFixture struct {
	carriers []domain.Carrier
	registry *adapter.Registry
	breakers map[string]*circuitbreaker.Breaker
	limiters map[string]*ratelimiter.Limiter
	trackers map[string]*orchestrator.EMATracker
	metrics  *testutil.NoopRecorder
}

func newFixture(t *testing.T, carriers []domain.Carrier) *orchestratorFixture {
	t.Helper()
	fix := &orchestratorFixture{
		carriers: carriers,
		registry: adapter.NewRegistry(),
		breakers: make(map[string]*circuitbreaker.Breaker),
		limiters: make(map[string]*ratelimiter.Limiter),
		trackers: make(map[string]*orchestrator.EMATracker),
		metrics:  testutil.NewNoopRecorder(),
	}
	for _, c := range carriers {
		mc := adapter.NewMockCarrier(c.ID, adapter.MockConfig{
			BaseLatency: 10 * time.Millisecond,
			JitterMs:    0,
			FailureRate: 0.0,
		}, discardLog)
		fix.registry.Register(c.ID, adapter.RegisterMockCarrier(mc))

		cbCfg := circuitbreaker.Config{
			FailureThreshold: c.Config.FailureThreshold,
			SuccessThreshold: c.Config.SuccessThreshold,
			OpenTimeout:      c.Config.OpenTimeout,
		}
		fix.breakers[c.ID] = circuitbreaker.New(c.ID, cbCfg, fix.metrics)
		fix.limiters[c.ID] = ratelimiter.New(c.ID, c.Config.RateLimit, fix.metrics)
		fix.trackers[c.ID] = orchestrator.NewEMATracker(c.ID, c.Config.TimeoutHint, c.Config, fix.metrics)
	}
	return fix
}

func (f *orchestratorFixture) build(t *testing.T) *orchestrator.Orchestrator {
	t.Helper()
	return orchestrator.New(orchestrator.OrchestratorConfig{
		Carriers: f.carriers,
		Registry: f.registry,
		Breakers: f.breakers,
		Limiters: f.limiters,
		Trackers: f.trackers,
		Metrics:  f.metrics,
		Cfg:      orchestrator.Config{HedgePollInterval: 5 * time.Millisecond},
		Log:      discardLog,
		Repo:     nil,
	})
}

func carrierIDs(results []domain.QuoteResult) []string {
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.CarrierID
	}
	return ids
}

func TestOrchestrator_IneligibleCarrierExcluded(t *testing.T) {
	t.Parallel()

	alpha := makeCarrier("alpha", []domain.CoverageLine{domain.CoverageLineAuto}, defaultCfg())
	gamma := makeCarrier("gamma", []domain.CoverageLine{"life"}, defaultCfg())

	fix := newFixture(t, []domain.Carrier{alpha, gamma})
	orch := fix.build(t)

	req := domain.QuoteRequest{
		RequestID:     "unit-01",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
		Timeout:       500 * time.Millisecond,
	}
	results, err := orch.GetQuotes(t.Context(), req)
	if err != nil {
		t.Fatalf("REQ-ORCH-001: unexpected error: %v", err)
	}
	for _, r := range results {
		if r.CarrierID == "gamma" {
			t.Fatal("REQ-ORCH-001: gamma (life-only) appeared in auto results")
		}
	}
	if len(results) != 1 || results[0].CarrierID != "alpha" {
		t.Fatalf("REQ-ORCH-001: expected [alpha], got %v", carrierIDs(results))
	}
}

func TestOrchestrator_NoMatchingCarriers_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	gamma := makeCarrier("gamma", []domain.CoverageLine{"life"}, defaultCfg())
	fix := newFixture(t, []domain.Carrier{gamma})
	orch := fix.build(t)

	req := domain.QuoteRequest{
		RequestID:     "unit-02",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
		Timeout:       500 * time.Millisecond,
	}
	results, err := orch.GetQuotes(t.Context(), req)
	if err != nil {
		t.Fatalf("REQ-ORCH-001: expected nil error, got %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("REQ-ORCH-001: expected empty results, got %d", len(results))
	}
}

func TestOrchestrator_ResultsSortedByPremiumAscending(t *testing.T) {
	t.Parallel()

	alpha := makeCarrier("alpha", []domain.CoverageLine{domain.CoverageLineAuto}, defaultCfg())
	beta := makeCarrier("beta", []domain.CoverageLine{domain.CoverageLineAuto}, defaultCfg())

	fix := newFixture(t, []domain.Carrier{alpha, beta})
	orch := fix.build(t)

	req := domain.QuoteRequest{
		RequestID:     "unit-03",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
		Timeout:       500 * time.Millisecond,
	}
	results, err := orch.GetQuotes(t.Context(), req)
	if err != nil {
		t.Fatalf("REQ-ORCH-004: unexpected error: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("REQ-ORCH-004: expected ≥2 results, got %d", len(results))
	}
	for i := 1; i < len(results); i++ {
		if results[i].Premium.Amount < results[i-1].Premium.Amount {
			t.Fatalf("REQ-ORCH-004: results not sorted ascending at index %d: %d > %d",
				i, results[i-1].Premium.Amount, results[i].Premium.Amount)
		}
	}
}

func TestOrchestrator_DuplicateCarrierResultsDeduplicated(t *testing.T) {
	t.Parallel()

	alpha := makeCarrier("alpha", []domain.CoverageLine{domain.CoverageLineAuto}, preWarmedCfg())
	fix := newFixture(t, []domain.Carrier{alpha})
	orch := fix.build(t)

	req := domain.QuoteRequest{
		RequestID:     "unit-04",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
		Timeout:       500 * time.Millisecond,
	}
	results, err := orch.GetQuotes(t.Context(), req)
	if err != nil {
		t.Fatalf("REQ-ORCH-004: unexpected error: %v", err)
	}
	seen := make(map[string]int)
	for _, r := range results {
		seen[r.CarrierID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("REQ-ORCH-004: carrier %q appeared %d times — dedup failed", id, count)
		}
	}
}

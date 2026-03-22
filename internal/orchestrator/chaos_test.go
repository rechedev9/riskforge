package orchestrator_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rechedev9/riskforge/internal/adapter"
	"github.com/rechedev9/riskforge/internal/circuitbreaker"
	"github.com/rechedev9/riskforge/internal/domain"
	"github.com/rechedev9/riskforge/internal/orchestrator"
	"github.com/rechedev9/riskforge/internal/ratelimiter"
	"github.com/rechedev9/riskforge/internal/testutil"
)

// --- helpers -----------------------------------------------------------------

// chaosFixture builds an orchestratorFixture with custom MockConfig per carrier.
func chaosFixture(t *testing.T, specs []chaosCarrierSpec) *orchestratorFixture {
	t.Helper()
	carriers := make([]domain.Carrier, len(specs))
	for i, s := range specs {
		carriers[i] = s.carrier
	}

	fix := &orchestratorFixture{
		carriers: carriers,
		registry: adapter.NewRegistry(),
		breakers: make(map[string]*circuitbreaker.Breaker),
		limiters: make(map[string]*ratelimiter.Limiter),
		trackers: make(map[string]*orchestrator.EMATracker),
		metrics:  testutil.NewNoopRecorder(),
	}

	for _, s := range specs {
		c := s.carrier
		mc := adapter.NewMockCarrier(c.ID, s.mockCfg, discardLog)
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

type chaosCarrierSpec struct {
	carrier domain.Carrier
	mockCfg adapter.MockConfig
}

// --- chaos tests -------------------------------------------------------------

// TestChaos_AllCarriersFail: all 3 carriers return errors simultaneously.
// Orchestrator must not panic, returns empty results.
func TestChaos_AllCarriersFail(t *testing.T) {
	t.Parallel()

	specs := make([]chaosCarrierSpec, 3)
	for i := range specs {
		id := fmt.Sprintf("fail-%d", i)
		specs[i] = chaosCarrierSpec{
			carrier: makeCarrier(id, []domain.CoverageLine{domain.CoverageLineAuto}, defaultCfg()),
			mockCfg: adapter.MockConfig{
				BaseLatency: 5 * time.Millisecond,
				JitterMs:    0,
				FailureRate: 1.0, // always fail
			},
		}
	}

	fix := chaosFixture(t, specs)
	orch := fix.build(t)

	req := domain.QuoteRequest{
		RequestID:     "chaos-all-fail",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
		Timeout:       500 * time.Millisecond,
	}

	results, err := orch.GetQuotes(t.Context(), req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results when all carriers fail, got %d", len(results))
	}
}

// TestChaos_CarriersTimeout: all carriers take longer than the request timeout.
// Must return partial or empty results without hanging.
func TestChaos_CarriersTimeout(t *testing.T) {
	t.Parallel()

	specs := make([]chaosCarrierSpec, 3)
	for i := range specs {
		id := fmt.Sprintf("slow-%d", i)
		specs[i] = chaosCarrierSpec{
			carrier: makeCarrier(id, []domain.CoverageLine{domain.CoverageLineAuto}, defaultCfg()),
			mockCfg: adapter.MockConfig{
				BaseLatency: 5 * time.Second, // well over the request timeout
				JitterMs:    0,
				FailureRate: 0.0,
			},
		}
	}

	fix := chaosFixture(t, specs)
	orch := fix.build(t)

	req := domain.QuoteRequest{
		RequestID:     "chaos-timeout",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
		Timeout:       200 * time.Millisecond,
	}

	start := time.Now()
	results, err := orch.GetQuotes(t.Context(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// All carriers timed out; expect empty results.
	if len(results) != 0 {
		t.Fatalf("expected 0 results when all carriers time out, got %d", len(results))
	}
	// Must not hang — should return within a reasonable margin of the timeout.
	if elapsed > 2*time.Second {
		t.Fatalf("GetQuotes hung: elapsed %v, want <2s", elapsed)
	}
}

// TestChaos_RapidCBTransitions: 50 goroutines rapidly alternate success/failure
// on a shared orchestrator. Circuit breakers must not race.
func TestChaos_RapidCBTransitions(t *testing.T) {
	t.Parallel()

	// Single carrier with a very low failure threshold so the CB toggles rapidly.
	cfg := defaultCfg()
	cfg.FailureThreshold = 2
	cfg.SuccessThreshold = 1
	cfg.OpenTimeout = 50 * time.Millisecond // fast recovery to HalfOpen

	specs := []chaosCarrierSpec{
		{
			carrier: makeCarrier("flipper", []domain.CoverageLine{domain.CoverageLineAuto}, cfg),
			mockCfg: adapter.MockConfig{
				BaseLatency: 1 * time.Millisecond,
				JitterMs:    0,
				FailureRate: 0.5, // 50% failure to create CB oscillation
			},
		},
	}

	fix := chaosFixture(t, specs)
	orch := fix.build(t)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	ctx := t.Context()
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			for j := range 20 {
				req := domain.QuoteRequest{
					RequestID:     fmt.Sprintf("cb-flip-%d-%d", i, j),
					CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
					Timeout:       200 * time.Millisecond,
				}
				_, _ = orch.GetQuotes(ctx, req)
			}
		}(i)
	}
	wg.Wait()
	// If we get here without a race detector failure or panic, the test passes.
}

// TestChaos_ConcurrentMixedRequests: 100 goroutines send requests with different
// coverage lines — some valid, some that match no carrier. No panics, no races.
func TestChaos_ConcurrentMixedRequests(t *testing.T) {
	t.Parallel()

	specs := []chaosCarrierSpec{
		{
			carrier: makeCarrier("auto-only", []domain.CoverageLine{domain.CoverageLineAuto}, defaultCfg()),
			mockCfg: adapter.MockConfig{BaseLatency: 5 * time.Millisecond, FailureRate: 0.1},
		},
		{
			carrier: makeCarrier("home-only", []domain.CoverageLine{domain.CoverageLineHomeowners}, defaultCfg()),
			mockCfg: adapter.MockConfig{BaseLatency: 10 * time.Millisecond, FailureRate: 0.2},
		},
		{
			carrier: makeCarrier("umbrella-only", []domain.CoverageLine{domain.CoverageLineUmbrella}, defaultCfg()),
			mockCfg: adapter.MockConfig{BaseLatency: 15 * time.Millisecond, FailureRate: 0.0},
		},
	}

	fix := chaosFixture(t, specs)
	orch := fix.build(t)

	// Mix of valid and non-matching coverage lines.
	coverageSets := [][]domain.CoverageLine{
		{domain.CoverageLineAuto},
		{domain.CoverageLineHomeowners},
		{domain.CoverageLineUmbrella},
		{domain.CoverageLineAuto, domain.CoverageLineHomeowners},
		{domain.CoverageLineAuto, domain.CoverageLineHomeowners, domain.CoverageLineUmbrella},
		{"nonexistent"},
	}

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	ctx := t.Context()
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			lines := coverageSets[i%len(coverageSets)]
			req := domain.QuoteRequest{
				RequestID:     fmt.Sprintf("mixed-%d", i),
				CoverageLines: lines,
				Timeout:       300 * time.Millisecond,
			}
			results, err := orch.GetQuotes(ctx, req)
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", i, err)
				return
			}
			// Results must have no duplicate carrier IDs.
			seen := make(map[string]bool, len(results))
			for _, r := range results {
				if seen[r.CarrierID] {
					t.Errorf("goroutine %d: duplicate carrier_id %q", i, r.CarrierID)
				}
				seen[r.CarrierID] = true
			}
		}(i)
	}
	wg.Wait()
}

// TestChaos_ContextCancelDuringFanOut: 20 goroutines cancel context mid-fan-out.
// Must not leak goroutines or panic.
func TestChaos_ContextCancelDuringFanOut(t *testing.T) {
	t.Parallel()

	// Use moderately slow carriers so cancellation happens mid-flight.
	specs := []chaosCarrierSpec{
		{
			carrier: makeCarrier("mid-a", []domain.CoverageLine{domain.CoverageLineAuto}, defaultCfg()),
			mockCfg: adapter.MockConfig{BaseLatency: 100 * time.Millisecond, FailureRate: 0.0},
		},
		{
			carrier: makeCarrier("mid-b", []domain.CoverageLine{domain.CoverageLineAuto}, defaultCfg()),
			mockCfg: adapter.MockConfig{BaseLatency: 150 * time.Millisecond, FailureRate: 0.0},
		},
		{
			carrier: makeCarrier("mid-c", []domain.CoverageLine{domain.CoverageLineAuto}, defaultCfg()),
			mockCfg: adapter.MockConfig{BaseLatency: 200 * time.Millisecond, FailureRate: 0.0},
		},
	}

	fix := chaosFixture(t, specs)
	orch := fix.build(t)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(t.Context())
			// Cancel after a short random-ish delay (10-50ms) while carriers are still working.
			go func() {
				time.Sleep(time.Duration(10+i*2) * time.Millisecond)
				cancel()
			}()

			req := domain.QuoteRequest{
				RequestID:     fmt.Sprintf("cancel-%d", i),
				CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
				Timeout:       500 * time.Millisecond,
			}
			// We don't care about the result — just that it doesn't panic.
			_, _ = orch.GetQuotes(ctx, req)
		}(i)
	}
	wg.Wait()

	// Allow goroutines spawned by the orchestrator to drain.
	time.Sleep(100 * time.Millisecond)
}

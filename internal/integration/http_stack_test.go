package integration_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rechedev9/riskforge/internal/adapter"
	"github.com/rechedev9/riskforge/internal/circuitbreaker"
	"github.com/rechedev9/riskforge/internal/domain"
	"github.com/rechedev9/riskforge/internal/handler"
	"github.com/rechedev9/riskforge/internal/middleware"
	"github.com/rechedev9/riskforge/internal/orchestrator"
	"github.com/rechedev9/riskforge/internal/ratelimiter"
	"github.com/rechedev9/riskforge/internal/testutil"
)

const testAPIKey = "test-key-abc"

// quoteResponseBody mirrors handler's unexported quoteResponse.
type quoteResponseBody struct {
	RequestID  string      `json:"request_id"`
	Quotes     []quoteItem `json:"quotes"`
	DurationMs int64       `json:"duration_ms"`
}

// quoteItem mirrors handler's unexported quoteItem.
type quoteItem struct {
	CarrierID    string `json:"carrier_id"`
	PremiumCents int64  `json:"premium_cents"`
	Currency     string `json:"currency"`
	IsHedged     bool   `json:"is_hedged"`
	LatencyMs    int64  `json:"latency_ms"`
}

// newTestStack wires the full production middleware chain with mock carriers
// and returns a running httptest.Server URL and the valid API key.
func newTestStack(t *testing.T, concurrencyLimit int) (string, string) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := testutil.NewNoopRecorder()
	reg := prometheus.NewRegistry()
	registry := adapter.NewRegistry()
	breakers := make(map[string]*circuitbreaker.Breaker)
	limiters := make(map[string]*ratelimiter.Limiter)
	trackers := make(map[string]*orchestrator.EMATracker)

	carriers := []domain.Carrier{
		{
			ID: "alpha", Name: "Alpha",
			Capabilities: []domain.CoverageLine{domain.CoverageLineAuto, domain.CoverageLineHomeowners},
			Config: domain.CarrierConfig{
				TimeoutHint: 50 * time.Millisecond, FailureThreshold: 5, SuccessThreshold: 2,
				OpenTimeout: 30 * time.Second, HedgeMultiplier: 1.5, EMAWarmupObservations: 10,
				RateLimit: domain.RateLimitConfig{TokensPerSecond: 100, Burst: 10},
			},
		},
		{
			ID: "beta", Name: "Beta",
			Capabilities: []domain.CoverageLine{domain.CoverageLineAuto},
			Config: domain.CarrierConfig{
				TimeoutHint: 50 * time.Millisecond, FailureThreshold: 5, SuccessThreshold: 2,
				OpenTimeout: 30 * time.Second, HedgeMultiplier: 1.5, EMAWarmupObservations: 10,
				RateLimit: domain.RateLimitConfig{TokensPerSecond: 100, Burst: 10},
			},
		},
	}

	for _, c := range carriers {
		mock := adapter.NewMockCarrier(c.ID, adapter.MockConfig{
			BaseLatency: 10 * time.Millisecond, JitterMs: 0, FailureRate: 0.0,
		}, log)
		registry.Register(c.ID, adapter.RegisterMockCarrier(mock))
		breakers[c.ID] = circuitbreaker.New(c.ID, circuitbreaker.Config{
			FailureThreshold: c.Config.FailureThreshold,
			SuccessThreshold: c.Config.SuccessThreshold,
			OpenTimeout:      c.Config.OpenTimeout,
		}, rec)
		limiters[c.ID] = ratelimiter.New(c.ID, c.Config.RateLimit, rec)
		trackers[c.ID] = orchestrator.NewEMATracker(c.ID, c.Config.TimeoutHint, c.Config, rec)
	}

	orch := orchestrator.New(orchestrator.OrchestratorConfig{
		Carriers: carriers, Registry: registry, Breakers: breakers,
		Limiters: limiters, Trackers: trackers, Metrics: rec,
		Cfg: orchestrator.Config{}, Log: log, Repo: nil,
	})
	h := handler.New(handler.HandlerConfig{
		Orch: orch, Metrics: rec, Gatherer: reg, Log: log, DB: nil,
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Wire middleware identically to cli.Run lines 178-184.
	var srv http.Handler = mux
	srv = middleware.LimitConcurrency(srv, concurrencyLimit, log)
	authHandler, stopAuth := middleware.RequireAPIKey(
		srv, []string{testAPIKey},
		[]string{"/healthz", "/readyz", "/metrics"}, log,
	)
	srv = authHandler
	srv = middleware.SecurityHeaders(srv)
	srv = middleware.AuditLog(srv, log)

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	t.Cleanup(stopAuth)

	return ts.URL, testAPIKey
}

// validQuoteBody returns a JSON request body with a unique request_id.
func validQuoteBody(id string) string {
	return `{"request_id":"` + id + `","coverage_lines":["auto"],"timeout_ms":5000}`
}

// --- Tests ---

func TestHTTPStack_PostQuotes_HappyPath(t *testing.T) {
	t.Parallel()
	base, key := newTestStack(t, 100)

	body := strings.NewReader(validQuoteBody("happy-1"))
	req, err := http.NewRequest(http.MethodPost, base+"/quotes", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var result quoteResponseBody
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(result.Quotes) < 1 {
		t.Fatalf("expected at least 1 quote, got %d", len(result.Quotes))
	}
	for i, q := range result.Quotes {
		if q.CarrierID == "" {
			t.Errorf("quote[%d].CarrierID is empty", i)
		}
		if q.PremiumCents <= 0 {
			t.Errorf("quote[%d].PremiumCents = %d, want > 0", i, q.PremiumCents)
		}
	}
}

func TestHTTPStack_PostQuotes_RequestIDAndDuration(t *testing.T) {
	t.Parallel()
	base, key := newTestStack(t, 100)

	body := strings.NewReader(validQuoteBody("reqid-42"))
	req, err := http.NewRequest(http.MethodPost, base+"/quotes", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	var result quoteResponseBody
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if result.RequestID != "reqid-42" {
		t.Errorf("request_id = %q, want %q", result.RequestID, "reqid-42")
	}
	if result.DurationMs < 0 {
		t.Errorf("duration_ms = %d, want >= 0", result.DurationMs)
	}
}

func TestHTTPStack_PostQuotes_MissingAuth(t *testing.T) {
	t.Parallel()
	base, _ := newTestStack(t, 100)

	body := strings.NewReader(validQuoteBody("noauth-1"))
	req, err := http.NewRequest(http.MethodPost, base+"/quotes", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "UNAUTHORIZED") {
		t.Errorf("body %q does not contain UNAUTHORIZED", b)
	}
}

func TestHTTPStack_PostQuotes_WrongKey(t *testing.T) {
	t.Parallel()
	base, _ := newTestStack(t, 100)

	body := strings.NewReader(validQuoteBody("wrongkey-1"))
	req, err := http.NewRequest(http.MethodPost, base+"/quotes", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer wrong-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "UNAUTHORIZED") {
		t.Errorf("body %q does not contain UNAUTHORIZED", b)
	}
}

func TestHTTPStack_SecurityHeaders(t *testing.T) {
	t.Parallel()
	base, key := newTestStack(t, 100)

	body := strings.NewReader(validQuoteBody("sec-hdr-1"))
	req, err := http.NewRequest(http.MethodPost, base+"/quotes", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	expected := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":          "DENY",
		"Strict-Transport-Security": "max-age=63072000; includeSubDomains",
		"Cache-Control":             "no-store",
		"Content-Security-Policy":   "default-src 'none'",
	}
	for hdr, want := range expected {
		got := resp.Header.Get(hdr)
		if got != want {
			t.Errorf("%s = %q, want %q", hdr, got, want)
		}
	}
}

func TestHTTPStack_SecurityHeaders_OnError(t *testing.T) {
	t.Parallel()
	base, key := newTestStack(t, 100)

	// Empty body triggers a 400 (INVALID_JSON).
	req, err := http.NewRequest(http.MethodPost, base+"/quotes", strings.NewReader(""))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, b)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q on error, want nosniff", got)
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q on error, want DENY", got)
	}
}

func TestHTTPStack_ConcurrencyLimit_503(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	blockCh := make(chan struct{})
	blockingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh
		w.WriteHeader(http.StatusOK)
	})

	// Wire middleware around the blocking handler with limit=2.
	var srv http.Handler = blockingHandler
	srv = middleware.LimitConcurrency(srv, 2, log)
	authHandler, stopAuth := middleware.RequireAPIKey(
		srv, []string{testAPIKey},
		[]string{"/healthz", "/readyz", "/metrics"}, log,
	)
	srv = authHandler
	srv = middleware.SecurityHeaders(srv)
	srv = middleware.AuditLog(srv, log)

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	t.Cleanup(stopAuth)

	// Fill both concurrency slots.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/quotes", nil)
			req.Header.Set("Authorization", "Bearer "+testAPIKey)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			io.ReadAll(resp.Body)
		}()
	}

	// Let goroutines acquire semaphore slots.
	time.Sleep(50 * time.Millisecond)

	// Overflow request should get 503.
	overflowReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/quotes", nil)
	overflowReq.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err := http.DefaultClient.Do(overflowReq)
	if err != nil {
		t.Fatalf("overflow request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra != "1" {
		t.Errorf("Retry-After = %q, want %q", ra, "1")
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "SERVICE_UNAVAILABLE") {
		t.Errorf("body %q does not contain SERVICE_UNAVAILABLE", b)
	}

	close(blockCh)
	wg.Wait()
}

func TestHTTPStack_ConcurrencyLimit_RecoveryAfterRelease(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	blockCh := make(chan struct{})
	blockingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh
		w.WriteHeader(http.StatusOK)
	})

	var srv http.Handler = blockingHandler
	srv = middleware.LimitConcurrency(srv, 2, log)
	authHandler, stopAuth := middleware.RequireAPIKey(
		srv, []string{testAPIKey},
		[]string{"/healthz", "/readyz", "/metrics"}, log,
	)
	srv = authHandler
	srv = middleware.SecurityHeaders(srv)
	srv = middleware.AuditLog(srv, log)

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	t.Cleanup(stopAuth)

	// Fill both slots.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/quotes", nil)
			req.Header.Set("Authorization", "Bearer "+testAPIKey)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			io.ReadAll(resp.Body)
		}()
	}
	time.Sleep(50 * time.Millisecond)

	// Release blockers.
	close(blockCh)
	wg.Wait()

	// After release, a new request should succeed with 200.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/quotes", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("recovery request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after release, got %d", resp.StatusCode)
	}
}

func TestHTTPStack_HealthzBypassesAuth(t *testing.T) {
	t.Parallel()
	base, _ := newTestStack(t, 100)

	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("get /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "ok" {
		t.Errorf("body = %q, want %q", b, "ok")
	}
}

func TestHTTPStack_ReadyzBypassesAuth(t *testing.T) {
	t.Parallel()
	base, _ := newTestStack(t, 100)

	resp, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatalf("get /readyz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "ok" {
		t.Errorf("body = %q, want %q", b, "ok")
	}
}

func TestHTTPStack_MetricsBypassesAuth(t *testing.T) {
	t.Parallel()
	base, _ := newTestStack(t, 100)

	resp, err := http.Get(base + "/metrics")
	if err != nil {
		t.Fatalf("get /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

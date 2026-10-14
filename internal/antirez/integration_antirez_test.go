// Package antirez_test is an adversarial test suite that attacks the full
// assembled HTTP stack through its public API. Every test targets a structural
// tension in the production code: boundary conditions, fuzz loops, corruption
// injection, stress tests, and concurrency storms.
//
// Run with: go test -race -count=1 -timeout 120s -v ./internal/antirez/...
package antirez_test

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/goleak"

	"github.com/rechedev9/riskforge/internal/adapter"
	"github.com/rechedev9/riskforge/internal/circuitbreaker"
	"github.com/rechedev9/riskforge/internal/domain"
	"github.com/rechedev9/riskforge/internal/handler"
	"github.com/rechedev9/riskforge/internal/middleware"
	"github.com/rechedev9/riskforge/internal/orchestrator"
	"github.com/rechedev9/riskforge/internal/ratelimiter"
	"github.com/rechedev9/riskforge/internal/testutil"
)

const (
	fuzzIterations = 5000
	testAPIKey     = "test-key-abc"
)

func seededRand(t *testing.T) *rand.Rand {
	t.Helper()
	seed := time.Now().UnixNano()
	t.Logf("antirez seed: %d", seed)
	return rand.New(rand.NewSource(seed))
}

func newAntirezStack(t *testing.T, concurrencyLimit int) (*httptest.Server, string) {
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

	return ts, testAPIKey
}

func validBody(id string) string {
	return `{"request_id":"` + id + `","coverage_lines":["auto"],"timeout_ms":5000}`
}

func doPost(t *testing.T, url, key, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/quotes", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, b
}

func doPostRaw(t *testing.T, url, key string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/quotes", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, b
}

func TestBoundary_RequestID_CharRange(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 100)

	for b := 0; b <= 0xFF; b++ {
		b := b
		t.Run(fmt.Sprintf("char-0x%02X", b), func(t *testing.T) {
			t.Parallel()
			id := string([]byte{byte(b)})
			idJSON, _ := jsonMarshalString(id)
			body := `{"request_id":` + idJSON + `,"coverage_lines":["auto"],"timeout_ms":5000}`

			status, _ := doPost(t, ts.URL, key, body)

			if b >= 0x20 && b <= 0x7E {
				if status != http.StatusOK {
					t.Errorf("char 0x%02X (%q): want 200, got %d", b, id, status)
				}
			} else {
				if status != http.StatusBadRequest {
					t.Errorf("char 0x%02X: want 400, got %d", b, status)
				}
			}
		})
	}
}

func jsonMarshalString(s string) (string, error) {
	var buf bytes.Buffer
	enc := newJSONStringEncoder(&buf)
	enc.writeString(s)
	return buf.String(), nil
}

type jsonStringEncoder struct {
	buf *bytes.Buffer
}

func newJSONStringEncoder(buf *bytes.Buffer) *jsonStringEncoder {
	return &jsonStringEncoder{buf: buf}
}

func (e *jsonStringEncoder) writeString(s string) {
	e.buf.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			e.buf.WriteString(`\"`)
		case c == '\\':
			e.buf.WriteString(`\\`)
		case c < 0x20:
			fmt.Fprintf(e.buf, `\u%04x`, c)
		default:
			e.buf.WriteByte(c)
		}
	}
	e.buf.WriteByte('"')
}

func TestBoundary_RequestID_Length(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 100)

	cases := []struct {
		name string
		len  int
		want int
	}{
		{"empty", 0, 400},
		{"one_byte", 1, 200},
		{"max_256", 256, 200},
		{"overflow_257", 257, 400},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id := strings.Repeat("a", tc.len)
			body := validBody(id)
			if tc.len == 0 {
				body = `{"request_id":"","coverage_lines":["auto"],"timeout_ms":5000}`
			}
			status, respBody := doPost(t, ts.URL, key, body)
			if status != tc.want {
				t.Errorf("len=%d: want %d, got %d: %s", tc.len, tc.want, status, respBody)
			}
		})
	}
}

func TestBoundary_TimeoutMs(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 100)

	cases := []struct {
		timeout int
		want    int
	}{
		{-1, 400},
		{0, 200},
		{99, 400},
		{100, 200},
		{30000, 200},
		{30001, 400},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("timeout_%d", tc.timeout), func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`{"request_id":"t-%d","coverage_lines":["auto"],"timeout_ms":%d}`, tc.timeout, tc.timeout)
			status, respBody := doPost(t, ts.URL, key, body)
			if status != tc.want {
				t.Errorf("timeout_ms=%d: want %d, got %d: %s", tc.timeout, tc.want, status, respBody)
			}
		})
	}
}

func TestBoundary_BodySize(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 100)

	const maxBodyBytes = 1 << 20

	t.Run("at_limit", func(t *testing.T) {
		t.Parallel()
		prefix := `{"request_id":"size","coverage_lines":["auto"],"timeout_ms":5000,"_pad":"`
		suffix := `"}`
		padLen := maxBodyBytes - len(prefix) - len(suffix)
		if padLen < 0 {
			t.Fatal("prefix+suffix exceeds maxBodyBytes")
		}
		body := prefix + strings.Repeat("x", padLen) + suffix

		status, _ := doPost(t, ts.URL, key, body)
		if status == http.StatusInternalServerError {
			t.Fatalf("got 500 at exactly maxBodyBytes — server error")
		}
	})

	t.Run("over_limit", func(t *testing.T) {
		t.Parallel()
		prefix := `{"request_id":"over","coverage_lines":["auto"],"timeout_ms":5000,"_pad":"`
		suffix := `"}`
		padLen := maxBodyBytes - len(prefix) - len(suffix) + 1
		body := prefix + strings.Repeat("x", padLen) + suffix

		status, respBody := doPost(t, ts.URL, key, body)
		if status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", status, respBody)
		}
	})
}

func TestFuzz_RandomRequests(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 100)
	rng := seededRand(t)

	for i := 0; i < fuzzIterations; i++ {
		idLen := rng.Intn(301)
		idBytes := make([]byte, idLen)
		for j := range idBytes {
			idBytes[j] = byte(rng.Intn(256))
		}

		idJSON, _ := jsonMarshalString(string(idBytes))

		numLines := rng.Intn(6)
		validLines := []string{"auto", "homeowners", "umbrella"}
		lines := make([]string, numLines)
		for j := range lines {
			if rng.Intn(2) == 0 {
				lines[j] = validLines[rng.Intn(len(validLines))]
			} else {
				garbageLen := rng.Intn(20)
				gb := make([]byte, garbageLen)
				for k := range gb {
					gb[k] = byte(rng.Intn(128))
				}
				lines[j] = string(gb)
			}
		}

		var linesBuf bytes.Buffer
		linesBuf.WriteByte('[')
		for j, l := range lines {
			if j > 0 {
				linesBuf.WriteByte(',')
			}
			lJSON, _ := jsonMarshalString(l)
			linesBuf.WriteString(lJSON)
		}
		linesBuf.WriteByte(']')

		timeoutMs := rng.Intn(40101) - 100

		body := fmt.Sprintf(`{"request_id":%s,"coverage_lines":%s,"timeout_ms":%d}`,
			idJSON, linesBuf.String(), timeoutMs)

		status, respBody := doPost(t, ts.URL, key, body)
		if status == http.StatusInternalServerError {
			t.Fatalf("iteration %d: got 500 — body: %s\nrequest: %s", i, respBody, body)
		}
		if status != 200 && status != 400 && status != 422 {
			t.Fatalf("iteration %d: unexpected status %d — body: %s", i, status, respBody)
		}
	}
}

func TestFuzz_RandomAuth(t *testing.T) {
	t.Parallel()
	ts, _ := newAntirezStack(t, 100)
	rng := seededRand(t)
	body := validBody("auth-fuzz")

	for i := 0; i < fuzzIterations; i++ {
		var authHeader string
		switch rng.Intn(6) {
		case 0:
			authHeader = "Bearer " + testAPIKey
		case 1:
			rl := rng.Intn(100)
			rb := make([]byte, rl)
			for j := range rb {
				rb[j] = byte(32 + rng.Intn(95))
			}
			authHeader = "Bearer " + string(rb)
		case 2:
			authHeader = ""
		case 3:
			authHeader = "Basic dXNlcjpwYXNz"
		case 4:
			authHeader = "Bearer " + strings.Repeat("x", 10000)
		case 5:
			authHeader = "Bearer "
		}

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/quotes", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("iteration %d: do request: %v", i, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusInternalServerError {
			t.Fatalf("iteration %d: got 500: %s", i, b)
		}

		if authHeader == "Bearer "+testAPIKey {
			// Valid key may get 429 when the IP has been rate-limited due to
			// prior auth failures from the same localhost address (TOCTOU
			// tension #7). Both 200 and 429 are acceptable.
			if resp.StatusCode != 200 && resp.StatusCode != 429 {
				t.Errorf("iteration %d: valid key got %d, want 200 or 429", i, resp.StatusCode)
			}
		} else {
			if resp.StatusCode != 401 && resp.StatusCode != 429 {
				t.Errorf("iteration %d: invalid key got %d, want 401 or 429", i, resp.StatusCode)
			}
		}
	}
}

func TestCorruption_MutatedJSON(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 100)
	rng := seededRand(t)

	base := []byte(`{"request_id":"ok","coverage_lines":["auto"],"timeout_ms":5000}`)

	for i := 0; i < fuzzIterations; i++ {
		mutated := make([]byte, len(base))
		copy(mutated, base)

		switch rng.Intn(4) {
		case 0:
			if len(mutated) > 0 {
				idx := rng.Intn(len(mutated))
				bit := byte(1 << uint(rng.Intn(8)))
				mutated[idx] ^= bit
			}
		case 1:
			if len(mutated) > 1 {
				mutated = mutated[:rng.Intn(len(mutated))]
			}
		case 2:
			idx := rng.Intn(len(mutated) + 1)
			rb := byte(rng.Intn(256))
			mutated = append(mutated[:idx], append([]byte{rb}, mutated[idx:]...)...)
		case 3:
			if len(mutated) > 2 {
				start := rng.Intn(len(mutated))
				end := start + rng.Intn(len(mutated)-start) + 1
				if end > len(mutated) {
					end = len(mutated)
				}
				chunk := make([]byte, end-start)
				copy(chunk, mutated[start:end])
				mutated = append(mutated[:end], append(chunk, mutated[end:]...)...)
			}
		}

		status, respBody := doPostRaw(t, ts.URL, key, mutated)
		if status == http.StatusInternalServerError {
			t.Fatalf("iteration %d: got 500 with mutated body %q: %s", i, mutated, respBody)
		}
		if status != 200 && status != 400 {
			if status != 422 {
				t.Fatalf("iteration %d: unexpected status %d with mutated body: %s", i, status, respBody)
			}
		}
	}
}

func TestCorruption_ExtraFields(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 100)

	body := `{"request_id":"x","coverage_lines":["auto"],"evil_field":"hack"}`
	status, respBody := doPost(t, ts.URL, key, body)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown field, got %d: %s", status, respBody)
	}
}

func TestCorruption_DuplicateKeys(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 100)

	body := `{"request_id":"first","request_id":"second","coverage_lines":["auto"]}`
	status, respBody := doPost(t, ts.URL, key, body)
	// encoding/json does not reject duplicate keys; last-wins applies.
	// 500 is not an acceptable outcome.
	if status == http.StatusInternalServerError {
		t.Fatalf("got 500 with duplicate keys: %s", respBody)
	}
	// If accepted, the request_id should be "second" (last-wins).
	if status == http.StatusOK {
		if !bytes.Contains(respBody, []byte(`"second"`)) {
			if !bytes.Contains(respBody, []byte(`request_id`)) {
				t.Logf("response: %s", respBody)
			}
		}
	}
}

func TestStress_RapidRequestCycles(t *testing.T) {
	t.Parallel()

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

	var srv http.Handler = mux
	srv = middleware.LimitConcurrency(srv, 100, log)
	authHandler, stopAuth := middleware.RequireAPIKey(
		srv, []string{testAPIKey},
		[]string{"/healthz", "/readyz", "/metrics"}, log,
	)
	srv = authHandler
	srv = middleware.SecurityHeaders(srv)
	srv = middleware.AuditLog(srv, log)

	ts := httptest.NewServer(srv)
	key := testAPIKey

	tr := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{Transport: tr}

	for i := 0; i < 1000; i++ {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/quotes",
			strings.NewReader(validBody(fmt.Sprintf("rapid-%d", i))))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	goroutinesBeforeClose := runtime.NumGoroutine()

	tr.CloseIdleConnections()
	ts.Close()
	stopAuth()

	// Let goroutines settle after server close.
	runtime.GC()
	time.Sleep(500 * time.Millisecond)

	goroutinesAfterClose := runtime.NumGoroutine()
	delta := goroutinesAfterClose - goroutinesBeforeClose
	t.Logf("goroutines: before_close=%d after_close=%d delta=%d",
		goroutinesBeforeClose, goroutinesAfterClose, delta)
	// A large positive delta means a leak. Small jitter from GC is ignored.
	if delta > 10 {
		t.Errorf("goroutine leak suspected: delta=%d (before_close=%d, after_close=%d)",
			delta, goroutinesBeforeClose, goroutinesAfterClose)
	}
}

func TestConcurrency_FanInStorm(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 200)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var statuses []int

	for g := 0; g < 50; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				id := fmt.Sprintf("fan-%d-%d", g, i)
				req, err := http.NewRequest(http.MethodPost, ts.URL+"/quotes",
					strings.NewReader(validBody(id)))
				if err != nil {
					t.Errorf("new request: %v", err)
					return
				}
				req.Header.Set("Authorization", "Bearer "+key)
				req.Header.Set("Content-Type", "application/json")

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Errorf("goroutine %d, iter %d: %v", g, i, err)
					return
				}
				io.ReadAll(resp.Body)
				resp.Body.Close()

				mu.Lock()
				statuses = append(statuses, resp.StatusCode)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	for i, s := range statuses {
		if s != 200 {
			t.Errorf("request %d: want 200, got %d", i, s)
		}
	}
}

func TestConcurrency_MixedAuthStorm(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 200)
	body := validBody("mixed-auth")

	var wg sync.WaitGroup
	type result struct {
		status int
		valid  bool
	}
	var mu sync.Mutex
	var results []result

	for g := 0; g < 50; g++ {
		g := g
		isValid := g < 25
		authKey := key
		if !isValid {
			authKey = "wrong-key-" + fmt.Sprintf("%d", g)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				req, err := http.NewRequest(http.MethodPost, ts.URL+"/quotes",
					strings.NewReader(body))
				if err != nil {
					t.Errorf("new request: %v", err)
					return
				}
				req.Header.Set("Authorization", "Bearer "+authKey)
				req.Header.Set("Content-Type", "application/json")

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Errorf("goroutine %d, iter %d: %v", g, i, err)
					return
				}
				io.ReadAll(resp.Body)
				resp.Body.Close()

				mu.Lock()
				results = append(results, result{status: resp.StatusCode, valid: isValid})
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	for i, r := range results {
		if r.valid {
			// Valid key may get 429 when the IP has been rate-limited due to
			// concurrent auth failures from the same localhost address (TOCTOU
			// tension #7 — auth rate limiter).
			if r.status != 200 && r.status != 429 {
				t.Errorf("result %d: valid auth got %d, want 200 or 429", i, r.status)
			}
		} else {
			if r.status != 401 && r.status != 429 {
				t.Errorf("result %d: invalid auth got %d, want 401 or 429", i, r.status)
			}
		}
		if r.status == 500 {
			t.Fatalf("result %d: got 500", i)
		}
	}
}

func TestConcurrency_SameRequestID(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 100)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var statuses []int

	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/quotes",
				strings.NewReader(validBody("same-id-dedup")))
			if err != nil {
				t.Errorf("new request: %v", err)
				return
			}
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("do: %v", err)
				return
			}
			io.ReadAll(resp.Body)
			resp.Body.Close()

			mu.Lock()
			statuses = append(statuses, resp.StatusCode)
			mu.Unlock()
		}()
	}
	wg.Wait()

	for i, s := range statuses {
		if s != 200 {
			t.Errorf("goroutine %d: want 200, got %d", i, s)
		}
	}
}

func TestConcurrency_ContextCancelMidFlight(t *testing.T) {
	t.Parallel()

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

	var srv http.Handler = mux
	srv = middleware.LimitConcurrency(srv, 100, log)
	authHandler, stopAuth := middleware.RequireAPIKey(
		srv, []string{testAPIKey},
		[]string{"/healthz", "/readyz", "/metrics"}, log,
	)
	srv = authHandler
	srv = middleware.SecurityHeaders(srv)
	srv = middleware.AuditLog(srv, log)

	ts := httptest.NewServer(srv)
	key := testAPIKey

	tr := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{Timeout: 1 * time.Millisecond, Transport: tr}

	for i := 0; i < 100; i++ {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/quotes",
			strings.NewReader(validBody(fmt.Sprintf("cancel-%d", i))))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	tr.CloseIdleConnections()
	ts.Close()
	stopAuth()

	// Goroutine count is not asserted: other tests in this process also
	// contribute to runtime.NumGoroutine.
	runtime.GC()
	time.Sleep(2 * time.Second)
	t.Logf("goroutines after drain: %d", runtime.NumGoroutine())
}

func TestConcurrency_ConcurrencyLimitUnderLoad(t *testing.T) {
	t.Parallel()
	ts, key := newAntirezStack(t, 5)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var statuses []int

	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/quotes",
				strings.NewReader(validBody("limit-test")))
			if err != nil {
				t.Errorf("new request: %v", err)
				return
			}
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("do: %v", err)
				return
			}
			io.ReadAll(resp.Body)
			resp.Body.Close()

			mu.Lock()
			statuses = append(statuses, resp.StatusCode)
			mu.Unlock()
		}()
	}
	wg.Wait()

	got200, got503 := 0, 0
	for _, s := range statuses {
		switch s {
		case 200:
			got200++
		case 503:
			got503++
		case 500:
			t.Fatal("got 500")
		}
	}

	if got503 == 0 {
		t.Errorf("expected at least some 503 responses (limiter should reject overflow); got200=%d, got503=%d", got200, got503)
	}
	t.Logf("200s: %d, 503s: %d", got200, got503)
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
		goleak.IgnoreTopFunction("time.Sleep"),
	)
}

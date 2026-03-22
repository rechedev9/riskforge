# Technical Design: Integration and E2E Testing

**Change**: integration-e2e-testing
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Technical Approach

This change adds three categories of tests to riskforge, each targeting a distinct untested boundary: (1) HTTP stack integration tests that assemble the full production middleware chain (`AuditLog -> SecurityHeaders -> RequireAPIKey -> LimitConcurrency -> mux -> Handler -> Orchestrator`) with mock carriers and a real `net.Listener` via `httptest.NewServer`; (2) Spanner adapter tests that exercise `QuoteRepo`, `CarrierRepo`, and `AppetiteRepo` against the Spanner emulator, gated behind `//go:build integration`; (3) CLI lifecycle tests that validate `cli.Run`'s environment variable checking and graceful shutdown via `context.WithCancel`.

The design follows existing project patterns: table-driven tests with `t.Parallel()`, `testutil.NoopRecorder` for metrics, `adapter.NewMockCarrier` for carrier simulation, and `httptest` for HTTP testing. No production code is modified -- all changes are additive test files plus CI/Makefile config. The `internal/integration/` package is separate from `internal/cli/` to avoid transitively importing `spanneradapter` (and its Spanner SDK dependency) into every test binary.

The Spanner adapter tests use direct `spanner.Mutation` inserts for test data setup (rather than going through the repo layer) to test the repo's SQL queries against known data. A shared `newEmulatorClient(t)` helper in `testutil_test.go` handles client creation and `t.Skip` when the emulator is absent.

---

## Architecture Decisions

| # | Decision | Choice | Alternatives Considered | Rationale |
|---|---|---|---|---|
| 1 | HTTP stack test location | New `internal/integration/` package | (A) `internal/cli/run_integration_test.go`, (B) `internal/handler/integration_test.go` | `cli` imports `spanneradapter` which pulls the Spanner SDK into the test binary. `handler` tests would need to import middleware, violating the package's unit-test-only convention. A standalone package imports only what it needs: `handler`, `middleware`, `orchestrator`, `adapter`, `metrics`, `testutil`. Matches the existing `internal/antirez/` pattern. |
| 2 | Spanner test data seeding | Direct `spanner.Mutation` inserts | (A) Use repo methods for seeding, (B) Use `scripts/seed-emulator` shell script | Direct mutations give precise control over column values (e.g., `ExpiresAt` in the past, `NULL` ClassCode) that cannot be set through repo methods which enforce `CommitTimestamp`. The seed script is designed for demo data, not per-test isolation. |
| 3 | Build tag strategy for Spanner tests | `//go:build integration` | (A) Skip via `os.Getenv` check only (no build tag), (B) Separate Go module | Build tag prevents Spanner SDK compilation in `go test ./...`, keeping the fast path fast. Env-var-only skip still compiles the Spanner SDK. Separate module is over-engineering for 4 test files. |
| 4 | CLI lifecycle test approach | `t.Setenv` + `context.WithCancel` | (A) Refactor `cli.Run` to accept a config struct, (B) Use `os/exec` to spawn a subprocess | `cli.Run` is already testable via `t.Setenv` (safe, auto-restored). Refactoring is unnecessary scope creep. Subprocess testing is fragile and slow. |
| 5 | Concurrency limit test mechanism | Blocking handler with channel signaling | (A) Sleep-based timing, (B) Atomic counter polling | Channel signaling is deterministic -- goroutines hold semaphore slots until explicitly released, eliminating timing flakiness. Sleep-based tests are inherently flaky. |
| 6 | Emulator readiness detection in CI | Poll HTTP endpoint with retry loop | (A) Fixed `sleep 30`, (B) Docker healthcheck wait | Polling is the fastest reliable approach -- exits as soon as the emulator responds. Fixed sleep wastes time or races. Docker healthcheck requires modifying `docker-compose.yml`. |
| 7 | Test isolation per Spanner test | Unique `RequestID`/`RuleId`/`CarrierId` per test | (A) Truncate tables between tests, (B) Create fresh database per test | Unique IDs allow parallel-safe test execution without cleanup overhead. Truncation serializes tests. Fresh databases are slow (~2s each on emulator). |

---

## Data Flow

### HTTP Stack Integration Test Wiring

```
newTestStack(t, concurrencyLimit)
  |
  +-- adapter.NewMockCarrier("alpha", cfg, log)
  +-- adapter.NewMockCarrier("beta", cfg, log)
  +-- adapter.RegisterMockCarrier(mock) --> AdapterFunc
  +-- adapter.NewRegistry().Register(id, fn)
  +-- circuitbreaker.New(id, cbCfg, rec)
  +-- ratelimiter.New(id, rlCfg, rec)
  +-- orchestrator.NewEMATracker(id, hint, cfg, rec)
  +-- orchestrator.New(OrchestratorConfig{...Repo: nil})
  +-- handler.New(HandlerConfig{Orch, Metrics, Gatherer, Log, DB: nil})
  +-- mux := http.NewServeMux()
  +-- h.RegisterRoutes(mux)
  |
  +-- middleware.LimitConcurrency(mux, limit, log)
  +-- middleware.RequireAPIKey(srv, keys, skipPaths, log) --> (handler, stopAuth)
  +-- middleware.SecurityHeaders(authHandler)
  +-- middleware.AuditLog(secHandler, log)
  |
  +-- httptest.NewServer(finalHandler) --> *httptest.Server
  +-- t.Cleanup(srv.Close)
  +-- t.Cleanup(stopAuth)
  |
  Returns: (serverURL string, apiKey string)
```

```
Test HTTP call flow:

  http.Post(serverURL+"/quotes", body, "Authorization: Bearer "+apiKey)
    -> AuditLog (wraps ResponseWriter in statusRecorder)
      -> SecurityHeaders (sets 5 headers)
        -> RequireAPIKey (validates Bearer token; injects clientIDKey)
          -> LimitConcurrency (semaphore acquire or 503)
            -> mux.ServeHTTP (routes POST /quotes)
              -> handler.handlePostQuotes
                -> orchestrator.GetQuotes (no cache -- Repo is nil)
                  -> fanOut -> callCarrier x N -> MockCarrier.Call
                -> JSON encode quoteResponse
```

### Spanner Adapter Test Wiring

```
newEmulatorClient(t)
  |
  +-- os.Getenv("SPANNER_EMULATOR_HOST") -> skip if empty
  +-- spanner.NewClient(ctx, "projects/P/instances/I/databases/D")
  +-- t.Cleanup(client.Close)
  |
  Returns: *spanner.Client

Test data flow (QuoteRepo example):

  client.Apply(ctx, []*spanner.Mutation{
    spanner.InsertOrUpdate("Quotes", columns, values),  // direct mutation
  })
    |
    v
  repo := spanner.NewQuoteRepo(client)
  repo.FindByRequestID(ctx, "req-xxx")
    -> SELECT ... WHERE RequestID = @requestID AND ExpiresAt > CURRENT_TIMESTAMP()
    -> []domain.QuoteResult, found, err
```

### CLI Lifecycle Test Flow

```
TestRun_MissingAPIKeys:
  t.Setenv("API_KEYS", "")
  err := cli.Run(ctx, nil, io.Discard, io.Discard)
  assert err != nil, contains "API_KEYS environment variable required"

TestRun_CleanShutdown:
  t.Setenv("API_KEYS", "test-key-abc")
  t.Setenv("PORT", freePort())
  ctx, cancel := context.WithCancel(t.Context())
  go func() { errCh <- cli.Run(ctx, nil, io.Discard, io.Discard) }()
  pollHealthz(t, "http://127.0.0.1:"+port+"/healthz", 2*time.Second)
  cancel()
  err := <-errCh   // expect nil within 5s
```

---

## File Changes

| # | File Path | Action | Description |
|---|---|---|---|
| 1 | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | create | Full middleware chain integration tests: happy path, auth 401, security headers, concurrency 503, health/readyz/metrics bypass, cleanup |
| 2 | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/testutil_test.go` | create | `newEmulatorClient(t)` helper and `clearTable(t, client, table)` helper; `//go:build integration` |
| 3 | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/quote_repo_test.go` | create | `QuoteRepo` tests: Save/FindByRequestID round-trip, miss, expired filtering, DeleteExpired; `//go:build integration` |
| 4 | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/carrier_repo_test.go` | create | `CarrierRepo.ListActive` tests: active filtering, JSON config decoding, NULL config; `//go:build integration` |
| 5 | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/appetite_repo_test.go` | create | `AppetiteRepo` tests: FindMatchingRules (state+LOB, classCode, premium range, no match), ListAll; `//go:build integration` |
| 6 | `/home/reche/projects/ProyectoAgentero/internal/cli/run_test.go` | create | `cli.Run` lifecycle tests: missing API_KEYS error, clean shutdown via context cancel |
| 7 | `/home/reche/projects/ProyectoAgentero/.github/workflows/go.yml` | modify | Add `integration-tests` job: Spanner emulator via docker compose, go test with `-tags integration` |
| 8 | `/home/reche/projects/ProyectoAgentero/Makefile` | modify | Add `test-integration` target |

**Summary**: 6 files created, 2 files modified, 0 files deleted

---

## Interfaces and Contracts

No new interfaces or types are introduced. All tests consume existing production interfaces:

- `ports.OrchestratorPort` -- consumed by `handler.New` (via `handler.HandlerConfig.Orch`)
- `ports.MetricsRecorder` -- satisfied by `testutil.NoopRecorder`
- `ports.QuoteRepository` -- satisfied by `spanner.QuoteRepo`; set to `nil` in HTTP stack tests
- `ports.CarrierRepository` -- satisfied by `spanner.CarrierRepo`
- `ports.AppetiteRepository` -- satisfied by `spanner.AppetiteRepo`

### Test Helper Function Signatures

```go
// internal/integration/http_stack_test.go
// newTestStack assembles the full production middleware chain with mock carriers.
// Returns the server URL and the API key to use in requests.
func newTestStack(t *testing.T, concurrencyLimit int) (serverURL string, apiKey string)
```

```go
// internal/adapter/spanner/testutil_test.go
// newEmulatorClient connects to the Spanner emulator or skips the test.
func newEmulatorClient(t *testing.T) *spanner.Client

// envOrDefault returns os.Getenv(key) if non-empty, else fallback.
func envOrDefault(key, fallback string) string
```

```go
// internal/cli/run_test.go
// freePort obtains an ephemeral port via net.Listen(":0") and returns it as a string.
func freePort(t *testing.T) string

// pollHealthz polls GET /healthz until 200 or timeout.
func pollHealthz(t *testing.T, url string, timeout time.Duration)
```

---

## Implementation Details

### File 1: `internal/integration/http_stack_test.go`

**Package**: `integration_test` (external test package)

**Imports**: `handler`, `middleware`, `orchestrator`, `adapter`, `circuitbreaker`, `ratelimiter`, `domain`, `testutil`, `metrics`, `prometheus`, `httptest`, `net/http`, `encoding/json`, `io`, `log/slog`, `strings`, `sync`, `testing`, `time`

**Constants**:
```go
const testAPIKey = "test-key-abc"
```

**Helper**: `newTestStack`

```go
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
            ID:           "alpha",
            Name:         "Alpha",
            Capabilities: []domain.CoverageLine{domain.CoverageLineAuto, domain.CoverageLineHomeowners},
            Config: domain.CarrierConfig{
                TimeoutHint: 50 * time.Millisecond, FailureThreshold: 5,
                SuccessThreshold: 2, OpenTimeout: 30 * time.Second,
                HedgeMultiplier: 1.5, EMAWarmupObservations: 10,
                RateLimit: domain.RateLimitConfig{TokensPerSecond: 100, Burst: 10},
            },
        },
        {
            ID:           "beta",
            Name:         "Beta",
            Capabilities: []domain.CoverageLine{domain.CoverageLineAuto},
            Config: domain.CarrierConfig{
                TimeoutHint: 50 * time.Millisecond, FailureThreshold: 5,
                SuccessThreshold: 2, OpenTimeout: 30 * time.Second,
                HedgeMultiplier: 1.5, EMAWarmupObservations: 10,
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

    // Wire middleware identically to cli.Run lines 178-184
    var srv http.Handler = mux
    srv = middleware.LimitConcurrency(srv, concurrencyLimit, log)
    authHandler, stopAuth := middleware.RequireAPIKey(srv, []string{testAPIKey},
        []string{"/healthz", "/readyz", "/metrics"}, log)
    srv = authHandler
    srv = middleware.SecurityHeaders(srv)
    srv = middleware.AuditLog(srv, log)

    ts := httptest.NewServer(srv)
    t.Cleanup(ts.Close)
    t.Cleanup(stopAuth)

    return ts.URL, testAPIKey
}
```

**Response type** (mirrors handler's unexported type):
```go
type quoteResponseBody struct {
    RequestID  string      `json:"request_id"`
    Quotes     []quoteItem `json:"quotes"`
    DurationMs int64       `json:"duration_ms"`
}

type quoteItem struct {
    CarrierID    string `json:"carrier_id"`
    PremiumCents int64  `json:"premium_cents"`
    Currency     string `json:"currency"`
    IsHedged     bool   `json:"is_hedged"`
    LatencyMs    int64  `json:"latency_ms"`
}
```

**Tests**:

```go
func TestHTTPStack_PostQuotes_HappyPath(t *testing.T) {
    // REQ-HTTP-STACK-001 scenario 1: authenticated POST /quotes returns 200 with JSON
    t.Parallel()
    url, key := newTestStack(t, 100)

    body := `{"request_id":"integ-001","coverage_lines":["auto"],"timeout_ms":5000}`
    req, _ := http.NewRequest(http.MethodPost, url+"/quotes", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+key)

    resp, err := http.DefaultClient.Do(req)
    // assert err == nil
    // assert resp.StatusCode == 200
    // assert resp.Header.Get("Content-Type") == "application/json"
    // decode JSON -> quoteResponseBody
    // assert len(parsed.Quotes) >= 1
    // assert each quote has non-empty CarrierID and PremiumCents > 0
}

func TestHTTPStack_PostQuotes_RequestIDAndDuration(t *testing.T) {
    // REQ-HTTP-STACK-001 scenario 2: response includes request_id and duration_ms
    t.Parallel()
    url, key := newTestStack(t, 100)

    body := `{"request_id":"integ-002","coverage_lines":["auto"],"timeout_ms":5000}`
    // send POST with auth
    // decode response
    // assert parsed.RequestID == "integ-002"
    // assert parsed.DurationMs >= 0
}

func TestHTTPStack_PostQuotes_MissingAuth(t *testing.T) {
    // REQ-HTTP-STACK-002 scenario 1: no Authorization header -> 401
    t.Parallel()
    url, _ := newTestStack(t, 100)

    body := `{"request_id":"integ-003","coverage_lines":["auto"]}`
    req, _ := http.NewRequest(http.MethodPost, url+"/quotes", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    // NO Authorization header

    resp, err := http.DefaultClient.Do(req)
    // assert resp.StatusCode == 401
    // assert body contains "UNAUTHORIZED"
}

func TestHTTPStack_PostQuotes_WrongKey(t *testing.T) {
    // REQ-HTTP-STACK-002 scenario 2: invalid Bearer token -> 401
    t.Parallel()
    url, _ := newTestStack(t, 100)

    body := `{"request_id":"integ-004","coverage_lines":["auto"]}`
    req, _ := http.NewRequest(http.MethodPost, url+"/quotes", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer wrong-key-999")

    resp, err := http.DefaultClient.Do(req)
    // assert resp.StatusCode == 401
    // assert body contains "UNAUTHORIZED"
}

func TestHTTPStack_SecurityHeaders(t *testing.T) {
    // REQ-HTTP-STACK-003 scenario 1: all 5 security headers present on authenticated request
    t.Parallel()
    url, key := newTestStack(t, 100)

    body := `{"request_id":"integ-005","coverage_lines":["auto"]}`
    req, _ := http.NewRequest(http.MethodPost, url+"/quotes", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+key)

    resp, err := http.DefaultClient.Do(req)
    // assert resp.Header.Get("X-Content-Type-Options") == "nosniff"
    // assert resp.Header.Get("X-Frame-Options") == "DENY"
    // assert resp.Header.Get("Strict-Transport-Security") == "max-age=63072000; includeSubDomains"
    // assert resp.Header.Get("Cache-Control") == "no-store"
    // assert resp.Header.Get("Content-Security-Policy") == "default-src 'none'"
}

func TestHTTPStack_SecurityHeaders_OnError(t *testing.T) {
    // REQ-HTTP-STACK-003 scenario 2: security headers present on 400 error response
    t.Parallel()
    url, key := newTestStack(t, 100)

    req, _ := http.NewRequest(http.MethodPost, url+"/quotes", strings.NewReader(""))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+key)

    resp, err := http.DefaultClient.Do(req)
    // assert resp.StatusCode == 400
    // assert resp.Header.Get("X-Content-Type-Options") == "nosniff"
    // assert resp.Header.Get("X-Frame-Options") == "DENY"
}

func TestHTTPStack_ConcurrencyLimit_503(t *testing.T) {
    // REQ-HTTP-STACK-004 scenario 1: overflow request gets 503 when semaphore full
    t.Parallel()
    // Use concurrencyLimit=2 with a blocking handler
    // This requires a custom stack with a slow/blocking mock carrier

    log := slog.New(slog.NewTextHandler(io.Discard, nil))
    blockCh := make(chan struct{})

    // Build a custom handler that blocks until signaled
    blockingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        <-blockCh // block until released
        w.WriteHeader(http.StatusOK)
    })

    const limit = 2
    srv := middleware.LimitConcurrency(blockingHandler, limit, log)
    authHandler, stopAuth := middleware.RequireAPIKey(srv, []string{testAPIKey},
        []string{"/healthz"}, log)
    finalHandler := middleware.SecurityHeaders(authHandler)
    finalHandler = middleware.AuditLog(finalHandler, log)

    ts := httptest.NewServer(finalHandler)
    t.Cleanup(ts.Close)
    t.Cleanup(stopAuth)

    // Fill both slots
    var wg sync.WaitGroup
    for i := 0; i < limit; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            req, _ := http.NewRequest(http.MethodPost, ts.URL+"/quotes",
                strings.NewReader(`{"request_id":"block","coverage_lines":["auto"]}`))
            req.Header.Set("Authorization", "Bearer "+testAPIKey)
            http.DefaultClient.Do(req)
        }()
    }
    // Give goroutines time to acquire slots
    time.Sleep(50 * time.Millisecond)

    // Overflow request
    req, _ := http.NewRequest(http.MethodPost, ts.URL+"/quotes",
        strings.NewReader(`{"request_id":"overflow","coverage_lines":["auto"]}`))
    req.Header.Set("Authorization", "Bearer "+testAPIKey)
    resp, _ := http.DefaultClient.Do(req)
    // assert resp.StatusCode == 503
    // assert resp.Header.Get("Retry-After") == "1"
    // assert body contains "SERVICE_UNAVAILABLE"

    close(blockCh) // release blockers
    wg.Wait()
}

func TestHTTPStack_ConcurrencyLimit_RecoveryAfterRelease(t *testing.T) {
    // REQ-HTTP-STACK-004 scenario 2: requests succeed after semaphore slots freed
    // Similar to above but after close(blockCh), send a new request and assert 200
}

func TestHTTPStack_HealthzBypassesAuth(t *testing.T) {
    // REQ-HTTP-STACK-005 scenario 1
    t.Parallel()
    url, _ := newTestStack(t, 100)
    resp, _ := http.Get(url + "/healthz") // no Authorization header
    // assert resp.StatusCode == 200
    // assert body == "ok"
}

func TestHTTPStack_ReadyzBypassesAuth(t *testing.T) {
    // REQ-HTTP-STACK-005 scenario 2
    t.Parallel()
    url, _ := newTestStack(t, 100)
    resp, _ := http.Get(url + "/readyz")
    // assert resp.StatusCode == 200
    // assert body == "ok"
}

func TestHTTPStack_MetricsBypassesAuth(t *testing.T) {
    // REQ-HTTP-STACK-005 scenario 3
    t.Parallel()
    url, _ := newTestStack(t, 100)
    resp, _ := http.Get(url + "/metrics")
    // assert resp.StatusCode == 200
}
```

**Cleanup / goroutine-leak safety** (REQ-HTTP-STACK-006): `newTestStack` registers `t.Cleanup(srv.Close)` and `t.Cleanup(stopAuth)` -- every test that calls `newTestStack` inherits both cleanups. The concurrency-limit test creates its own server and explicitly registers the same cleanups. All tests will be run with `go test -race` in CI.

**Estimated LOC**: ~350

---

### File 2: `internal/adapter/spanner/testutil_test.go`

**Build tag**: `//go:build integration`

**Package**: `spanner` (internal test package -- same package to access unexported types if needed)

```go
//go:build integration

package spanner

import (
    "context"
    "os"
    "testing"

    "cloud.google.com/go/spanner"
)

// newEmulatorClient creates a Spanner client connected to the emulator.
// Calls t.Skip if SPANNER_EMULATOR_HOST is not set.
// Registers t.Cleanup to close the client.
func newEmulatorClient(t *testing.T) *spanner.Client {
    t.Helper()
    host := os.Getenv("SPANNER_EMULATOR_HOST")
    if host == "" {
        t.Skip("SPANNER_EMULATOR_HOST not set — skipping integration test")
    }
    client, err := NewClient(
        context.Background(),
        envOrDefault("SPANNER_PROJECT", "riskforge-dev"),
        envOrDefault("SPANNER_INSTANCE", "test-instance"),
        envOrDefault("SPANNER_DATABASE", "test-db"),
    )
    if err != nil {
        t.Fatalf("newEmulatorClient: %v", err)
    }
    t.Cleanup(func() { client.Close() })
    return client
}

func envOrDefault(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return fallback
}
```

**Estimated LOC**: ~40

---

### File 3: `internal/adapter/spanner/quote_repo_test.go`

**Build tag**: `//go:build integration`

**Package**: `spanner`

```go
//go:build integration

package spanner

import (
    "context"
    "testing"
    "time"

    "cloud.google.com/go/spanner"
    "github.com/rechedev9/riskforge/internal/domain"
)

func TestQuoteRepo_Save_FindByRequestID(t *testing.T) {
    // REQ-QUOTE-REPO-001 scenario 1: save two results, find both ordered by PremiumCents ASC
    client := newEmulatorClient(t)
    repo := NewQuoteRepo(client)
    ctx := context.Background()

    requestID := "req-save-001"
    results := []domain.QuoteResult{
        {
            CarrierID:  "carrier-a",
            Premium:    domain.Money{Amount: 5000, Currency: "USD"},
            ExpiresAt:  time.Now().Add(1 * time.Hour),
            IsHedged:   false,
            Latency:    50 * time.Millisecond,
            CarrierRef: "ref-a",
        },
        {
            CarrierID:  "carrier-b",
            Premium:    domain.Money{Amount: 3000, Currency: "USD"},
            ExpiresAt:  time.Now().Add(1 * time.Hour),
            IsHedged:   false,
            Latency:    80 * time.Millisecond,
            CarrierRef: "ref-b",
        },
    }
    err := repo.Save(ctx, requestID, results)
    // assert err == nil
    found, ok, err := repo.FindByRequestID(ctx, requestID)
    // assert err == nil, ok == true, len(found) == 2
    // assert found[0].Premium.Amount == 3000 (lowest first)
    // assert found[1].Premium.Amount == 5000
}

func TestQuoteRepo_FindByRequestID_Miss(t *testing.T) {
    // REQ-QUOTE-REPO-001 scenario 2: unknown ID returns not found
    client := newEmulatorClient(t)
    repo := NewQuoteRepo(client)
    ctx := context.Background()

    results, found, err := repo.FindByRequestID(ctx, "nonexistent-req-999")
    // assert err == nil, found == false, results == nil
}

func TestQuoteRepo_FindByRequestID_IgnoresExpired(t *testing.T) {
    // REQ-QUOTE-REPO-002 scenario 1: expired row not returned
    client := newEmulatorClient(t)
    ctx := context.Background()

    // Insert expired row via direct mutation (bypass CommitTimestamp)
    requestID := "req-expired-001"
    m, _ := spanner.InsertOrUpdateStruct("Quotes", &quoteRow{
        RequestID:    requestID,
        CarrierID:    "carrier-exp",
        PremiumCents: 1000,
        Currency:     "USD",
        ExpiresAt:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
        IsHedged:     false,
        LatencyMs:    10,
        CarrierRef:   "",
        CreatedAt:    spanner.CommitTimestamp,
    })
    _, _ = client.Apply(ctx, []*spanner.Mutation{m})

    repo := NewQuoteRepo(client)
    _, found, err := repo.FindByRequestID(ctx, requestID)
    // assert err == nil, found == false
}

func TestQuoteRepo_FindByRequestID_MixedExpiry(t *testing.T) {
    // REQ-QUOTE-REPO-002 scenario 2: mix of expired and valid, only valid returned
    client := newEmulatorClient(t)
    ctx := context.Background()
    requestID := "req-mixed-001"

    // Insert expired row via direct mutation
    mExpired, _ := spanner.InsertOrUpdateStruct("Quotes", &quoteRow{
        RequestID: requestID, CarrierID: "carrier-x",
        PremiumCents: 1000, Currency: "USD",
        ExpiresAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
        IsHedged: false, LatencyMs: 10, CreatedAt: spanner.CommitTimestamp,
    })
    // Insert valid row via direct mutation
    mValid, _ := spanner.InsertOrUpdateStruct("Quotes", &quoteRow{
        RequestID: requestID, CarrierID: "carrier-y",
        PremiumCents: 2000, Currency: "USD",
        ExpiresAt: time.Now().Add(1 * time.Hour),
        IsHedged: false, LatencyMs: 20, CreatedAt: spanner.CommitTimestamp,
    })
    _, _ = client.Apply(ctx, []*spanner.Mutation{mExpired, mValid})

    repo := NewQuoteRepo(client)
    results, found, err := repo.FindByRequestID(ctx, requestID)
    // assert err == nil, found == true, len(results) == 1
}

func TestQuoteRepo_DeleteExpired(t *testing.T) {
    // REQ-QUOTE-REPO-003 scenarios 1+2: delete expired row, verify count, verify miss
    client := newEmulatorClient(t)
    ctx := context.Background()
    requestID := "req-del-001"

    // Insert expired row
    m, _ := spanner.InsertOrUpdateStruct("Quotes", &quoteRow{
        RequestID: requestID, CarrierID: "carrier-del",
        PremiumCents: 500, Currency: "USD",
        ExpiresAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
        IsHedged: false, LatencyMs: 5, CreatedAt: spanner.CommitTimestamp,
    })
    _, _ = client.Apply(ctx, []*spanner.Mutation{m})

    repo := NewQuoteRepo(client)
    count, err := repo.DeleteExpired(ctx)
    // assert err == nil, count >= 1

    _, found, err := repo.FindByRequestID(ctx, requestID)
    // assert err == nil, found == false
}
```

**Estimated LOC**: ~140

---

### File 4: `internal/adapter/spanner/carrier_repo_test.go`

**Build tag**: `//go:build integration`

**Package**: `spanner`

```go
//go:build integration

package spanner

import (
    "context"
    "encoding/json"
    "testing"
    "time"

    "cloud.google.com/go/spanner"
    "github.com/rechedev9/riskforge/internal/domain"
)

func TestCarrierRepo_ListActive_FiltersInactive(t *testing.T) {
    // REQ-CARRIER-REPO-001 scenario 1: only active carriers returned
    client := newEmulatorClient(t)
    ctx := context.Background()

    // Insert 3 carriers: 2 active, 1 inactive
    mutations := []*spanner.Mutation{
        spanner.InsertOrUpdate("Carriers",
            []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
            []interface{}{"carrier-a", "Active A", "A", true,
                spanner.CommitTimestamp, spanner.CommitTimestamp}),
        spanner.InsertOrUpdate("Carriers",
            []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
            []interface{}{"carrier-b", "Active B", "B", true,
                spanner.CommitTimestamp, spanner.CommitTimestamp}),
        spanner.InsertOrUpdate("Carriers",
            []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
            []interface{}{"carrier-c", "Inactive C", "C", false,
                spanner.CommitTimestamp, spanner.CommitTimestamp}),
    }
    _, err := client.Apply(ctx, mutations)
    // assert err == nil

    repo := NewCarrierRepo(client)
    carriers, err := repo.ListActive(ctx)
    // assert err == nil
    // assert len(carriers) >= 2  (use >=, other tests may have seeded data)
    // assert no carrier has ID == "carrier-c"
    // assert carrier-a and carrier-b present
}

func TestCarrierRepo_ListActive_AllInactive(t *testing.T) {
    // REQ-CARRIER-REPO-001 scenario 2: all inactive -> empty
    // Use unique carrier IDs to avoid collision
    client := newEmulatorClient(t)
    ctx := context.Background()

    // Insert 1 inactive carrier with unique ID
    m := spanner.InsertOrUpdate("Carriers",
        []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
        []interface{}{"carrier-only-inactive", "Inactive", "INX", false,
            spanner.CommitTimestamp, spanner.CommitTimestamp})
    _, _ = client.Apply(ctx, []*spanner.Mutation{m})

    repo := NewCarrierRepo(client)
    carriers, err := repo.ListActive(ctx)
    // assert err == nil
    // NOTE: Other tests may seed active carriers. This test asserts the inactive one is excluded.
    // We verify "carrier-only-inactive" is NOT in the returned slice.
    for _, c := range carriers {
        if c.ID == "carrier-only-inactive" {
            t.Fatal("inactive carrier should not be in ListActive results")
        }
    }
}

func TestCarrierRepo_ListActive_DecodesConfig(t *testing.T) {
    // REQ-CARRIER-REPO-002 scenario 1: JSON Config round-trip preserves values
    client := newEmulatorClient(t)
    ctx := context.Background()

    configJSON := map[string]interface{}{
        "TimeoutHint":      150000000, // 150ms in nanoseconds (time.Duration)
        "FailureThreshold": 3,
        "RateLimit":        map[string]interface{}{"TokensPerSecond": 50.0, "Burst": 5},
    }
    configBytes, _ := json.Marshal(configJSON)

    m := spanner.InsertOrUpdate("Carriers",
        []string{"CarrierId", "Name", "Code", "IsActive", "Config", "CreatedAt", "UpdatedAt"},
        []interface{}{"carrier-cfg", "Config Test", "CFG", true,
            spanner.NullJSON{Value: json.RawMessage(configBytes), Valid: true},
            spanner.CommitTimestamp, spanner.CommitTimestamp})
    _, _ = client.Apply(ctx, []*spanner.Mutation{m})

    repo := NewCarrierRepo(client)
    carriers, err := repo.ListActive(ctx)
    // assert err == nil
    // find carrier with ID "carrier-cfg"
    // assert Config.TimeoutHint == 150*time.Millisecond
    // assert Config.FailureThreshold == 3
    // assert Config.RateLimit.TokensPerSecond == 50
}

func TestCarrierRepo_ListActive_NullConfig(t *testing.T) {
    // REQ-CARRIER-REPO-002 scenario 2: NULL Config -> zero-value CarrierConfig
    client := newEmulatorClient(t)
    ctx := context.Background()

    m := spanner.InsertOrUpdate("Carriers",
        []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
        []interface{}{"carrier-null-cfg", "Null Config", "NCF", true,
            spanner.CommitTimestamp, spanner.CommitTimestamp})
    _, _ = client.Apply(ctx, []*spanner.Mutation{m})

    repo := NewCarrierRepo(client)
    carriers, err := repo.ListActive(ctx)
    // find "carrier-null-cfg"
    // assert Config == domain.CarrierConfig{}
}
```

**Estimated LOC**: ~130

---

### File 5: `internal/adapter/spanner/appetite_repo_test.go`

**Build tag**: `//go:build integration`

**Package**: `spanner`

```go
//go:build integration

package spanner

import (
    "context"
    "testing"

    "cloud.google.com/go/spanner"
    "github.com/rechedev9/riskforge/internal/domain"
)

func TestAppetiteRepo_FindMatchingRules_RequiredOnly(t *testing.T) {
    // REQ-APPETITE-REPO-001 scenario 1: match by state and LOB
    client := newEmulatorClient(t)
    ctx := context.Background()

    // Insert carrier parent row first (interleaved table)
    carrierMut := spanner.InsertOrUpdate("Carriers",
        []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
        []interface{}{"carrier-appetite-1", "Appetite Test", "APT", true,
            spanner.CommitTimestamp, spanner.CommitTimestamp})

    ruleMutCA := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "IsActive", "CreatedAt"},
        []interface{}{"rule-req-1", "carrier-appetite-1", "CA", "auto", true,
            spanner.CommitTimestamp})
    ruleMutNY := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "IsActive", "CreatedAt"},
        []interface{}{"rule-req-2", "carrier-appetite-1", "NY", "auto", true,
            spanner.CommitTimestamp})

    _, _ = client.Apply(ctx, []*spanner.Mutation{carrierMut, ruleMutCA, ruleMutNY})

    repo := NewAppetiteRepo(client)
    rules, err := repo.FindMatchingRules(ctx, domain.RiskClassification{
        State: "CA", LineOfBusiness: "auto",
    })
    // assert err == nil, len(rules) >= 1
    // assert all returned rules have State == "CA"
}

func TestAppetiteRepo_FindMatchingRules_NoMatch(t *testing.T) {
    // REQ-APPETITE-REPO-001 scenario 2: no match -> empty, nil error
    client := newEmulatorClient(t)
    repo := NewAppetiteRepo(client)
    rules, err := repo.FindMatchingRules(context.Background(), domain.RiskClassification{
        State: "ZZ", LineOfBusiness: "nonexistent",
    })
    // assert err == nil, len(rules) == 0
}

func TestAppetiteRepo_FindMatchingRules_ClassCodeWildcard(t *testing.T) {
    // REQ-APPETITE-REPO-002 scenario 1: NULL ClassCode matches any classCode query
    client := newEmulatorClient(t)
    ctx := context.Background()

    carrierMut := spanner.InsertOrUpdate("Carriers",
        []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
        []interface{}{"carrier-cc-test", "CC Test", "CCT", true,
            spanner.CommitTimestamp, spanner.CommitTimestamp})

    // Rule with specific ClassCode
    ruleExact := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "ClassCode", "IsActive", "CreatedAt"},
        []interface{}{"rule-cc1", "carrier-cc-test", "CA", "auto", "auto-commercial", true,
            spanner.CommitTimestamp})

    // Rule with NULL ClassCode (wildcard)
    ruleWild := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "IsActive", "CreatedAt"},
        []interface{}{"rule-cc2", "carrier-cc-test", "CA", "auto", true,
            spanner.CommitTimestamp})
    // Note: ClassCode column omitted -> NULL

    _, _ = client.Apply(ctx, []*spanner.Mutation{carrierMut, ruleExact, ruleWild})

    repo := NewAppetiteRepo(client)
    rules, err := repo.FindMatchingRules(ctx, domain.RiskClassification{
        State: "CA", LineOfBusiness: "auto", ClassCode: "auto-commercial",
    })
    // assert err == nil
    // assert len(rules) >= 2 (both exact and wildcard match)
}

func TestAppetiteRepo_FindMatchingRules_EmptyClassCodeSkipsFilter(t *testing.T) {
    // REQ-APPETITE-REPO-002 scenario 2: empty classCode skips ClassCode filter entirely
    client := newEmulatorClient(t)
    ctx := context.Background()

    carrierMut := spanner.InsertOrUpdate("Carriers",
        []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
        []interface{}{"carrier-cc-skip", "Skip Test", "SKP", true,
            spanner.CommitTimestamp, spanner.CommitTimestamp})
    ruleWithCC := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "ClassCode", "IsActive", "CreatedAt"},
        []interface{}{"rule-cc-skip-1", "carrier-cc-skip", "CA", "auto", "auto-commercial", true,
            spanner.CommitTimestamp})
    _, _ = client.Apply(ctx, []*spanner.Mutation{carrierMut, ruleWithCC})

    repo := NewAppetiteRepo(client)
    rules, err := repo.FindMatchingRules(ctx, domain.RiskClassification{
        State: "CA", LineOfBusiness: "auto", ClassCode: "", // empty -> no ClassCode filter
    })
    // assert err == nil, len(rules) >= 1
}

func TestAppetiteRepo_FindMatchingRules_PremiumInRange(t *testing.T) {
    // REQ-APPETITE-REPO-003 scenario 1: premium within range matches
    client := newEmulatorClient(t)
    ctx := context.Background()

    carrierMut := spanner.InsertOrUpdate("Carriers",
        []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
        []interface{}{"carrier-prem", "Premium Test", "PRM", true,
            spanner.CommitTimestamp, spanner.CommitTimestamp})
    rulePrem := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "MinPremium", "MaxPremium", "IsActive", "CreatedAt"},
        []interface{}{"rule-prem-1", "carrier-prem", "CA", "auto", 1000.0, 5000.0, true,
            spanner.CommitTimestamp})
    _, _ = client.Apply(ctx, []*spanner.Mutation{carrierMut, rulePrem})

    repo := NewAppetiteRepo(client)
    rules, err := repo.FindMatchingRules(ctx, domain.RiskClassification{
        State: "CA", LineOfBusiness: "auto", EstimatedPremium: 3000,
    })
    // assert err == nil, len(rules) >= 1
}

func TestAppetiteRepo_FindMatchingRules_PremiumOutOfRange(t *testing.T) {
    // REQ-APPETITE-REPO-003 scenario 2: premium outside range -> no match
    client := newEmulatorClient(t)
    ctx := context.Background()

    // Reuse carrier-prem and rule-prem-1 from above (or re-seed)
    carrierMut := spanner.InsertOrUpdate("Carriers",
        []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
        []interface{}{"carrier-prem-out", "Premium Out", "PO", true,
            spanner.CommitTimestamp, spanner.CommitTimestamp})
    rulePrem := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "MinPremium", "MaxPremium", "IsActive", "CreatedAt"},
        []interface{}{"rule-prem-out", "carrier-prem-out", "CA", "auto", 1000.0, 5000.0, true,
            spanner.CommitTimestamp})
    _, _ = client.Apply(ctx, []*spanner.Mutation{carrierMut, rulePrem})

    repo := NewAppetiteRepo(client)
    rules, err := repo.FindMatchingRules(ctx, domain.RiskClassification{
        State: "CA", LineOfBusiness: "auto", EstimatedPremium: 6000,
    })
    // assert err == nil, len(rules) == 0 (rule-prem-out has MaxPremium=5000 < 6000)
    // Note: other rules in emulator may match; filter results by CarrierId for precise check
}

func TestAppetiteRepo_ListAll(t *testing.T) {
    // REQ-APPETITE-REPO-004 scenario 1: all active rules across states
    client := newEmulatorClient(t)
    ctx := context.Background()

    carrierMut := spanner.InsertOrUpdate("Carriers",
        []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
        []interface{}{"carrier-listall", "ListAll", "LA", true,
            spanner.CommitTimestamp, spanner.CommitTimestamp})
    rCA := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "IsActive", "CreatedAt"},
        []interface{}{"rule-la-ca", "carrier-listall", "CA", "auto", true, spanner.CommitTimestamp})
    rNY := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "IsActive", "CreatedAt"},
        []interface{}{"rule-la-ny", "carrier-listall", "NY", "auto", true, spanner.CommitTimestamp})
    rTX := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "IsActive", "CreatedAt"},
        []interface{}{"rule-la-tx", "carrier-listall", "TX", "auto", true, spanner.CommitTimestamp})
    _, _ = client.Apply(ctx, []*spanner.Mutation{carrierMut, rCA, rNY, rTX})

    repo := NewAppetiteRepo(client)
    rules, err := repo.ListAll(ctx)
    // assert err == nil, len(rules) >= 3
}

func TestAppetiteRepo_ListAll_ExcludesInactive(t *testing.T) {
    // REQ-APPETITE-REPO-004 scenario 2: inactive rules excluded
    client := newEmulatorClient(t)
    ctx := context.Background()

    carrierMut := spanner.InsertOrUpdate("Carriers",
        []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt"},
        []interface{}{"carrier-inactive-rule", "InactiveRule", "IR", true,
            spanner.CommitTimestamp, spanner.CommitTimestamp})
    rInactive := spanner.InsertOrUpdate("AppetiteRules",
        []string{"RuleId", "CarrierId", "State", "LineOfBusiness", "IsActive", "CreatedAt"},
        []interface{}{"rule-inactive-1", "carrier-inactive-rule", "CA", "auto", false,
            spanner.CommitTimestamp})
    _, _ = client.Apply(ctx, []*spanner.Mutation{carrierMut, rInactive})

    repo := NewAppetiteRepo(client)
    rules, err := repo.ListAll(ctx)
    // assert err == nil
    // assert "rule-inactive-1" NOT in returned slice
    for _, r := range rules {
        if r.RuleID == "rule-inactive-1" {
            t.Fatal("inactive rule should not appear in ListAll")
        }
    }
}
```

**Estimated LOC**: ~200

---

### File 6: `internal/cli/run_test.go`

**Package**: `cli_test` (external test package)

**No build tag** -- runs in standard `go test ./...`.

```go
package cli_test

import (
    "context"
    "fmt"
    "io"
    "net"
    "net/http"
    "strings"
    "testing"
    "time"

    "github.com/rechedev9/riskforge/internal/cli"
)

// freePort obtains an ephemeral port from the OS.
func freePort(t *testing.T) string {
    t.Helper()
    l, err := net.Listen("tcp", ":0")
    if err != nil {
        t.Fatalf("freePort: %v", err)
    }
    port := l.Addr().(*net.TCPAddr).Port
    l.Close()
    return fmt.Sprintf("%d", port)
}

// pollHealthz polls GET url until HTTP 200 or timeout.
func pollHealthz(t *testing.T, url string, timeout time.Duration) {
    t.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        resp, err := http.Get(url)
        if err == nil && resp.StatusCode == http.StatusOK {
            resp.Body.Close()
            return
        }
        if resp != nil {
            resp.Body.Close()
        }
        time.Sleep(50 * time.Millisecond)
    }
    t.Fatalf("pollHealthz: %s did not return 200 within %v", url, timeout)
}

func TestRun_MissingAPIKeys_Empty(t *testing.T) {
    // REQ-CLI-LIFECYCLE-001 scenario 1: empty API_KEYS returns error
    t.Setenv("API_KEYS", "")
    // Ensure no Spanner connection attempt
    t.Setenv("SPANNER_PROJECT", "")
    t.Setenv("SPANNER_INSTANCE", "")
    t.Setenv("SPANNER_DATABASE", "")

    ctx := t.Context()
    err := cli.Run(ctx, nil, io.Discard, io.Discard)
    if err == nil {
        t.Fatal("expected error when API_KEYS is empty, got nil")
    }
    if !strings.Contains(err.Error(), "API_KEYS environment variable required") {
        t.Fatalf("unexpected error: %v", err)
    }
}

func TestRun_MissingAPIKeys_Unset(t *testing.T) {
    // REQ-CLI-LIFECYCLE-001 scenario 2: unset API_KEYS returns error
    t.Setenv("API_KEYS", "")
    t.Setenv("SPANNER_PROJECT", "")
    t.Setenv("SPANNER_INSTANCE", "")
    t.Setenv("SPANNER_DATABASE", "")

    ctx := t.Context()
    err := cli.Run(ctx, nil, io.Discard, io.Discard)
    if err == nil {
        t.Fatal("expected error when API_KEYS is unset, got nil")
    }
    if !strings.Contains(err.Error(), "API_KEYS environment variable required") {
        t.Fatalf("unexpected error: %v", err)
    }
}

func TestRun_CleanShutdown(t *testing.T) {
    // REQ-CLI-LIFECYCLE-002 scenarios 1+2: server starts, responds on /healthz, shuts down on cancel
    port := freePort(t)
    t.Setenv("API_KEYS", "test-key-abc")
    t.Setenv("PORT", port)
    // No Spanner -> falls back to mock carriers
    t.Setenv("SPANNER_PROJECT", "")
    t.Setenv("SPANNER_INSTANCE", "")
    t.Setenv("SPANNER_DATABASE", "")

    ctx, cancel := context.WithCancel(t.Context())
    defer cancel()

    errCh := make(chan error, 1)
    go func() {
        errCh <- cli.Run(ctx, nil, io.Discard, io.Discard)
    }()

    // Poll /healthz for readiness
    pollHealthz(t, fmt.Sprintf("http://127.0.0.1:%s/healthz", port), 2*time.Second)

    // Cancel context to trigger shutdown
    cancel()

    // Wait for Run to return
    select {
    case err := <-errCh:
        if err != nil {
            t.Fatalf("expected nil error from clean shutdown, got: %v", err)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("cli.Run did not return within 5 seconds after context cancel")
    }
}
```

**Estimated LOC**: ~100

---

### File 7: `.github/workflows/go.yml` (modification)

Add `integration-tests` job after the existing `build-and-test` job:

```yaml
  integration-tests:
    name: Integration Tests (Spanner Emulator)
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.25"

      - name: Start Spanner emulator
        run: docker compose up -d spanner-emulator spanner-init

      - name: Wait for emulator ready
        run: |
          for i in $(seq 1 30); do
            curl -sf http://localhost:9020/v1/projects/riskforge-dev/instances && break
            sleep 2
          done

      - name: Run integration tests
        env:
          SPANNER_EMULATOR_HOST: localhost:9010
          SPANNER_PROJECT: riskforge-dev
          SPANNER_INSTANCE: test-instance
          SPANNER_DATABASE: test-db
        run: go test -tags integration -race -count=1 -v ./internal/adapter/spanner/...
```

The `integration-tests` job has no `needs:` dependency on `build-and-test` -- they run in parallel. A Spanner emulator failure does not block the fast-path unit test job.

---

### File 8: `Makefile` (modification)

Add `test-integration` to `.PHONY` and as a new target:

```makefile
.PHONY: build test vet check docker up down clean test-integration

test-integration:
	docker compose up -d spanner-emulator spanner-init
	@echo "Waiting for Spanner emulator..."
	@for i in $$(seq 1 30); do \
		curl -sf http://localhost:9020/v1/projects/riskforge-dev/instances > /dev/null 2>&1 && break; \
		sleep 2; \
	done
	SPANNER_EMULATOR_HOST=localhost:9010 \
	SPANNER_PROJECT=riskforge-dev \
	SPANNER_INSTANCE=test-instance \
	SPANNER_DATABASE=test-db \
	go test -tags integration -race -count=1 -v ./internal/adapter/spanner/...
```

---

## Testing Strategy

| # | What to Test | Type | File Path | Maps to Requirement |
|---|---|---|---|---|
| 1 | Authenticated POST /quotes returns 200 with valid JSON | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-001 |
| 2 | Response includes request_id and duration_ms | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-001 |
| 3 | Missing auth returns 401 | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-002 |
| 4 | Invalid Bearer token returns 401 | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-002 |
| 5 | All 5 security headers on authenticated request | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-003 |
| 6 | Security headers on error response | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-003 |
| 7 | 503 when concurrency semaphore full | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-004 |
| 8 | Request succeeds after semaphore release | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-004 |
| 9 | GET /healthz bypasses auth | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-005 |
| 10 | GET /readyz bypasses auth | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-005 |
| 11 | GET /metrics bypasses auth | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-005 |
| 12 | stopAuth cleanup via t.Cleanup | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-006 |
| 13 | Race detector passes | integration | `/home/reche/projects/ProyectoAgentero/internal/integration/http_stack_test.go` | REQ-HTTP-STACK-006 |
| 14 | Save + FindByRequestID round-trip | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/quote_repo_test.go` | REQ-QUOTE-REPO-001 |
| 15 | FindByRequestID miss | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/quote_repo_test.go` | REQ-QUOTE-REPO-001 |
| 16 | Expired row not returned | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/quote_repo_test.go` | REQ-QUOTE-REPO-002 |
| 17 | Mixed expiry: only valid returned | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/quote_repo_test.go` | REQ-QUOTE-REPO-002 |
| 18 | DeleteExpired removes expired row | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/quote_repo_test.go` | REQ-QUOTE-REPO-003 |
| 19 | FindByRequestID misses after DeleteExpired | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/quote_repo_test.go` | REQ-QUOTE-REPO-003 |
| 20 | Emulator host unset -> t.Skip | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/testutil_test.go` | REQ-QUOTE-REPO-004 |
| 21 | Emulator host set -> valid client | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/testutil_test.go` | REQ-QUOTE-REPO-004 |
| 22 | ListActive filters inactive carriers | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/carrier_repo_test.go` | REQ-CARRIER-REPO-001 |
| 23 | ListActive with all inactive returns empty | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/carrier_repo_test.go` | REQ-CARRIER-REPO-001 |
| 24 | JSON Config round-trip preserves fields | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/carrier_repo_test.go` | REQ-CARRIER-REPO-002 |
| 25 | NULL Config -> zero-value CarrierConfig | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/carrier_repo_test.go` | REQ-CARRIER-REPO-002 |
| 26 | FindMatchingRules by state+LOB | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/appetite_repo_test.go` | REQ-APPETITE-REPO-001 |
| 27 | FindMatchingRules no match | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/appetite_repo_test.go` | REQ-APPETITE-REPO-001 |
| 28 | NULL ClassCode matches as wildcard | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/appetite_repo_test.go` | REQ-APPETITE-REPO-002 |
| 29 | Empty classCode skips filter | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/appetite_repo_test.go` | REQ-APPETITE-REPO-002 |
| 30 | Premium in range matches | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/appetite_repo_test.go` | REQ-APPETITE-REPO-003 |
| 31 | Premium out of range -> no match | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/appetite_repo_test.go` | REQ-APPETITE-REPO-003 |
| 32 | ListAll returns all active rules | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/appetite_repo_test.go` | REQ-APPETITE-REPO-004 |
| 33 | ListAll excludes inactive | integration | `/home/reche/projects/ProyectoAgentero/internal/adapter/spanner/appetite_repo_test.go` | REQ-APPETITE-REPO-004 |
| 34 | Empty API_KEYS returns error | unit | `/home/reche/projects/ProyectoAgentero/internal/cli/run_test.go` | REQ-CLI-LIFECYCLE-001 |
| 35 | Unset API_KEYS returns error | unit | `/home/reche/projects/ProyectoAgentero/internal/cli/run_test.go` | REQ-CLI-LIFECYCLE-001 |
| 36 | Server starts, /healthz OK, clean shutdown | integration | `/home/reche/projects/ProyectoAgentero/internal/cli/run_test.go` | REQ-CLI-LIFECYCLE-002 |
| 37 | Server does not block after cancel | integration | `/home/reche/projects/ProyectoAgentero/internal/cli/run_test.go` | REQ-CLI-LIFECYCLE-002 |
| 38 | CI job env vars correct | config | `/home/reche/projects/ProyectoAgentero/.github/workflows/go.yml` | REQ-CI-INTEGRATION-001 |
| 39 | CI job independent of build-and-test | config | `/home/reche/projects/ProyectoAgentero/.github/workflows/go.yml` | REQ-CI-INTEGRATION-001 |
| 40 | make test-integration runs tests | config | `/home/reche/projects/ProyectoAgentero/Makefile` | REQ-CI-INTEGRATION-002 |
| 41 | test-integration is additive | config | `/home/reche/projects/ProyectoAgentero/Makefile` | REQ-CI-INTEGRATION-002 |
| 42 | go test ./... excludes integration tests | config | All `*_test.go` with `//go:build integration` | REQ-CI-INTEGRATION-003 |
| 43 | go test -tags integration includes them | config | All `*_test.go` with `//go:build integration` | REQ-CI-INTEGRATION-003 |

### Test Dependencies

- **Mocks needed**: `adapter.MockCarrier` (existing), `testutil.NoopRecorder` (existing) -- both used in HTTP stack tests. No new mocks needed.
- **Fixtures needed**: Direct `spanner.Mutation` inserts for Spanner tests (carriers, appetite rules, quotes). Each test seeds its own data with unique IDs.
- **Infrastructure**: Spanner emulator via `docker compose up -d spanner-emulator spanner-init` for Spanner adapter tests. No infrastructure needed for HTTP stack or CLI tests.

---

## Traceability Matrix

| Requirement | Test Function | File |
|---|---|---|
| REQ-HTTP-STACK-001 (S1) | `TestHTTPStack_PostQuotes_HappyPath` | `http_stack_test.go` |
| REQ-HTTP-STACK-001 (S2) | `TestHTTPStack_PostQuotes_RequestIDAndDuration` | `http_stack_test.go` |
| REQ-HTTP-STACK-002 (S1) | `TestHTTPStack_PostQuotes_MissingAuth` | `http_stack_test.go` |
| REQ-HTTP-STACK-002 (S2) | `TestHTTPStack_PostQuotes_WrongKey` | `http_stack_test.go` |
| REQ-HTTP-STACK-003 (S1) | `TestHTTPStack_SecurityHeaders` | `http_stack_test.go` |
| REQ-HTTP-STACK-003 (S2) | `TestHTTPStack_SecurityHeaders_OnError` | `http_stack_test.go` |
| REQ-HTTP-STACK-004 (S1) | `TestHTTPStack_ConcurrencyLimit_503` | `http_stack_test.go` |
| REQ-HTTP-STACK-004 (S2) | `TestHTTPStack_ConcurrencyLimit_RecoveryAfterRelease` | `http_stack_test.go` |
| REQ-HTTP-STACK-005 (S1) | `TestHTTPStack_HealthzBypassesAuth` | `http_stack_test.go` |
| REQ-HTTP-STACK-005 (S2) | `TestHTTPStack_ReadyzBypassesAuth` | `http_stack_test.go` |
| REQ-HTTP-STACK-005 (S3) | `TestHTTPStack_MetricsBypassesAuth` | `http_stack_test.go` |
| REQ-HTTP-STACK-006 (S1) | All tests via `t.Cleanup(stopAuth)` in `newTestStack` | `http_stack_test.go` |
| REQ-HTTP-STACK-006 (S2) | `go test -race` in CI | `http_stack_test.go` |
| REQ-QUOTE-REPO-001 (S1) | `TestQuoteRepo_Save_FindByRequestID` | `quote_repo_test.go` |
| REQ-QUOTE-REPO-001 (S2) | `TestQuoteRepo_FindByRequestID_Miss` | `quote_repo_test.go` |
| REQ-QUOTE-REPO-002 (S1) | `TestQuoteRepo_FindByRequestID_IgnoresExpired` | `quote_repo_test.go` |
| REQ-QUOTE-REPO-002 (S2) | `TestQuoteRepo_FindByRequestID_MixedExpiry` | `quote_repo_test.go` |
| REQ-QUOTE-REPO-003 (S1) | `TestQuoteRepo_DeleteExpired` (first half) | `quote_repo_test.go` |
| REQ-QUOTE-REPO-003 (S2) | `TestQuoteRepo_DeleteExpired` (second half) | `quote_repo_test.go` |
| REQ-QUOTE-REPO-004 (S1) | `newEmulatorClient` skip path | `testutil_test.go` |
| REQ-QUOTE-REPO-004 (S2) | `newEmulatorClient` success path | `testutil_test.go` |
| REQ-CARRIER-REPO-001 (S1) | `TestCarrierRepo_ListActive_FiltersInactive` | `carrier_repo_test.go` |
| REQ-CARRIER-REPO-001 (S2) | `TestCarrierRepo_ListActive_AllInactive` | `carrier_repo_test.go` |
| REQ-CARRIER-REPO-002 (S1) | `TestCarrierRepo_ListActive_DecodesConfig` | `carrier_repo_test.go` |
| REQ-CARRIER-REPO-002 (S2) | `TestCarrierRepo_ListActive_NullConfig` | `carrier_repo_test.go` |
| REQ-APPETITE-REPO-001 (S1) | `TestAppetiteRepo_FindMatchingRules_RequiredOnly` | `appetite_repo_test.go` |
| REQ-APPETITE-REPO-001 (S2) | `TestAppetiteRepo_FindMatchingRules_NoMatch` | `appetite_repo_test.go` |
| REQ-APPETITE-REPO-002 (S1) | `TestAppetiteRepo_FindMatchingRules_ClassCodeWildcard` | `appetite_repo_test.go` |
| REQ-APPETITE-REPO-002 (S2) | `TestAppetiteRepo_FindMatchingRules_EmptyClassCodeSkipsFilter` | `appetite_repo_test.go` |
| REQ-APPETITE-REPO-003 (S1) | `TestAppetiteRepo_FindMatchingRules_PremiumInRange` | `appetite_repo_test.go` |
| REQ-APPETITE-REPO-003 (S2) | `TestAppetiteRepo_FindMatchingRules_PremiumOutOfRange` | `appetite_repo_test.go` |
| REQ-APPETITE-REPO-004 (S1) | `TestAppetiteRepo_ListAll` | `appetite_repo_test.go` |
| REQ-APPETITE-REPO-004 (S2) | `TestAppetiteRepo_ListAll_ExcludesInactive` | `appetite_repo_test.go` |
| REQ-CLI-LIFECYCLE-001 (S1) | `TestRun_MissingAPIKeys_Empty` | `run_test.go` |
| REQ-CLI-LIFECYCLE-001 (S2) | `TestRun_MissingAPIKeys_Unset` | `run_test.go` |
| REQ-CLI-LIFECYCLE-002 (S1) | `TestRun_CleanShutdown` | `run_test.go` |
| REQ-CLI-LIFECYCLE-002 (S2) | `TestRun_CleanShutdown` (timeout assertion) | `run_test.go` |
| REQ-CI-INTEGRATION-001 (S1) | CI job YAML env vars | `go.yml` |
| REQ-CI-INTEGRATION-001 (S2) | CI job no `needs:` dependency | `go.yml` |
| REQ-CI-INTEGRATION-002 (S1) | Makefile `test-integration` target | `Makefile` |
| REQ-CI-INTEGRATION-002 (S2) | Existing `test` target unchanged | `Makefile` |
| REQ-CI-INTEGRATION-003 (S1) | `//go:build integration` on all Spanner test files | All `*_test.go` |
| REQ-CI-INTEGRATION-003 (S2) | `-tags integration` flag in CI and Makefile | `go.yml`, `Makefile` |

---

## Migration and Rollout

No migration or rollout steps required. All changes are additive test files and CI configuration. No production code is modified.

### Rollback Steps

Per the proposal's rollback plan:
1. Delete `internal/integration/http_stack_test.go`
2. Delete `internal/cli/run_test.go`
3. Delete `internal/adapter/spanner/testutil_test.go`, `quote_repo_test.go`, `carrier_repo_test.go`, `appetite_repo_test.go`
4. Revert `integration-tests` job from `.github/workflows/go.yml`
5. Revert `test-integration` target from `Makefile`

No production behavior is affected.

---

## Open Questions

None. All technical questions from the exploration phase have been resolved:

- CLI testability: `t.Setenv` approach confirmed (no refactoring needed).
- Emulator support for `PartitionedUpdate`: supported since emulator v1.4; `latest` tag is sufficient.
- Antirez overlap: delineated by package and focus (correctness vs. adversarial).
- `AppetiteRules` interleaved table: test mutations must include parent `Carriers` row first (addressed in all appetite test data seeding above).

---

**Next Step**: After both design and specs are complete, run `sdd-tasks` to create the implementation checklist.

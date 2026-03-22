# Proposal: integration-e2e-testing

**Date**: 2026-03-22
**Status**: Draft

## Intent

Add integration and E2E tests to close three hard gaps in riskforge test coverage: the full assembled middleware chain (currently exercised only via adversarial/antirez tests that skip `SecurityHeaders` and `LimitConcurrency` assertions), the Spanner adapter repositories (zero test files), and the `cli.Run` startup lifecycle (zero test file). The work follows a hybrid strategy — in-process HTTP stack tests run in every `go test ./...` invocation; Spanner adapter tests are guarded by `//go:build integration` and run only when the emulator is available.

## Scope

### In Scope

- New `internal/integration/` package: full HTTP middleware chain tests using `httptest.NewServer` with real `net.Listener`, mock carriers, no Spanner
- New `internal/adapter/spanner/quote_repo_test.go` (`//go:build integration`): `Save`, `FindByRequestID` (cache hit/miss), `DeleteExpired`
- New `internal/adapter/spanner/carrier_repo_test.go` (`//go:build integration`): `ListActive` including `NullJSON` config decoding and positional column ordering
- New `internal/adapter/spanner/appetite_repo_test.go` (`//go:build integration`): `FindMatchingRules` (state+LOB required; classCode+premium optional), `ListAll`
- New `internal/adapter/spanner/testutil_test.go` (`//go:build integration`): shared emulator client helper
- New `internal/cli/run_test.go`: startup lifecycle — `API_KEYS` missing returns error immediately; clean shutdown via context cancellation; poll `/healthz` for readiness
- `.github/workflows/go.yml` — second CI job (`integration-tests`) that starts the Spanner emulator via `docker-compose` and runs `go test -tags integration -race -count=1 ./internal/adapter/spanner/...`
- `Makefile` — new `test-integration` target

### Out of Scope

- `internal/domain/` unit tests (pure value types and sentinel errors; deferred — low marginal value)
- Real GCP Spanner tests (emulator only)
- HTTP carrier tests against live external endpoints
- Coverage threshold enforcement (deferred to a separate change)
- Emulator version pinning in `docker-compose.yml` (deferred — low urgency)
- Refactoring `cli.Run` to accept a config struct (not required; `t.Setenv` is sufficient)
- Duplicating antirez adversarial patterns in new integration tests

## Approach

Option C (hybrid) from the exploration: in-process HTTP stack tests cover the gap in assembled middleware chain verification without any CI infrastructure cost; emulator-gated Spanner tests cover the SQL/column-ordering/NullJSON gaps that cannot be verified with fakes; `cli.Run` lifecycle tests use `t.Setenv` and context cancellation to avoid signal machinery.

**Rationale for not modifying `cli.Run`:** The current signature `Run(ctx context.Context, _ []string, stdout, _ io.Writer) error` is directly testable. `t.Setenv("API_KEYS", "...")` sets env vars for the duration of the test and restores them on cleanup. No refactoring is required.

**Rationale for a separate `internal/integration/` package rather than `internal/cli/integration_test.go`:** `cli` imports every adapter including `spanneradapter`. An `_test` package under `cli` would transitively pull Spanner into every test binary even when Spanner is not under test. A standalone `internal/integration/` package has no import of `cli` and imports only `handler`, `middleware`, `orchestrator`, `adapter`, `metrics`, and `testutil` — no Spanner dependency.

**Delineation from antirez tests:** `internal/antirez/antirez_test.go` uses `httptest.NewServer` with `RequireAPIKey` and `mux` but focuses on adversarial boundary attacks (malformed JSON, concurrency storms, corpus-driven fuzzing). The new `internal/integration/` tests focus on correctness of the assembled stack for the happy path and specific middleware behaviors (`SecurityHeaders` values, `LimitConcurrency` 503, unauthenticated 401, `/healthz` bypass). There is no behavioral overlap — antirez tests should not be modified.

### New Files

| File path | Purpose | Build tag |
|---|---|---|
| `internal/integration/http_stack_test.go` | Full middleware chain E2E tests using `httptest.NewServer`; mock carriers; no Spanner | (none — runs in standard `go test ./...`) |
| `internal/adapter/spanner/quote_repo_test.go` | `QuoteRepo.Save`, `FindByRequestID` (hit/miss/expiry), `DeleteExpired` against emulator | `//go:build integration` |
| `internal/adapter/spanner/carrier_repo_test.go` | `CarrierRepo.ListActive`, JSON config round-trip, inactive row filtering | `//go:build integration` |
| `internal/adapter/spanner/appetite_repo_test.go` | `AppetiteRepo.FindMatchingRules` (required-only, classCode, premium range, no match), `ListAll` | `//go:build integration` |
| `internal/adapter/spanner/testutil_test.go` | `newEmulatorClient(t)` helper — reads env vars, calls `spanneradapter.NewClient`, calls `t.Cleanup(client.Close)` | `//go:build integration` |
| `internal/cli/run_test.go` | `TestRun_MissingAPIKeys`, `TestRun_CleanShutdown` — uses `t.Setenv`, `context.WithCancel`, polls `/healthz` | (none) |

### Modified Files

| File path | Change description |
|---|---|
| `.github/workflows/go.yml` | Add `integration-tests` job: `docker-compose up -d spanner-emulator spanner-init`, wait-for-ready step, `go test -tags integration -race -count=1 ./internal/adapter/spanner/...` |
| `Makefile` | Add `test-integration` target: `docker compose up -d && go test -tags integration -race -count=1 ./internal/adapter/spanner/...` |

## Test Strategy

### `internal/integration/http_stack_test.go`

Wires the full production middleware chain identically to `cli.Run` (lines 178–184):

```
AuditLog → SecurityHeaders → RequireAPIKey → LimitConcurrency → mux → Handler → Orchestrator
```

Uses `httptest.NewServer` (real `net.Listener`) with mock carriers (alpha, beta, gamma identical to `buildCarriers()` defaults). Each test calls `t.Cleanup(srv.Close)` and `t.Cleanup(stopAuth)`.

Tests:

- `TestHTTPStack_PostQuotes_HappyPath` — authenticated POST `/quotes` with valid body; asserts 200, non-empty `quotes` array, `Content-Type: application/json`
- `TestHTTPStack_PostQuotes_Unauthorized` — missing `Authorization` header; asserts 401
- `TestHTTPStack_PostQuotes_WrongKey` — `Authorization: Bearer wrong`; asserts 401
- `TestHTTPStack_SecurityHeaders` — authenticated request; asserts `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` present in response
- `TestHTTPStack_ConcurrencyLimit` — fill semaphore with N goroutines, then one extra; asserts 503 on the overflow request
- `TestHTTPStack_HealthzBypassesAuth` — unauthenticated GET `/healthz`; asserts 200
- `TestHTTPStack_ReadyzBypassesAuth` — unauthenticated GET `/readyz`; asserts 200
- `TestHTTPStack_MetricsBypassesAuth` — unauthenticated GET `/metrics`; asserts 200

### `internal/adapter/spanner/testutil_test.go`

```go
//go:build integration

func newEmulatorClient(t *testing.T) *spanner.Client {
    t.Helper()
    host := os.Getenv("SPANNER_EMULATOR_HOST")
    if host == "" {
        t.Skip("SPANNER_EMULATOR_HOST not set — skipping integration test")
    }
    client, err := NewClient(context.Background(),
        envOrDefault("SPANNER_PROJECT", "riskforge-dev"),
        envOrDefault("SPANNER_INSTANCE", "test-instance"),
        envOrDefault("SPANNER_DATABASE", "test-db"),
    )
    require.NoError(t, err)
    t.Cleanup(client.Close)
    return client
}
```

The `t.Skip` guard means the integration tests degrade gracefully if the emulator is absent (e.g., a developer running `go test -tags integration ./...` locally without Docker).

### `internal/adapter/spanner/quote_repo_test.go`

Tests use `newEmulatorClient(t)`, then `NewQuoteRepo(client)`. Each test inserts then reads via the repo; no raw Spanner mutations bypass the repo interface.

- `TestQuoteRepo_Save_FindByRequestID` — save two results for the same `requestID`; call `FindByRequestID`; assert `found == true`, len == 2, `PremiumCents` match, results ordered ascending
- `TestQuoteRepo_FindByRequestID_Miss` — call `FindByRequestID` for an unknown ID; assert `found == false`, `results == nil`
- `TestQuoteRepo_FindByRequestID_IgnoresExpired` — save one result with `ExpiresAt` in the past (using a direct Spanner mutation to bypass the repo's `CommitTimestamp` constraint); call `FindByRequestID`; assert `found == false`
- `TestQuoteRepo_DeleteExpired` — save one expired row via direct mutation; call `DeleteExpired`; assert returned count >= 1; call `FindByRequestID`; assert miss

### `internal/adapter/spanner/carrier_repo_test.go`

Tests insert rows directly via Spanner mutations, then read via `NewCarrierRepo(client).ListActive(ctx)`.

- `TestCarrierRepo_ListActive_ReturnsActive` — insert two active rows and one inactive row; assert `ListActive` returns exactly two carriers
- `TestCarrierRepo_ListActive_DecodesConfig` — insert one active row with a JSON `Config` containing `TimeoutHint`, `FailureThreshold`, `RateLimit`; assert decoded `domain.CarrierConfig` fields match input (verifies `json.Marshal` → `NullJSON` → `json.Unmarshal` round-trip and positional column ordering: `CarrierId, Name, Code, Config`)
- `TestCarrierRepo_ListActive_NullConfig` — insert one active row with `Config = NULL`; assert `ListActive` returns the carrier with a zero-value `domain.CarrierConfig`

### `internal/adapter/spanner/appetite_repo_test.go`

Tests insert rows directly, then read via `NewAppetiteRepo(client).FindMatchingRules(ctx, risk)`.

- `TestAppetiteRepo_FindMatchingRules_RequiredOnly` — insert two rules: one matching `state="CA"`, `lob="auto"` and one for `state="NY"`; call with `{State: "CA", LineOfBusiness: "auto"}`; assert one result
- `TestAppetiteRepo_FindMatchingRules_WithClassCode` — insert rule with `ClassCode = "auto-commercial"` and one with `ClassCode = NULL`; call with classCode set; assert both returned (NULL acts as wildcard per the `ClassCode IS NULL OR ClassCode = @classCode` SQL)
- `TestAppetiteRepo_FindMatchingRules_PremiumRange` — insert rule with `MinPremium = 1000`, `MaxPremium = 5000`; call with `EstimatedPremium = 3000`; assert match; call with `EstimatedPremium = 6000`; assert no match
- `TestAppetiteRepo_FindMatchingRules_NoMatch` — call with state/lob with no seeded data; assert empty slice, nil error
- `TestAppetiteRepo_ListAll` — insert three rules across different states; call `ListAll`; assert all three returned

### `internal/cli/run_test.go`

No build tag — runs in standard `go test ./...`. Does not require Spanner (Spanner env vars absent → `cli.Run` falls back to mock carriers).

- `TestRun_MissingAPIKeys` — `t.Setenv("API_KEYS", "")`, call `cli.Run(ctx, nil, io.Discard, io.Discard)`; assert returned error wraps the string `"API_KEYS environment variable required"`; assert returns immediately (no blocking)
- `TestRun_CleanShutdown` — `t.Setenv("API_KEYS", "test-key-abc")`, `t.Setenv("PORT", freePort())`; call `cli.Run` in a goroutine; poll `GET http://127.0.0.1:{port}/healthz` until 200 or 2 s timeout; cancel context; assert goroutine returns nil within 5 s

`freePort()` is a helper that opens and immediately closes a `net.Listen("tcp", ":0")` to obtain an ephemeral port, avoiding port collisions between parallel tests.

### CI integration job

```yaml
integration-tests:
  name: Integration Tests (Spanner Emulator)
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
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

The integration job is independent of `build-and-test` — a Spanner emulator failure does not block the fast-path job.

## Risks

| Risk | Mitigation |
|---|---|
| Spanner emulator absent in developer's local env | `newEmulatorClient(t)` calls `t.Skip` when `SPANNER_EMULATOR_HOST` is unset; integration tests degrade silently rather than fail |
| `TestRun_CleanShutdown` flakiness from port collision | Use `net.Listen(":0")` to obtain an OS-assigned ephemeral port; pass it to `Run` via `t.Setenv("PORT", ...)` |
| `TestRun_CleanShutdown` blocked by `signal.NotifyContext` inside `cli.Run` | `cli.Run` wraps the passed-in `ctx` with `signal.NotifyContext` at line 45; cancelling the parent context propagates correctly — `<-ctx.Done()` fires at line 203 regardless of the signal wrapper |
| Column order regression in `CarrierRepo.ListActive` | `TestCarrierRepo_ListActive_DecodesConfig` verifies the full `row.Columns(&id, &name, &code, &configJSON)` scan against a known insertion; a reorder in the SQL breaks this test deterministically |
| `antirez` test overlap causing confusion | New integration tests are in a separate package (`internal/integration`) with a distinct focus (correctness + middleware headers); antirez focus remains adversarial/chaos; no shared helpers are introduced between the two |

## Rollback Plan

All new test files are additive — they introduce no changes to production code. Rollback is:

1. Delete `internal/integration/http_stack_test.go`
2. Delete `internal/cli/run_test.go`
3. Delete `internal/adapter/spanner/*_test.go` and `internal/adapter/spanner/testutil_test.go`
4. Revert `.github/workflows/go.yml` to remove the `integration-tests` job
5. Revert `Makefile` to remove `test-integration` target

No production behavior is affected by any of these deletions.

## Success Criteria

- `go test -race -count=1 ./...` passes with zero new failures and includes `internal/integration/` and `internal/cli/` packages in the run
- `go test -tags integration -race -count=1 ./internal/adapter/spanner/...` passes against a running Spanner emulator (`docker compose up -d spanner-emulator spanner-init`)
- The `integration-tests` CI job is green on a clean `main` push
- The following specific behaviors are verified by named tests:
  - `TestHTTPStack_PostQuotes_Unauthorized` confirms 401 without `Authorization` header
  - `TestHTTPStack_SecurityHeaders` confirms `X-Content-Type-Options: nosniff` is present
  - `TestHTTPStack_ConcurrencyLimit` confirms 503 when semaphore is full
  - `TestQuoteRepo_FindByRequestID_IgnoresExpired` confirms expired rows are not returned
  - `TestCarrierRepo_ListActive_DecodesConfig` confirms `NullJSON` → `domain.CarrierConfig` round-trip
  - `TestAppetiteRepo_FindMatchingRules_WithClassCode` confirms `NULL ClassCode` acts as wildcard
  - `TestRun_MissingAPIKeys` confirms `cli.Run` returns a non-nil error immediately when `API_KEYS` is empty
  - `TestRun_CleanShutdown` confirms `cli.Run` returns nil after context cancellation

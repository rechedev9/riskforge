# Exploration: integration-e2e-testing

## Current State

The riskforge codebase has thorough unit coverage for individual components but
has no integration or E2E tests that exercise the full assembled stack.

**Existing test inventory (17 test files across 9 packages):**

| Package | Test file(s) | What is tested |
|---|---|---|
| `internal/handler` | `http_test.go`, `fuzz_test.go` | Handler in isolation with mock orchestrator via `httptest.NewRecorder` |
| `internal/middleware` | `middleware_test.go` | Each middleware in isolation with `httptest.NewRecorder` |
| `internal/orchestrator` | `orchestrator_test.go`, `orchestrator_concurrency_test.go`, `orchestrator_random_test.go`, `chaos_test.go`, `hedging_test.go`, `leak_test.go` | Orchestrator with mock adapters; no real HTTP server, no Spanner |
| `internal/adapter` | `http_carrier_test.go`, `mock_carrier_test.go`, `adapter_test.go` | HTTP carrier against `httptest.Server`, mock carrier |
| `internal/circuitbreaker` | `breaker_test.go` | CB state machine |
| `internal/ratelimiter` | `limiter_test.go` | Token bucket |
| `internal/cleanup` | `ticker_test.go` | Cleanup ticker with stub repo |
| `internal/metrics` | `prometheus_test.go` | Prometheus recorder |
| `internal/antirez` | `antirez_test.go` | Adversarial black-box tests wiring handler+orchestrator+middleware via `httptest.NewServer` but with mock carriers and no Spanner |

**What is NOT tested:**

- `internal/cli/run.go` — zero test file; the full server wiring, `API_KEYS` validation, signal handling, graceful shutdown path
- `internal/adapter/spanner/` — zero test file for any of `QuoteRepo`, `CarrierRepo`, `AppetiteRepo`
- `internal/domain/` — zero test file (pure value types and sentinel errors; low risk but some behavior worth pinning)
- Full HTTP stack assembled end-to-end: `AuditLog → SecurityHeaders → RequireAPIKey → LimitConcurrency → mux → Handler → Orchestrator → MockCarrier` with a real `net.Listener`
- Cache hit / cache miss paths in orchestrator (require a wired `QuoteRepository`)
- `cli.Run` failure paths (missing `API_KEYS`, Spanner connection error)

**CI pipeline state:**
`.github/workflows/go.yml` runs `go test -race -count=1 ./...` with no emulator service. The Spanner adapter packages are not skipped — they simply have no tests. The workflow has no `services:` block and does not start the Spanner emulator.

**Infrastructure for emulator:**
`docker-compose.yml` brings up `gcr.io/cloud-spanner-emulator/emulator:latest` on ports `9010` (gRPC) and `9020` (HTTP/REST). A `spanner-init` container creates project `riskforge-dev`, instance `test-instance`, database `test-db` with the full DDL (Carriers, AppetiteRules, Quotes tables). `scripts/seed-emulator` seeds demo data via `gcloud`. The Spanner client respects `SPANNER_EMULATOR_HOST` env var (handled transparently by the Google Cloud Go SDK — `NewClient` in `internal/adapter/spanner/client.go` line 12 does not need modification).

---

## Relevant Files

| File | Role | Notes |
|---|---|---|
| `internal/cli/run.go` | Server wiring | Only production file; no test file; wires all deps, reads env vars, starts HTTP server |
| `internal/handler/http.go` | HTTP handler | `handlePostQuotes`, `handleReadyz`, `/metrics`, `/healthz`; fully unit-tested in isolation |
| `internal/handler/http_test.go` | Existing unit tests | Uses `httptest.NewRecorder` + mock orchestrator; middleware NOT in the chain |
| `internal/domain/quote.go` | Value types | `QuoteRequest`, `QuoteResult`, `Money`, `CoverageLine` |
| `internal/domain/carrier.go` | Value types | `Carrier`, `CarrierConfig`, `RateLimitConfig` |
| `internal/domain/appetite.go` | Value types | `AppetiteRule`, `RiskClassification` |
| `internal/domain/errors.go` | Sentinel errors | 6 sentinel errors; no tests |
| `internal/adapter/spanner/client.go` | Spanner client constructor | Reads `SPANNER_EMULATOR_HOST` from environment automatically |
| `internal/adapter/spanner/quote_repo.go` | Spanner QuoteRepo | `Save`, `FindByRequestID`, `DeleteExpired` |
| `internal/adapter/spanner/carrier_repo.go` | Spanner CarrierRepo | `ListActive` — reads Carriers table, decodes JSON Config |
| `internal/adapter/spanner/appetite_repo.go` | Spanner AppetiteRepo | `FindMatchingRules`, `ListAll` — reads AppetiteRules table |
| `internal/ports/repository_port.go` | QuoteRepository interface | Documents nil-is-allowed contract |
| `internal/ports/quote_port.go` | CarrierPort, OrchestratorPort interfaces | |
| `internal/ports/appetite_port.go` | AppetiteRepository, CarrierRepository interfaces | |
| `internal/ports/metrics_port.go` | MetricsRecorder interface | |
| `internal/middleware/auth.go` | API key auth middleware | `RequireAPIKey` returns `(http.Handler, func())` — stop func must be called |
| `internal/middleware/security.go` | Security headers | `SecurityHeaders` — pure wrapper |
| `internal/middleware/concurrency.go` | Concurrency limiter | `LimitConcurrency` — semaphore; returns 503 at cap |
| `internal/middleware/audit.go` | Audit log | `AuditLog` — `statusRecorder` wrapper |
| `internal/orchestrator/orchestrator.go` | Orchestrator | Cache hit/miss logic at lines 98–111; singleflight at 118–132; repo.Save at 245–253 |
| `internal/testutil/recorder.go` | `NoopRecorder` | Shared test helper; atomic call counters |
| `docker-compose.yml` | Local infra | Spanner emulator + init container; project `riskforge-dev`, instance `test-instance`, db `test-db` |
| `.github/workflows/go.yml` | CI | No Spanner emulator service; does not guard Spanner tests |
| `scripts/seed-emulator` | Seed script | Seeds 3 carriers + 10 appetite rules via `gcloud` |
| `Makefile` | Build targets | `test` = `go test -race -count=1 ./...`; no emulator target |

---

## Dependency Map

```
cli.Run
  ├── spanneradapter.NewClient          (env: SPANNER_EMULATOR_HOST, SPANNER_PROJECT, ...)
  ├── spanneradapter.NewCarrierRepo     → ListActive (Spanner Carriers table)
  ├── spanneradapter.NewQuoteRepo       → Save, FindByRequestID, DeleteExpired
  ├── adapter.NewRegistry + MockCarrier
  ├── circuitbreaker.New
  ├── ratelimiter.New
  ├── orchestrator.NewEMATracker
  ├── orchestrator.New(OrchestratorConfig)   ← repo is QuoteRepository (nil when no Spanner)
  ├── handler.New(HandlerConfig)
  ├── mux.RegisterRoutes
  ├── middleware.LimitConcurrency
  ├── middleware.RequireAPIKey            ← returns stopFn — must defer
  ├── middleware.SecurityHeaders
  ├── middleware.AuditLog
  └── http.Server{ Addr, Handler, timeouts }

Test wiring gaps:
  antirez_test: wires handler+orch+middleware but via httptest.NewServer, no Spanner
  handler_test:  wires handler only, no middleware, no Spanner
  orchestrator_test: wires orch only, no HTTP layer, no Spanner (Repo: nil)
```

**Key structural points:**

- `cli.Run` is the only place all dependencies are wired together.
- `RequireAPIKey` returns a `(handler, stopFunc)` pair — tests that don't call `stopFunc` leak a goroutine.
- `orchestrator.GetQuotes` has two branching paths depending on whether `repo != nil`: cache lookup → singleflight → fanOut → repo.Save. These paths are not covered without a real or fake `QuoteRepository`.
- `spanneradapter.NewClient` connects to the emulator automatically when `SPANNER_EMULATOR_HOST` is set; no code changes required.
- The Quotes table DDL uses `(RequestID, CarrierID)` as PK and `QuotesByExpiry` secondary index; `DeleteExpired` uses `PartitionedUpdate` which requires a `readwrite` transaction mode.

---

## Data Flow

### POST /quotes — full stack (what integration tests must verify)

```
HTTP client
  → AuditLog middleware (wraps ResponseWriter in statusRecorder)
  → SecurityHeaders middleware (sets headers before handler runs)
  → RequireAPIKey middleware (reads Authorization: Bearer <key>; sets clientIDKey in ctx)
  → LimitConcurrency middleware (semaphore; 503 if full)
  → mux.ServeHTTP (route match: POST /quotes)
  → handler.handlePostQuotes
      ├── JSON decode + validate (request_id, coverage_lines, timeout_ms)
      ├── buildDomainRequest → domain.QuoteRequest
      ├── middleware.ClientIDFromContext (reads clientIDKey)
      └── orchestrator.GetQuotes(ctx, req)
              ├── repo.FindByRequestID (cache check) [if repo != nil]
              ├── singleflight.Do (dedup by scoped key)
              └── fanOut
                      ├── filterEligibleCarriers (capability + CB state)
                      ├── callCarrier × N (parallel: rate limiter → CB → adapter)
                      ├── hedgeMonitor goroutine
                      ├── collect + dedup + sort by premium
                      └── repo.Save [if repo != nil && results > 0]
  → JSON encode quoteResponse
  → AuditLog records status + duration
```

### Spanner repo data flow (what Spanner adapter tests must verify)

```
QuoteRepo.Save
  → spanner.InsertOrUpdateStruct("Quotes", quoteRow{...})
  → client.Apply(ctx, mutations)

QuoteRepo.FindByRequestID
  → client.Single().Query(SQL with ExpiresAt > NOW())
  → row.ToStruct(&quoteRow) → domain.QuoteResult

QuoteRepo.DeleteExpired
  → client.PartitionedUpdate("DELETE FROM Quotes WHERE ExpiresAt <= CURRENT_TIMESTAMP()")

CarrierRepo.ListActive
  → client.Single().Query(SQL WHERE IsActive = true)
  → row.Columns(&id, &name, &code, &configJSON)
  → json.Unmarshal(configJSON) → domain.CarrierConfig

AppetiteRepo.FindMatchingRules
  → dynamic SQL (state + lob required; classCode + premium optional)
  → row.Columns(8 cols including NullString, NullFloat64, NullJSON)
  → domain.AppetiteRule
```

**Critical field mismatches to verify:**
- `CarrierRepo`: column order is `CarrierId, Name, Code, Config` (line 25 of carrier_repo.go) — must match exactly.
- `AppetiteRepo`: queries 8 columns in specific order; `ClassCode` is `NullString`, `MinPremium`/`MaxPremium` are `NullFloat64`, `EligibilityCriteria` is `NullJSON`.
- `QuoteRepo.Save`: uses `spanner.CommitTimestamp` for `CreatedAt` — must use `allow_commit_timestamp = true` column option (present in DDL).

---

## Risk Assessment

| Risk | Severity | Likelihood | Notes |
|---|---|---|---|
| Spanner emulator unavailable in CI | High | High | CI has no emulator service; Spanner adapter tests will fail unless guarded by build tag or env-var skip |
| `PartitionedUpdate` not supported by emulator | Medium | Low | `PartitionedUpdate` is supported by the emulator since v1.4; verify version in docker-compose (`latest`) |
| Column order mismatch in `CarrierRepo.ListActive` | Medium | Medium | `row.Columns` is positional — query selects `CarrierId, Name, Code, Config`; must exactly match scan order |
| `CommitTimestamp` in `quoteRow.CreatedAt` | Low | Low | `spanner.CommitTimestamp` is a sentinel value; `allow_commit_timestamp = true` is in DDL; should work |
| `cli.Run` goroutine leak from `stopAuth` | Low | Low | `defer stopAuth()` is already present in production code; tests using `cli.Run` must call it or use `context.WithCancel` |
| Flaky timing in E2E startup test | Medium | Medium | Test must wait for server readiness (poll `/healthz`) rather than fixed sleep |
| `internal/domain` has no behavioral logic to unit test | Low | Low | All fields are plain Go structs; only sentinel errors and typed constants; pinning tests have low marginal value |
| antirez tests already cover some integration paths | Low | Low | `antirez_test.go` assembles handler+orch+middleware via `httptest.NewServer` but without Spanner — overlap risk with new integration tests |

---

## Approach Comparison

### Option A — Pure in-process integration tests (no emulator required)

Wire the full HTTP stack (`AuditLog → SecurityHeaders → RequireAPIKey → LimitConcurrency → mux → Handler → Orchestrator`) with mock carriers but no Spanner. Use `httptest.NewServer` (real `net.Listener`) or `httptest.NewRecorder` with the full middleware chain.

**Pros:**
- Runs in CI today with zero infrastructure changes.
- Covers the assembled middleware chain (gap not covered by existing tests).
- Fast (no I/O).

**Cons:**
- Does not cover `cli.Run` wiring itself.
- Does not cover Spanner adapter code.
- `QuoteRepository` cache paths remain untested.

### Option B — Spanner emulator tests (integration, opt-in via build tag)

Add `//go:build integration` tag to Spanner adapter tests. Tests connect to emulator via `SPANNER_EMULATOR_HOST`. CI gets a new job with `docker-compose up -d spanner-emulator spanner-init` before running `go test -tags integration ./...`.

**Pros:**
- Full Spanner CRUD coverage: `Save`, `FindByRequestID`, `DeleteExpired`, `ListActive`, `FindMatchingRules`.
- Tests real SQL, column ordering, `NullJSON` decoding, `PartitionedUpdate`.

**Cons:**
- Requires CI infrastructure changes (Docker service, wait-for-ready step).
- Slow to start emulator (~5–10 seconds).
- Emulator behavioral edge cases differ slightly from production Spanner (e.g., strong reads, some PDML constraints).

### Option C — Hybrid: in-process integration + emulator-gated Spanner tests

Implement both A and B:
- New `internal/integration/` package: full HTTP stack test with mock carriers, no Spanner; runs in standard `go test ./...`.
- New `internal/adapter/spanner/` `*_test.go` files with `//go:build integration` tag; run only when emulator is up.
- New `internal/cli/run_test.go`: lightweight startup test using `context.WithCancel` to drive shutdown; mock Spanner env vars absent (falls back to mock carriers); validates `API_KEYS` rejection path.
- Extend `.github/workflows/go.yml` with a second job using `services:` or `docker-compose` to run emulator-gated tests.

**Pros:**
- Fills every identified gap.
- Standard tests (`go test ./...`) remain fast and require no infra.
- Emulator tests run in a dedicated CI job — failures don't block the fast path.

**Cons:**
- Two CI jobs to maintain.
- Build tag discipline required (`//go:build integration` must be consistent).

---

## Recommendation

**Option C — Hybrid approach.**

Rationale:
1. The biggest unaddressed gap is the assembled middleware chain. A new `internal/integration/` package (or `internal/handler/integration_test.go`) with `httptest.NewServer` and the real middleware stack covers this with zero infra cost.
2. The Spanner adapter is completely untested. The emulator is already present in `docker-compose.yml`; the only missing piece is CI integration and test files.
3. `cli.Run` startup behavior (missing `API_KEYS`, clean shutdown) is exercisable without Spanner — tests drive `Run` directly by injecting a cancelable context.

**Proposed file structure:**

```
internal/integration/
  http_stack_test.go       # full middleware chain, mock carriers, httptest.NewServer
internal/adapter/spanner/
  quote_repo_test.go       # //go:build integration — Save/FindByRequestID/DeleteExpired
  carrier_repo_test.go     # //go:build integration — ListActive, JSON config decoding
  appetite_repo_test.go    # //go:build integration — FindMatchingRules (with/without classCode, premium)
internal/cli/
  run_test.go              # startup lifecycle: API_KEYS missing, clean shutdown via ctx cancel
internal/domain/
  errors_test.go           # optional: pin sentinel error identities with errors.Is
.github/workflows/go.yml   # add integration job with Spanner emulator service
Makefile                   # add `test-integration` target
```

**Key implementation notes:**

- `internal/integration/` is a new package, not `_test.go` under `cli` or `handler`, to avoid import cycles.
- Emulator test helper: a `testutil/spanner.go` (build-tag-gated) that calls `spanneradapter.NewClient` with env vars `SPANNER_EMULATOR_HOST`, `SPANNER_PROJECT=riskforge-dev`, `SPANNER_INSTANCE=test-instance`, `SPANNER_DATABASE=test-db`, then uses the DDL from `docker-compose.yml` or connects to a pre-initialized emulator.
- Integration HTTP tests must call `stopAuth()` returned by `middleware.RequireAPIKey` to avoid goroutine leaks in `-race` mode.
- `cli.Run` test: set `API_KEYS=""` → assert `Run` returns non-nil error immediately (no server starts). For clean shutdown: set env vars, call `Run` in a goroutine, poll `/healthz`, then cancel context, assert `Run` returns nil.
- Domain unit tests: `errors.Is` checks for each sentinel; `CoverageLine` constant values; `Money.Amount` zero value. Low value — defer or skip.

---

## Clarification Required (BLOCKING)

None. All information needed to write the proposal is available:
- Test gap inventory is complete.
- Emulator infrastructure is already present.
- Build tag strategy (`//go:build integration`) is standard Go practice.
- Port interfaces are well-defined; test doubles are straightforward.

---

## Open Questions (DEFERRED)

1. **Emulator version pinning.** `docker-compose.yml` uses `gcr.io/cloud-spanner-emulator/emulator:latest`. Should CI pin a specific version tag to prevent surprise breakage? Low urgency — can be addressed in the proposal or as a follow-up.

2. **Domain unit tests scope.** The domain package has no behavior beyond typed constants and sentinel errors. Is pinning these (e.g., `errors.Is(domain.ErrCircuitOpen, domain.ErrCircuitOpen)`) worth the file, or should the domain package remain test-free? Recommend deferring — the proposal can omit domain tests unless the reviewer requests them.

3. **`cli.Run` test isolation strategy.** `cli.Run` reads multiple env vars (`PORT`, `API_KEYS`, `SPANNER_PROJECT`, etc.) and calls `signal.NotifyContext`. Testing this cleanly requires either injecting env vars via `t.Setenv` (safe, reverted after test) or refactoring `Run` to accept a config struct. The current signature `Run(ctx, args, stdout, stderr)` is testable as-is using `t.Setenv` — no refactoring required. Confirm this approach is acceptable before the proposal stage.

4. **`antirez` overlap.** `internal/antirez/antirez_test.go` already assembles handler + orchestrator + all middleware via `httptest.NewServer` (adversarial focus). New integration tests should complement (different assertions: status codes, response body structure, security headers) rather than duplicate. Delineation should be made explicit in the proposal.

5. **Coverage target.** CI currently reports total coverage but does not enforce a minimum threshold. Should the integration work introduce a coverage gate (e.g., `COVERAGE >= 80%`)? Deferred to proposal.

# Delta Spec: Integration and E2E Testing

**Change**: integration-e2e-testing
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

This change adds integration and E2E tests to close three hard coverage gaps in riskforge: (1) the fully assembled HTTP middleware chain (`AuditLog -> SecurityHeaders -> RequireAPIKey -> LimitConcurrency -> mux -> Handler -> Orchestrator`), (2) the Spanner adapter repositories (`QuoteRepo`, `CarrierRepo`, `AppetiteRepo`) which have zero test files, and (3) the `cli.Run` startup/shutdown lifecycle which also has zero test files. No existing specs exist -- all requirements are ADDED.

---

## ADDED Requirements

---

### Domain: http-stack

### REQ-HTTP-STACK-001: Authenticated POST /quotes Returns Valid JSON Response

The test suite **MUST** wire the full production middleware chain identically to `cli.Run` (lines 178-184 of `internal/cli/run.go`): `AuditLog -> SecurityHeaders -> RequireAPIKey -> LimitConcurrency -> mux -> Handler -> Orchestrator`, using `httptest.NewServer` with a real `net.Listener` and mock carriers. An authenticated `POST /quotes` with a valid body **MUST** return HTTP 200 with `Content-Type: application/json` and a non-empty `quotes` array in the response body.

#### Scenario: Happy path POST /quotes with valid auth and body · `code-based` · `critical`

- **WHEN** a `POST /quotes` request is sent to the assembled stack with header `Authorization: Bearer test-key-abc` and body `{"request_id":"integ-001","coverage_lines":["auto"],"timeout_ms":5000}`
- **THEN** the response status code is `200`, the `Content-Type` header is `application/json`, and the decoded JSON body contains a `quotes` array with length >= 1 where each element has non-empty `carrier_id` and `premium_cents` > 0

#### Scenario: Response includes request_id and duration_ms fields · `code-based` · `critical`

- **WHEN** a `POST /quotes` request is sent with `request_id` set to `"integ-002"` and valid auth
- **THEN** the decoded JSON response has `request_id` equal to `"integ-002"` and `duration_ms` >= 0

---

### REQ-HTTP-STACK-002: Unauthenticated Requests to /quotes Return 401

The middleware chain **MUST** reject requests to protected endpoints that lack a valid `Authorization: Bearer <key>` header with HTTP 401 and a JSON error body.

#### Scenario: Missing Authorization header returns 401 · `code-based` · `critical`

- **WHEN** a `POST /quotes` request is sent to the assembled stack with no `Authorization` header and body `{"request_id":"integ-003","coverage_lines":["auto"]}`
- **THEN** the response status code is `401` and the body contains `"UNAUTHORIZED"`

#### Scenario: Invalid Bearer token returns 401 · `code-based` · `critical`

- **WHEN** a `POST /quotes` request is sent with header `Authorization: Bearer wrong-key-999` where `wrong-key-999` is not in the configured API keys
- **THEN** the response status code is `401` and the body contains `"UNAUTHORIZED"`

---

### REQ-HTTP-STACK-003: SecurityHeaders Middleware Sets All Required Headers

The `SecurityHeaders` middleware **MUST** set the five security headers defined in `internal/middleware/security.go` on every response. The test **MUST** verify the actual header values from the source code: `X-Content-Type-Options: nosniff`, `Strict-Transport-Security: max-age=63072000; includeSubDomains`, `X-Frame-Options: DENY`, `Cache-Control: no-store`, `Content-Security-Policy: default-src 'none'`.

Note: The proposal mentions `Referrer-Policy: no-referrer` but this header is NOT set by `SecurityHeaders` in the actual source (`internal/middleware/security.go` lines 7-15). Specs align with source code, not the proposal narrative.

#### Scenario: All five security headers present on authenticated request · `code-based` · `critical`

- **WHEN** a `POST /quotes` request is sent with valid auth and body to the assembled stack
- **THEN** the response includes headers: `X-Content-Type-Options` = `nosniff`, `X-Frame-Options` = `DENY`, `Strict-Transport-Security` = `max-age=63072000; includeSubDomains`, `Cache-Control` = `no-store`, `Content-Security-Policy` = `default-src 'none'`

#### Scenario: Security headers present even on error responses · `code-based` · `critical`

- **WHEN** a `POST /quotes` request is sent with valid auth but an empty body (triggering a 400)
- **THEN** the response status code is `400` AND the `X-Content-Type-Options` header is `nosniff` AND the `X-Frame-Options` header is `DENY`

---

### REQ-HTTP-STACK-004: LimitConcurrency Returns 503 When Semaphore Is Full

The `LimitConcurrency` middleware **MUST** return HTTP 503 with `Retry-After: 1` header when the configured concurrency limit is reached. The test **MUST** fill the semaphore to capacity, then issue one additional request and verify the 503 response.

#### Scenario: Overflow request receives 503 when semaphore is full · `code-based` · `critical`

- **GIVEN** the concurrency limit is set to `N` (e.g., 2) and `N` goroutines each hold a slot by making requests to a handler that blocks until signaled
- **WHEN** one additional `POST /quotes` request is sent with valid auth
- **THEN** the response status code is `503` and the `Retry-After` header is `"1"` and the body contains `"SERVICE_UNAVAILABLE"`

#### Scenario: Requests succeed after semaphore slots are released · `code-based` · `standard`

- **GIVEN** the concurrency limit is `2` and both slots were previously full but one blocking request has completed
- **WHEN** a new `POST /quotes` request is sent with valid auth and valid body
- **THEN** the response status code is `200`

---

### REQ-HTTP-STACK-005: Health, Readiness, and Metrics Endpoints Bypass Auth

The paths `/healthz`, `/readyz`, and `/metrics` **MUST** be accessible without an `Authorization` header, as configured by the `skipPaths` parameter in `RequireAPIKey` (line 176 of `internal/cli/run.go`).

#### Scenario: GET /healthz returns 200 without auth · `code-based` · `critical`

- **WHEN** a `GET /healthz` request is sent to the assembled stack with no `Authorization` header
- **THEN** the response status code is `200` and the body is `"ok"`

#### Scenario: GET /readyz returns 200 without auth · `code-based` · `critical`

- **WHEN** a `GET /readyz` request is sent to the assembled stack with no `Authorization` header
- **THEN** the response status code is `200` and the body is `"ok"`

#### Scenario: GET /metrics returns 200 without auth · `code-based` · `critical`

- **WHEN** a `GET /metrics` request is sent to the assembled stack with no `Authorization` header
- **THEN** the response status code is `200`

---

### REQ-HTTP-STACK-006: Test Cleanup Prevents Goroutine Leaks

Each HTTP stack test **MUST** call `t.Cleanup(srv.Close)` for the `httptest.Server` and `t.Cleanup(stopAuth)` for the auth middleware's cleanup function. The test binary **MUST** pass with `-race` flag without goroutine leak warnings.

#### Scenario: stopAuth is called on test cleanup · `code-based` · `critical`

- **WHEN** an HTTP stack test completes (pass or fail)
- **THEN** `stopAuth()` returned by `middleware.RequireAPIKey` has been invoked via `t.Cleanup`, and the auth failure limiter background goroutine is stopped

#### Scenario: Race detector passes on HTTP stack tests · `code-based` · `critical`

- **WHEN** `go test -race ./internal/integration/...` is executed
- **THEN** the test binary exits with code 0 and no race conditions are reported

---

### Domain: spanner-quote-repo

### REQ-QUOTE-REPO-001: Save and FindByRequestID Round-Trip

The `QuoteRepo` integration tests **MUST** use the `//go:build integration` build tag and connect to the Spanner emulator via `newEmulatorClient(t)`. `QuoteRepo.Save` followed by `QuoteRepo.FindByRequestID` for the same `requestID` **MUST** return the saved results with `found == true`, correct `PremiumCents` values, and results ordered by `PremiumCents ASC`.

#### Scenario: Save two results then find both by requestID · `code-based` · `critical`

- **WHEN** `QuoteRepo.Save(ctx, "req-save-001", results)` is called with two `domain.QuoteResult` entries having `PremiumCents` 5000 and 3000, then `QuoteRepo.FindByRequestID(ctx, "req-save-001")` is called
- **THEN** `found` is `true`, `len(results)` is `2`, `results[0].Premium.Amount` is `3000` (lowest first), and `results[1].Premium.Amount` is `5000`

#### Scenario: FindByRequestID for unknown ID returns not found · `code-based` · `critical`

- **WHEN** `QuoteRepo.FindByRequestID(ctx, "nonexistent-req-999")` is called without any prior `Save` for that ID
- **THEN** `found` is `false`, `results` is `nil`, and `err` is `nil`

---

### REQ-QUOTE-REPO-002: FindByRequestID Filters Expired Rows

`QuoteRepo.FindByRequestID` **MUST** only return rows where `ExpiresAt > CURRENT_TIMESTAMP()` as specified by the SQL in `internal/adapter/spanner/quote_repo.go` line 53. Rows with `ExpiresAt` in the past **MUST NOT** be returned.

#### Scenario: Expired row is not returned by FindByRequestID · `code-based` · `critical`

- **GIVEN** a quote row exists in the `Quotes` table with `RequestID = "req-expired-001"` and `ExpiresAt` set to `2020-01-01T00:00:00Z` (in the past), inserted via a direct Spanner mutation to bypass `CommitTimestamp` constraints
- **WHEN** `QuoteRepo.FindByRequestID(ctx, "req-expired-001")` is called
- **THEN** `found` is `false` and `results` is `nil`

#### Scenario: Mix of expired and valid rows returns only valid · `code-based` · `critical`

- **GIVEN** two quote rows exist for `RequestID = "req-mixed-001"`: one with `ExpiresAt` 1 hour in the future and one with `ExpiresAt` in the past
- **WHEN** `QuoteRepo.FindByRequestID(ctx, "req-mixed-001")` is called
- **THEN** `found` is `true` and `len(results)` is `1`

---

### REQ-QUOTE-REPO-003: DeleteExpired Removes Expired Rows

`QuoteRepo.DeleteExpired` **MUST** delete all rows where `ExpiresAt <= CURRENT_TIMESTAMP()` using `PartitionedUpdate` and return the count of deleted rows. After deletion, `FindByRequestID` for the deleted request **MUST** return `found == false`.

#### Scenario: DeleteExpired removes expired row and returns count >= 1 · `code-based` · `critical`

- **GIVEN** a quote row exists with `RequestID = "req-del-001"` and `ExpiresAt` in the past (inserted via direct Spanner mutation)
- **WHEN** `QuoteRepo.DeleteExpired(ctx)` is called
- **THEN** the returned `count` is >= 1 and `err` is `nil`

#### Scenario: FindByRequestID misses after DeleteExpired · `code-based` · `critical`

- **GIVEN** `DeleteExpired` was called and removed the row for `RequestID = "req-del-001"`
- **WHEN** `QuoteRepo.FindByRequestID(ctx, "req-del-001")` is called
- **THEN** `found` is `false`

---

### REQ-QUOTE-REPO-004: Emulator Absence Causes Graceful Skip

When the Spanner emulator is not available, integration tests **MUST** skip gracefully rather than fail. The `newEmulatorClient(t)` helper **MUST** call `t.Skip` when `SPANNER_EMULATOR_HOST` is unset.

#### Scenario: SPANNER_EMULATOR_HOST unset causes t.Skip · `code-based` · `critical`

- **GIVEN** the environment variable `SPANNER_EMULATOR_HOST` is empty or unset
- **WHEN** `newEmulatorClient(t)` is called
- **THEN** `t.Skip` is invoked with a message containing `"SPANNER_EMULATOR_HOST not set"` and no test failure is recorded

#### Scenario: SPANNER_EMULATOR_HOST set returns valid client · `code-based` · `critical`

- **GIVEN** the environment variable `SPANNER_EMULATOR_HOST` is set to `localhost:9010` and the emulator is running
- **WHEN** `newEmulatorClient(t)` is called
- **THEN** the returned `*spanner.Client` is non-nil and `t.Cleanup` has been registered to call `client.Close()`

---

### Domain: spanner-carrier-repo

### REQ-CARRIER-REPO-001: ListActive Returns Only Active Carriers

`CarrierRepo.ListActive` **MUST** return only rows where `IsActive = true` from the `Carriers` table. Inactive carriers **MUST NOT** appear in the result.

#### Scenario: ListActive filters inactive carriers · `code-based` · `critical`

- **GIVEN** the `Carriers` table contains 3 rows: `carrier-a` (IsActive=true), `carrier-b` (IsActive=true), `carrier-c` (IsActive=false), inserted via direct Spanner mutations
- **WHEN** `CarrierRepo.ListActive(ctx)` is called
- **THEN** the returned slice has length `2` and contains only carriers with IDs `carrier-a` and `carrier-b`

#### Scenario: ListActive with all inactive returns empty slice · `code-based` · `standard`

- **GIVEN** only inactive carrier rows exist in the `Carriers` table
- **WHEN** `CarrierRepo.ListActive(ctx)` is called
- **THEN** the returned slice is empty (length 0) and `err` is `nil`

---

### REQ-CARRIER-REPO-002: ListActive Decodes NullJSON Config to domain.CarrierConfig

`CarrierRepo.ListActive` **MUST** correctly decode the `Config` column (Spanner `JSON` type read as `spanner.NullJSON`) into `domain.CarrierConfig` via `json.Marshal` -> `json.Unmarshal`. The positional column ordering `CarrierId, Name, Code, Config` in the SQL SELECT (line 25 of `internal/adapter/spanner/carrier_repo.go`) **MUST** match the `row.Columns(&id, &name, &code, &configJSON)` scan order (line 34).

#### Scenario: JSON Config round-trip preserves field values · `code-based` · `critical`

- **GIVEN** the `Carriers` table has one active row with `CarrierId = "carrier-cfg"`, `Name = "Config Test"`, `Code = "CFG"`, and `Config` containing JSON `{"TimeoutHint": 150000000, "FailureThreshold": 3, "RateLimit": {"TokensPerSecond": 50, "Burst": 5}}`
- **WHEN** `CarrierRepo.ListActive(ctx)` is called
- **THEN** the returned carrier has `Config.TimeoutHint` equal to `150ms`, `Config.FailureThreshold` equal to `3`, and `Config.RateLimit.TokensPerSecond` equal to `50`

#### Scenario: NULL Config column yields zero-value CarrierConfig · `code-based` · `critical`

- **GIVEN** the `Carriers` table has one active row with `Config = NULL`
- **WHEN** `CarrierRepo.ListActive(ctx)` is called
- **THEN** the returned carrier has `Config` equal to the zero value of `domain.CarrierConfig{}` (all fields are zero/default)

---

### Domain: spanner-appetite-repo

### REQ-APPETITE-REPO-001: FindMatchingRules Filters by Required Fields (State + LOB)

`AppetiteRepo.FindMatchingRules` **MUST** filter by `State` and `LineOfBusiness` as required parameters. Only rules matching both fields **MUST** be returned.

#### Scenario: Match by state and LOB returns correct rules · `code-based` · `critical`

- **GIVEN** the `AppetiteRules` table has two active rules: rule-1 with `State="CA"`, `LineOfBusiness="auto"` and rule-2 with `State="NY"`, `LineOfBusiness="auto"`
- **WHEN** `AppetiteRepo.FindMatchingRules(ctx, domain.RiskClassification{State: "CA", LineOfBusiness: "auto"})` is called
- **THEN** the returned slice has length `1` and `rules[0].State` is `"CA"`

#### Scenario: No matching rules returns empty slice and nil error · `code-based` · `critical`

- **WHEN** `AppetiteRepo.FindMatchingRules(ctx, domain.RiskClassification{State: "ZZ", LineOfBusiness: "nonexistent"})` is called with no seeded data for that state/LOB combination
- **THEN** the returned slice is empty (length 0) and `err` is `nil`

---

### REQ-APPETITE-REPO-002: FindMatchingRules Treats NULL ClassCode as Wildcard

When `risk.ClassCode` is provided, `FindMatchingRules` **MUST** include rules where `ClassCode IS NULL` (wildcard) in addition to rules where `ClassCode` matches the provided value, per the SQL clause `ClassCode IS NULL OR ClassCode = @classCode` at line 33 of `internal/adapter/spanner/appetite_repo.go`.

#### Scenario: NULL ClassCode rule matches any classCode query · `code-based` · `critical`

- **GIVEN** two active rules exist: rule-cc1 with `State="CA"`, `LineOfBusiness="auto"`, `ClassCode="auto-commercial"` and rule-cc2 with `State="CA"`, `LineOfBusiness="auto"`, `ClassCode=NULL`
- **WHEN** `FindMatchingRules(ctx, domain.RiskClassification{State: "CA", LineOfBusiness: "auto", ClassCode: "auto-commercial"})` is called
- **THEN** the returned slice has length `2` (both rules match — one by exact ClassCode, one by NULL wildcard)

#### Scenario: Query without classCode skips ClassCode filter entirely · `code-based` · `critical`

- **GIVEN** an active rule exists with `State="CA"`, `LineOfBusiness="auto"`, `ClassCode="auto-commercial"`
- **WHEN** `FindMatchingRules(ctx, domain.RiskClassification{State: "CA", LineOfBusiness: "auto", ClassCode: ""})` is called (empty ClassCode)
- **THEN** the returned slice has length `1` (the ClassCode filter clause is not appended to the SQL when `ClassCode == ""`)

---

### REQ-APPETITE-REPO-003: FindMatchingRules Applies Premium Range Filter

When `risk.EstimatedPremium > 0`, `FindMatchingRules` **MUST** add the premium range filter: `(MinPremium IS NULL OR MinPremium <= @premium) AND (MaxPremium IS NULL OR MaxPremium >= @premium)` per lines 38-39 of `internal/adapter/spanner/appetite_repo.go`.

#### Scenario: Premium within range matches · `code-based` · `critical`

- **GIVEN** an active rule exists with `State="CA"`, `LineOfBusiness="auto"`, `MinPremium=1000`, `MaxPremium=5000`
- **WHEN** `FindMatchingRules(ctx, domain.RiskClassification{State: "CA", LineOfBusiness: "auto", EstimatedPremium: 3000})` is called
- **THEN** the returned slice has length `1`

#### Scenario: Premium outside range does not match · `code-based` · `critical`

- **GIVEN** the same rule as above with `MinPremium=1000`, `MaxPremium=5000`
- **WHEN** `FindMatchingRules(ctx, domain.RiskClassification{State: "CA", LineOfBusiness: "auto", EstimatedPremium: 6000})` is called
- **THEN** the returned slice is empty (length 0) and `err` is `nil`

---

### REQ-APPETITE-REPO-004: ListAll Returns All Active Rules

`AppetiteRepo.ListAll` **MUST** return all rows from `AppetiteRules` where `IsActive = true`, regardless of state, LOB, or other filters.

#### Scenario: ListAll returns all active rules across states · `code-based` · `critical`

- **GIVEN** the `AppetiteRules` table has 3 active rules across states `"CA"`, `"NY"`, `"TX"`
- **WHEN** `AppetiteRepo.ListAll(ctx)` is called
- **THEN** the returned slice has length `3`

#### Scenario: ListAll excludes inactive rules · `code-based` · `standard`

- **GIVEN** the `AppetiteRules` table has 2 active rules and 1 inactive rule
- **WHEN** `AppetiteRepo.ListAll(ctx)` is called
- **THEN** the returned slice has length `2`

---

### Domain: cli-lifecycle

### REQ-CLI-LIFECYCLE-001: Missing API_KEYS Returns Error Immediately

`cli.Run` **MUST** return a non-nil error immediately (without starting the HTTP server) when the `API_KEYS` environment variable is empty or unset. The error message **MUST** contain the string `"API_KEYS environment variable required"` as specified at line 42 of `internal/cli/run.go`.

#### Scenario: Empty API_KEYS returns error without blocking · `code-based` · `critical`

- **WHEN** `cli.Run(ctx, nil, io.Discard, io.Discard)` is called with `API_KEYS` set to `""` via `t.Setenv`
- **THEN** the returned error is non-nil, `err.Error()` contains `"API_KEYS environment variable required"`, and the call returns within 1 second (no blocking on server listen)

#### Scenario: Unset API_KEYS returns same error · `code-based` · `critical`

- **GIVEN** the `API_KEYS` environment variable is not set at all (default empty)
- **WHEN** `cli.Run(ctx, nil, io.Discard, io.Discard)` is called
- **THEN** the returned error is non-nil and contains `"API_KEYS environment variable required"`

---

### REQ-CLI-LIFECYCLE-002: Clean Shutdown via Context Cancellation

`cli.Run` **MUST** start an HTTP server that responds on `/healthz` and **MUST** return `nil` when the parent context is cancelled. The test **MUST** use an ephemeral port obtained via `net.Listen("tcp", ":0")` to avoid port collisions, and **MUST** poll `/healthz` for readiness rather than using a fixed sleep.

#### Scenario: Server starts and responds to /healthz then shuts down cleanly · `code-based` · `critical`

- **GIVEN** `API_KEYS` is set to `"test-key-abc"` and `PORT` is set to an OS-assigned ephemeral port via `t.Setenv`
- **WHEN** `cli.Run(ctx, nil, io.Discard, io.Discard)` is called in a goroutine, `/healthz` is polled until HTTP 200 is received (within 2 seconds), and then the context is cancelled
- **THEN** `cli.Run` returns `nil` within 5 seconds

#### Scenario: Server does not block indefinitely after context cancel · `code-based` · `critical`

- **GIVEN** `cli.Run` is running in a goroutine with a valid `API_KEYS` and ephemeral `PORT`
- **WHEN** the parent `context.Context` is cancelled
- **THEN** the goroutine returns (does not hang) because `<-ctx.Done()` fires at `internal/cli/run.go` line 204 even though `signal.NotifyContext` wraps the context at line 45

---

### Domain: ci-integration

### REQ-CI-INTEGRATION-001: Integration Tests CI Job Uses Spanner Emulator

The `.github/workflows/go.yml` workflow **MUST** include a new `integration-tests` job that starts the Spanner emulator via `docker compose up -d spanner-emulator spanner-init`, waits for the emulator to be ready, and runs `go test -tags integration -race -count=1 ./internal/adapter/spanner/...` with the correct environment variables.

#### Scenario: CI job sets required environment variables · `code-based` · `critical`

- **WHEN** the `integration-tests` job runs
- **THEN** the `go test` step has environment variables `SPANNER_EMULATOR_HOST=localhost:9010`, `SPANNER_PROJECT=riskforge-dev`, `SPANNER_INSTANCE=test-instance`, `SPANNER_DATABASE=test-db`

#### Scenario: CI job is independent of build-and-test job · `code-based` · `critical`

- **WHEN** the `integration-tests` job is defined in the workflow
- **THEN** it does not have a `needs:` dependency on the `build-and-test` job (they run in parallel), so a Spanner emulator failure does not block the fast-path unit test job

---

### REQ-CI-INTEGRATION-002: Makefile Includes test-integration Target

The `Makefile` **MUST** include a `test-integration` target that starts the Spanner emulator via `docker compose up -d` and runs `go test -tags integration -race -count=1 ./internal/adapter/spanner/...`.

#### Scenario: make test-integration runs integration tests · `code-based` · `standard`

- **WHEN** `make test-integration` is invoked
- **THEN** it executes `docker compose up -d` followed by `go test -tags integration -race -count=1 ./internal/adapter/spanner/...`

#### Scenario: make test-integration is additive to existing test target · `code-based` · `standard`

- **WHEN** the `Makefile` is inspected
- **THEN** the existing `test` target (`go test -race -count=1 ./...`) is unchanged and `test-integration` is a separate target

---

### REQ-CI-INTEGRATION-003: Integration Test Build Tag Isolation

All Spanner adapter test files **MUST** use `//go:build integration` as the first line. These tests **MUST NOT** run during `go test ./...` (without the `-tags integration` flag). Standard test suite (`go test -race -count=1 ./...`) **MUST** continue to pass with zero new failures.

#### Scenario: go test ./... does not execute Spanner integration tests · `code-based` · `critical`

- **WHEN** `go test ./...` is run without `-tags integration`
- **THEN** no test functions from `internal/adapter/spanner/*_test.go` are executed (they are excluded by the build tag)

#### Scenario: go test -tags integration includes Spanner tests · `code-based` · `critical`

- **GIVEN** the Spanner emulator is running and `SPANNER_EMULATOR_HOST` is set
- **WHEN** `go test -tags integration -race -count=1 ./internal/adapter/spanner/...` is run
- **THEN** all `Test*` functions in `quote_repo_test.go`, `carrier_repo_test.go`, and `appetite_repo_test.go` are executed and pass

---

## Acceptance Criteria Summary

| Requirement ID | Type | Priority | Scenarios |
|---|---|---|---|
| REQ-HTTP-STACK-001 | ADDED | MUST | 2 |
| REQ-HTTP-STACK-002 | ADDED | MUST | 2 |
| REQ-HTTP-STACK-003 | ADDED | MUST | 2 |
| REQ-HTTP-STACK-004 | ADDED | MUST | 2 |
| REQ-HTTP-STACK-005 | ADDED | MUST | 3 |
| REQ-HTTP-STACK-006 | ADDED | MUST | 2 |
| REQ-QUOTE-REPO-001 | ADDED | MUST | 2 |
| REQ-QUOTE-REPO-002 | ADDED | MUST | 2 |
| REQ-QUOTE-REPO-003 | ADDED | MUST | 2 |
| REQ-QUOTE-REPO-004 | ADDED | MUST | 2 |
| REQ-CARRIER-REPO-001 | ADDED | MUST | 2 |
| REQ-CARRIER-REPO-002 | ADDED | MUST | 2 |
| REQ-APPETITE-REPO-001 | ADDED | MUST | 2 |
| REQ-APPETITE-REPO-002 | ADDED | MUST | 2 |
| REQ-APPETITE-REPO-003 | ADDED | MUST | 2 |
| REQ-APPETITE-REPO-004 | ADDED | MUST | 2 |
| REQ-CLI-LIFECYCLE-001 | ADDED | MUST | 2 |
| REQ-CLI-LIFECYCLE-002 | ADDED | MUST | 2 |
| REQ-CI-INTEGRATION-001 | ADDED | MUST | 2 |
| REQ-CI-INTEGRATION-002 | ADDED | MUST | 2 |
| REQ-CI-INTEGRATION-003 | ADDED | MUST | 2 |

**Total Requirements**: 21
**Total Scenarios**: 43

---

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|---|---|---|---|
| REQ-HTTP-STACK-001 > Happy path POST /quotes with valid auth and body | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-001 > Response includes request_id and duration_ms fields | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-002 > Missing Authorization header returns 401 | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-002 > Invalid Bearer token returns 401 | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-003 > All five security headers present on authenticated request | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-003 > Security headers present even on error responses | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-004 > Overflow request receives 503 when semaphore is full | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-004 > Requests succeed after semaphore slots are released | code-based | standard | pass@3 >= 0.90 |
| REQ-HTTP-STACK-005 > GET /healthz returns 200 without auth | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-005 > GET /readyz returns 200 without auth | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-005 > GET /metrics returns 200 without auth | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-006 > stopAuth is called on test cleanup | code-based | critical | pass^3 = 1.00 |
| REQ-HTTP-STACK-006 > Race detector passes on HTTP stack tests | code-based | critical | pass^3 = 1.00 |
| REQ-QUOTE-REPO-001 > Save two results then find both by requestID | code-based | critical | pass^3 = 1.00 |
| REQ-QUOTE-REPO-001 > FindByRequestID for unknown ID returns not found | code-based | critical | pass^3 = 1.00 |
| REQ-QUOTE-REPO-002 > Expired row is not returned by FindByRequestID | code-based | critical | pass^3 = 1.00 |
| REQ-QUOTE-REPO-002 > Mix of expired and valid rows returns only valid | code-based | critical | pass^3 = 1.00 |
| REQ-QUOTE-REPO-003 > DeleteExpired removes expired row and returns count >= 1 | code-based | critical | pass^3 = 1.00 |
| REQ-QUOTE-REPO-003 > FindByRequestID misses after DeleteExpired | code-based | critical | pass^3 = 1.00 |
| REQ-QUOTE-REPO-004 > SPANNER_EMULATOR_HOST unset causes t.Skip | code-based | critical | pass^3 = 1.00 |
| REQ-QUOTE-REPO-004 > SPANNER_EMULATOR_HOST set returns valid client | code-based | critical | pass^3 = 1.00 |
| REQ-CARRIER-REPO-001 > ListActive filters inactive carriers | code-based | critical | pass^3 = 1.00 |
| REQ-CARRIER-REPO-001 > ListActive with all inactive returns empty slice | code-based | standard | pass@3 >= 0.90 |
| REQ-CARRIER-REPO-002 > JSON Config round-trip preserves field values | code-based | critical | pass^3 = 1.00 |
| REQ-CARRIER-REPO-002 > NULL Config column yields zero-value CarrierConfig | code-based | critical | pass^3 = 1.00 |
| REQ-APPETITE-REPO-001 > Match by state and LOB returns correct rules | code-based | critical | pass^3 = 1.00 |
| REQ-APPETITE-REPO-001 > No matching rules returns empty slice and nil error | code-based | critical | pass^3 = 1.00 |
| REQ-APPETITE-REPO-002 > NULL ClassCode rule matches any classCode query | code-based | critical | pass^3 = 1.00 |
| REQ-APPETITE-REPO-002 > Query without classCode skips ClassCode filter entirely | code-based | critical | pass^3 = 1.00 |
| REQ-APPETITE-REPO-003 > Premium within range matches | code-based | critical | pass^3 = 1.00 |
| REQ-APPETITE-REPO-003 > Premium outside range does not match | code-based | critical | pass^3 = 1.00 |
| REQ-APPETITE-REPO-004 > ListAll returns all active rules across states | code-based | critical | pass^3 = 1.00 |
| REQ-APPETITE-REPO-004 > ListAll excludes inactive rules | code-based | standard | pass@3 >= 0.90 |
| REQ-CLI-LIFECYCLE-001 > Empty API_KEYS returns error without blocking | code-based | critical | pass^3 = 1.00 |
| REQ-CLI-LIFECYCLE-001 > Unset API_KEYS returns same error | code-based | critical | pass^3 = 1.00 |
| REQ-CLI-LIFECYCLE-002 > Server starts and responds to /healthz then shuts down cleanly | code-based | critical | pass^3 = 1.00 |
| REQ-CLI-LIFECYCLE-002 > Server does not block indefinitely after context cancel | code-based | critical | pass^3 = 1.00 |
| REQ-CI-INTEGRATION-001 > CI job sets required environment variables | code-based | critical | pass^3 = 1.00 |
| REQ-CI-INTEGRATION-001 > CI job is independent of build-and-test job | code-based | critical | pass^3 = 1.00 |
| REQ-CI-INTEGRATION-002 > make test-integration runs integration tests | code-based | standard | pass@3 >= 0.90 |
| REQ-CI-INTEGRATION-002 > make test-integration is additive to existing test target | code-based | standard | pass@3 >= 0.90 |
| REQ-CI-INTEGRATION-003 > go test ./... does not execute Spanner integration tests | code-based | critical | pass^3 = 1.00 |
| REQ-CI-INTEGRATION-003 > go test -tags integration includes Spanner tests | code-based | critical | pass^3 = 1.00 |

# Implementation Tasks: Merge Carrier Gateway

**Change**: merge-carrier-gateway
**Date**: 2026-03-22T20:00:00Z
**Status**: draft
**Depends On**: propose.md, spec.md, design.md

---

## Phase 1: Copy Portable Code (3,597 LOC)

Copy all self-contained packages from `/tmp/carrier-gateway/internal/` to `/home/reche/projects/ProyectoAgentero/internal/`, rewriting import paths from `github.com/rechedev9/carrier-gateway` to `github.com/rechedev9/riskforge`.

- [ ] 1.1 Copy domain package

**Source files**:
- `/tmp/carrier-gateway/internal/domain/carrier.go`
- `/tmp/carrier-gateway/internal/domain/quote.go`
- `/tmp/carrier-gateway/internal/domain/errors.go`

**Target**: `internal/domain/`

**Actions**:
1. Create `internal/domain/` directory
2. Copy all 3 files
3. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in all imports (domain has no internal imports -- just verify no module reference in package doc)

**Verify**: `go build ./internal/domain/`

- [ ] 1.2 Copy ports package

**Source files**:
- `/tmp/carrier-gateway/internal/ports/quote_port.go`
- `/tmp/carrier-gateway/internal/ports/repository_port.go`
- `/tmp/carrier-gateway/internal/ports/metrics_port.go`

**Target**: `internal/ports/`

**Actions**:
1. Create `internal/ports/` directory
2. Copy all 3 files
3. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports

**Verify**: `go build ./internal/ports/`

- [ ] 1.3 Copy adapter package (core files)

**Source files**:
- `/tmp/carrier-gateway/internal/adapter/adapter.go`
- `/tmp/carrier-gateway/internal/adapter/adapter_test.go`
- `/tmp/carrier-gateway/internal/adapter/mock_carrier.go`
- `/tmp/carrier-gateway/internal/adapter/mock_carrier_test.go`
- `/tmp/carrier-gateway/internal/adapter/http_carrier.go`
- `/tmp/carrier-gateway/internal/adapter/http_carrier_test.go`
- `/tmp/carrier-gateway/internal/adapter/delta_carrier.go`

**Target**: `internal/adapter/`

**Actions**:
1. Create `internal/adapter/` directory
2. Copy all 7 files
3. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports

**Verify**: `go build ./internal/adapter/`

- [ ] 1.4 Copy circuitbreaker package

**Source files**:
- `/tmp/carrier-gateway/internal/circuitbreaker/breaker.go`
- `/tmp/carrier-gateway/internal/circuitbreaker/breaker_test.go`

**Target**: `internal/circuitbreaker/`

**Actions**:
1. Create `internal/circuitbreaker/` directory
2. Copy both files
3. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports

**Verify**: `go test ./internal/circuitbreaker/`

- [ ] 1.5 Copy ratelimiter package

**Source files**:
- `/tmp/carrier-gateway/internal/ratelimiter/limiter.go`
- `/tmp/carrier-gateway/internal/ratelimiter/limiter_test.go`

**Target**: `internal/ratelimiter/`

**Actions**:
1. Create `internal/ratelimiter/` directory
2. Copy both files
3. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports

**Verify**: `go test ./internal/ratelimiter/`

- [ ] 1.6 Copy metrics package

**Source files**:
- `/tmp/carrier-gateway/internal/metrics/prometheus.go`
- `/tmp/carrier-gateway/internal/metrics/prometheus_test.go`

**Target**: `internal/metrics/`

**Actions**:
1. Create `internal/metrics/` directory
2. Copy both files
3. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports

**Verify**: `go test ./internal/metrics/`

- [ ] 1.7 Copy middleware package

**Source files**:
- `/tmp/carrier-gateway/internal/middleware/auth.go`
- `/tmp/carrier-gateway/internal/middleware/audit.go`
- `/tmp/carrier-gateway/internal/middleware/concurrency.go`
- `/tmp/carrier-gateway/internal/middleware/security.go`
- `/tmp/carrier-gateway/internal/middleware/middleware_test.go`

**Target**: `internal/middleware/`

**Actions**:
1. Create `internal/middleware/` directory
2. Copy all 5 files
3. No import rename needed (middleware uses only stdlib + x/time)

**Verify**: `go build ./internal/middleware/`

- [ ] 1.8 Copy orchestrator/hedging

**Source files**:
- `/tmp/carrier-gateway/internal/orchestrator/hedging.go`
- `/tmp/carrier-gateway/internal/orchestrator/hedging_test.go`

**Target**: `internal/orchestrator/`

**Actions**:
1. Create `internal/orchestrator/` directory
2. Copy both files
3. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports

**Verify**: `go build ./internal/orchestrator/`

- [ ] 1.9 Copy cleanup package

**Source files**:
- `/tmp/carrier-gateway/internal/cleanup/cleanup.go`
- `/tmp/carrier-gateway/internal/cleanup/ticker_test.go`

**Target**: `internal/cleanup/`

**Actions**:
1. Create `internal/cleanup/` directory
2. Copy both files
3. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports

**Verify**: `go build ./internal/cleanup/`

- [ ] 1.10 Copy testutil package

**Source files**:
- `/tmp/carrier-gateway/internal/testutil/recorder.go`

**Target**: `internal/testutil/`

**Actions**:
1. Create `internal/testutil/` directory
2. Copy file
3. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports

**Verify**: `go build ./internal/testutil/`

- [ ] 1.11 Update go.mod dependencies

**Actions**:
1. Run `go mod tidy` to resolve all new dependencies (prometheus, x/sync, x/time, goleak)
2. Verify `go.sum` is generated

**Verify**: `go build ./internal/...`

### Phase 1 Gate

```bash
go build ./internal/... && echo "Phase 1 PASS"
grep -r "carrier-gateway" internal/ && echo "FAIL: old import found" || echo "Import rename OK"
```

---

## Phase 2: Extend Domain Model

- [ ] 2.1 Create internal/domain/appetite.go

**Target**: `internal/domain/appetite.go`

**Actions**:
1. Create file with `AppetiteRule`, `RiskClassification`, `MatchResult` structs per design.md
2. Fields match Spanner schema: RuleID, CarrierID, State, LineOfBusiness, ClassCode, MinPremium (int64 cents), MaxPremium (int64 cents), IsActive, EligibilityCriteria (map[string]any)

**Verify**: `go build ./internal/domain/`

- [ ] 2.2 Modify internal/domain/quote.go -- CoverageLine

**Target**: `internal/domain/quote.go` (already copied in Phase 1)

**Actions**:
1. Change `type CoverageLine string` to `type CoverageLine = string` (type alias)
2. Keep constants `CoverageLineAuto`, `CoverageLineHomeowners`, `CoverageLineUmbrella` as convenience values
3. Remove any switch statements that enforce the enum (none in domain; handler has one -- Phase 5)

**Verify**: `go build ./internal/domain/`

- [ ] 2.3 Modify internal/domain/quote.go -- QuoteRequest extension

**Target**: `internal/domain/quote.go`

**Actions**:
1. Add fields to `QuoteRequest`: `State string`, `ClassCode string`, `EstimatedPremium Money`
2. Add comments documenting optional nature (zero values skip appetite filtering)

**Verify**: `go build ./internal/...` (downstream packages that use QuoteRequest must still compile)

- [ ] 2.4 Modify internal/domain/carrier.go -- JSON tags

**Target**: `internal/domain/carrier.go`

**Actions**:
1. Add JSON struct tags to `CarrierConfig` and `RateLimitConfig` per design.md
2. No field additions or removals

**Verify**: `go build ./internal/domain/`

- [ ] 2.5 Create internal/ports/appetite_port.go

**Target**: `internal/ports/appetite_port.go`

**Actions**:
1. Create file with `AppetiteRepository` interface: `FindMatchingRules(ctx, RiskClassification) ([]AppetiteRule, error)` and `FindAll(ctx) ([]AppetiteRule, error)`

**Verify**: `go build ./internal/ports/`

### Phase 2 Gate

```bash
go build ./internal/... && go vet ./internal/... && echo "Phase 2 PASS"
```

---

## Phase 3: Spanner Adapters

- [ ] 3.1 Create internal/adapter/spanner/client.go

**Target**: `internal/adapter/spanner/client.go`

**Actions**:
1. Create `internal/adapter/spanner/` directory
2. Implement `Client` struct wrapping `*spanner.Client` per design.md
3. `NewClient(ctx, projectID, instanceID, databaseID) (*Client, error)`
4. `Ping(ctx) error` -- executes `SELECT 1` query
5. `Close()` -- closes underlying client

**Verify**: `go build ./internal/adapter/spanner/`

- [ ] 3.2 Create internal/adapter/spanner/quote_repo.go

**Target**: `internal/adapter/spanner/quote_repo.go`

**Actions**:
1. Implement `QuoteRepo` struct satisfying `ports.QuoteRepository`
2. `Save` -- uses `spanner.InsertOrUpdate` mutations
3. `FindByRequestID` -- SQL query filtering by `ExpiresAt > CURRENT_TIMESTAMP()`
4. `DeleteExpired` -- `PartitionedUpdate` with DELETE DML
5. Add compile-time interface assertion: `var _ ports.QuoteRepository = (*QuoteRepo)(nil)`

**Verify**: `go build ./internal/adapter/spanner/`

- [ ] 3.3 Create internal/adapter/spanner/appetite_repo.go

**Target**: `internal/adapter/spanner/appetite_repo.go`

**Actions**:
1. Implement `AppetiteRepo` struct satisfying `ports.AppetiteRepository`
2. `FindAll` -- SQL query selecting all active rules
3. `FindMatchingRules` -- parameterized SQL with NULL-safe premium/ClassCode comparisons per design.md
4. `EligibilityCriteria` column deserialized via `spanner.NullJSON` into `map[string]any`
5. Add compile-time interface assertion

**Verify**: `go build ./internal/adapter/spanner/`

- [ ] 3.4 Create internal/adapter/spanner/carrier_repo.go

**Target**: `internal/adapter/spanner/carrier_repo.go`

**Actions**:
1. Implement `CarrierRepo` struct with `LoadActive(ctx) ([]domain.Carrier, error)`
2. SQL query joins Carriers with distinct AppetiteRules.LineOfBusiness to build `Capabilities` slice
3. `Config JSON` column deserialized via `spanner.NullJSON` -> `json.Unmarshal` into `domain.CarrierConfig`
4. Only returns carriers where `IsActive = true`

**Verify**: `go build ./internal/adapter/spanner/`

- [ ] 3.5 Update go.mod with Spanner dependency

**Actions**:
1. Run `go get cloud.google.com/go/spanner`
2. Run `go mod tidy`

**Verify**: `go build ./internal/adapter/spanner/`

### Phase 3 Gate

```bash
go build ./internal/... && go vet ./internal/... && echo "Phase 3 PASS"
```

---

## Phase 4: Modify Orchestrator

- [ ] 4.1 Copy orchestrator.go with import rename

**Source**: `/tmp/carrier-gateway/internal/orchestrator/orchestrator.go`
**Target**: `internal/orchestrator/orchestrator.go`

**Actions**:
1. Copy file
2. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports

**Verify**: `go build ./internal/orchestrator/` (may fail until modifications complete)

- [ ] 4.2 Add appetite fields to Orchestrator struct

**Target**: `internal/orchestrator/orchestrator.go`

**Actions**:
1. Add fields to `Orchestrator` struct:
   - `appetiteRepo ports.AppetiteRepository` (optional, nil disables)
   - `cachedRules []domain.AppetiteRule`
   - `cacheMu sync.RWMutex`
2. Update `New()` constructor: add optional `appetiteRepo ports.AppetiteRepository` parameter
3. Add `StartCacheRefresh(ctx context.Context)` method:
   - Goroutine with 60s ticker
   - Calls `appetiteRepo.FindAll(ctx)`
   - Updates `cachedRules` under write lock
   - Exits on `ctx.Done()`

**Verify**: `go build ./internal/orchestrator/`

- [ ] 4.3 Implement matchAppetiteRules method

**Target**: `internal/orchestrator/orchestrator.go`

**Actions**:
1. Add `matchAppetiteRules(req domain.QuoteRequest) map[string]bool` method per design.md
2. Returns `nil` when `req.State == ""` or cache empty (all carriers pass)
3. Matches rules by State, LineOfBusiness (against req.CoverageLines), optional ClassCode, optional premium range
4. Returns map of eligible CarrierIDs

**Verify**: `go build ./internal/orchestrator/`

- [ ] 4.4 Modify filterEligibleCarriers

**Target**: `internal/orchestrator/orchestrator.go`

**Actions**:
1. Change signature from `filterEligibleCarriers(lines []domain.CoverageLine) []domain.Carrier` to `filterEligibleCarriers(req domain.QuoteRequest) []domain.Carrier`
2. Implement 3-stage pipeline:
   - Stage 1: `appetiteEligible := o.matchAppetiteRules(req)` -- may be nil
   - Stage 2: Capability match (carrier.Capabilities intersects req.CoverageLines)
   - Stage 3: Circuit breaker state (skip Open breakers)
   - Appetite gate applied first: if `appetiteEligible != nil && !appetiteEligible[c.ID]` -> skip
3. Update call site in `fanOut()`: change `o.filterEligibleCarriers(req.CoverageLines)` to `o.filterEligibleCarriers(req)`

**Verify**: `go build ./internal/orchestrator/`

- [ ] 4.5 Copy and adapt orchestrator tests

**Source files**:
- `/tmp/carrier-gateway/internal/orchestrator/orchestrator_test.go`
- `/tmp/carrier-gateway/internal/orchestrator/orchestrator_random_test.go`
- `/tmp/carrier-gateway/internal/orchestrator/orchestrator_concurrency_test.go`

**Target**: `internal/orchestrator/`

**Actions**:
1. Copy all test files
2. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports
3. Update any calls to `filterEligibleCarriers` to pass `domain.QuoteRequest` instead of `[]domain.CoverageLine`
4. Update `New()` calls to include the new `appetiteRepo` parameter (pass `nil` for existing tests)
5. Add new tests for appetite pre-filter:
   - Test with appetite fields populated -> filters by rules
   - Test with empty appetite fields -> all carriers eligible (backward compat)
   - Test with empty cache -> all carriers eligible (graceful degradation)

**Verify**: `go test ./internal/orchestrator/`

### Phase 4 Gate

```bash
go test ./internal/orchestrator/ && echo "Phase 4 PASS"
```

---

## Phase 5: Modify Handler

- [ ] 5.1 Copy handler with import rename

**Source files**:
- `/tmp/carrier-gateway/internal/handler/http.go`
- `/tmp/carrier-gateway/internal/handler/http_test.go`

**Target**: `internal/handler/`

**Actions**:
1. Create `internal/handler/` directory
2. Copy both files
3. Replace `github.com/rechedev9/carrier-gateway` with `github.com/rechedev9/riskforge` in imports

**Verify**: `go build ./internal/handler/` (may fail until modifications complete)

- [ ] 5.2 Extend quoteRequest struct

**Target**: `internal/handler/http.go`

**Actions**:
1. Add fields to `quoteRequest`: `State string`, `ClassCode string`, `EstimatedPremiumCents int64` (all `omitempty`)
2. Update `buildDomainRequest` to populate `QuoteRequest.State`, `QuoteRequest.ClassCode`, `QuoteRequest.EstimatedPremium`

**Verify**: `go build ./internal/handler/`

- [ ] 5.3 Update validation

**Target**: `internal/handler/http.go`

**Actions**:
1. Replace `isValidCoverageLine` enum check with length validation: non-empty, max 100 chars
2. Add `state` validation: when present, must be exactly 2 characters
3. Add `estimated_premium_cents` validation: when present, must be non-negative
4. Remove `isValidCoverageLine` function

**Verify**: `go build ./internal/handler/`

- [ ] 5.4 Replace *sql.DB with HealthChecker

**Target**: `internal/handler/http.go`

**Actions**:
1. Define `HealthChecker` interface: `Ping(ctx context.Context) error`
2. Replace `db *sql.DB` field with `health HealthChecker`
3. Update `New()` constructor signature: `db *sql.DB` -> `health HealthChecker`
4. Update `handleReadyz`: call `h.health.Ping(ctx)` instead of `h.db.PingContext(ctx)`
5. Remove `"database/sql"` import

**Verify**: `go build ./internal/handler/`

- [ ] 5.5 Update handler tests

**Target**: `internal/handler/http_test.go`

**Actions**:
1. Add mock `HealthChecker` implementation for tests
2. Update `New()` calls to pass `HealthChecker` instead of `*sql.DB`
3. Add test cases for new fields: state validation, class_code, estimated_premium_cents
4. Update coverage_lines tests to accept free strings
5. Remove tests that assert enum-only coverage lines

**Verify**: `go test ./internal/handler/`

### Phase 5 Gate

```bash
go test ./internal/handler/ && echo "Phase 5 PASS"
```

---

## Phase 6: New Entry Point + Dockerfile

- [ ] 6.1 Create cmd/api/main.go

**Target**: `cmd/api/main.go`

**Reference**: `/tmp/carrier-gateway/cmd/carrier-gateway/main.go` (rewrite, not copy)

**Actions**:
1. Create `cmd/api/` directory
2. Write `main.go` per design.md wiring plan:
   - JSON logger with configurable level (LOG_LEVEL env)
   - Spanner client from SPANNER_PROJECT, SPANNER_INSTANCE, SPANNER_DATABASE
   - `carrierRepo.LoadActive(ctx)` to get carriers
   - Build per-carrier breakers/limiters/trackers from `Carrier.Config`
   - Adapter registry with mock carriers (gated by ENABLE_MOCK_CARRIERS env var)
   - Wire quoteRepo, appetiteRepo
   - Construct orchestrator with all deps including appetiteRepo
   - Start appetite cache refresh goroutine
   - Optional cleanup ticker
   - Handler with Spanner HealthChecker
   - Middleware stack: AuditLog -> SecurityHeaders -> RequireAPIKey -> LimitConcurrency
   - HTTP server with Cloud Run timeouts
   - Signal handler (SIGTERM/SIGINT) -> graceful shutdown -> close Spanner client
3. `parseAPIKeys` function (copy from carrier-gateway main.go)

**Verify**: `go build ./cmd/api/`

- [ ] 6.2 Create Dockerfile

**Target**: `Dockerfile`

**Actions**:
1. Multi-stage build per design.md:
   - Builder: `golang:1.25`, copy go.mod/go.sum, `go mod download`, copy source, `CGO_ENABLED=0 go build -o /api ./cmd/api/`
   - Runtime: `gcr.io/distroless/static-debian12`, copy binary, expose 8080
2. Set `ENTRYPOINT ["/api"]`

**Verify**: `go build ./cmd/api/` (Docker build verified separately)

### Phase 6 Gate

```bash
go build ./cmd/api/ && echo "Phase 6 PASS"
```

---

## Phase 7: Spanner DDL Update

- [ ] 7.1 Add Config column to Carriers table

**Target**: `terraform/modules/spanner/main.tf`

**Actions**:
1. Add `Config JSON,` line after `IsActive BOOL NOT NULL DEFAULT (true),` in the Carriers CREATE TABLE DDL

**Verify**: `terraform fmt -check terraform/modules/spanner/main.tf` (if terraform available, otherwise visual inspection)

- [ ] 7.2 Add Quotes table

**Target**: `terraform/modules/spanner/main.tf`

**Actions**:
1. Add new DDL entry in the `ddl` list for CREATE TABLE Quotes per design.md:
   - QuoteId STRING(36) PK with GENERATE_UUID()
   - RequestID STRING(256) NOT NULL
   - CarrierId STRING(36) NOT NULL
   - PremiumCents INT64 NOT NULL
   - Currency STRING(3) NOT NULL
   - CarrierRef STRING(256)
   - ExpiresAt TIMESTAMP NOT NULL
   - IsHedged BOOL NOT NULL DEFAULT (false)
   - LatencyMs INT64
   - CreatedAt TIMESTAMP with allow_commit_timestamp
2. Add secondary index: `CREATE INDEX QuotesByRequestID ON Quotes(RequestID)`

**Verify**: `terraform validate` in spanner module directory (if terraform available)

### Phase 7 Gate

Visual review of DDL syntax. Full validation requires `terraform init && terraform validate`.

---

## Phase 8: CI Update

- [ ] 8.1 Create Go CI workflow

**Target**: `.github/workflows/go.yml`

**Actions**:
1. Create workflow file per design.md
2. Trigger on push/PR to main for `**.go`, `go.mod`, `go.sum` paths
3. Job `build-test` on `ubuntu-latest`:
   - `actions/checkout@v4`
   - `actions/setup-go@v5` with `go-version: '1.25.0'` and `cache: true`
   - `go build ./...`
   - `go test ./...`
   - `go vet ./...`

**Verify**: Visual inspection of YAML syntax. CI execution on push.

- [ ] 8.2 Update go.mod Go version

**Target**: `go.mod`

**Actions**:
1. Change `go 1.24.1` to `go 1.25.0`
2. Run `go mod tidy` to update toolchain directive if needed

**Verify**: `go build ./...`

### Phase 8 Gate

```bash
go build ./... && go test ./... && go vet ./... && echo "Phase 8: FULL GATE PASS"
```

---

## Summary

| Phase | Tasks | Key Files | Gate Command |
|---|---|---|---|
| 1: Copy Portable Code | 1.1--1.11 | 27 files copied + go.mod | `go build ./internal/...` |
| 2: Extend Domain Model | 2.1--2.5 | appetite.go (new), quote.go (mod), carrier.go (mod), appetite_port.go (new) | `go build ./internal/... && go vet ./internal/...` |
| 3: Spanner Adapters | 3.1--3.5 | 4 new files in adapter/spanner/ | `go build ./internal/...` |
| 4: Modify Orchestrator | 4.1--4.5 | orchestrator.go (copy+mod), 3 test files | `go test ./internal/orchestrator/` |
| 5: Modify Handler | 5.1--5.5 | http.go (copy+mod), http_test.go (copy+mod) | `go test ./internal/handler/` |
| 6: Entry Point + Dockerfile | 6.1--6.2 | cmd/api/main.go (new), Dockerfile (new) | `go build ./cmd/api/` |
| 7: Spanner DDL | 7.1--7.2 | terraform/modules/spanner/main.tf (mod) | `terraform validate` |
| 8: CI Update | 8.1--8.2 | .github/workflows/go.yml (new), go.mod (mod) | `go build ./... && go test ./... && go vet ./...` |

**Total tasks**: 30
**Total files**: ~44 (27 copied, 7 new, 6 modified, 4 test files adapted)

---

## Commit Plan

Each phase is one atomic commit:

1. `feat(internal): copy portable packages from carrier-gateway`
2. `feat(domain): extend model with appetite types and QuoteRequest fields`
3. `feat(adapter): add Spanner repository adapters`
4. `feat(orchestrator): add appetite pre-filter with in-memory cache`
5. `feat(handler): extend request schema and replace DB health check`
6. `feat(api): add Cloud Run entry point and Dockerfile`
7. `feat(spanner): add Config column and Quotes table to DDL`
8. `ci(go): add build/test/vet workflow`

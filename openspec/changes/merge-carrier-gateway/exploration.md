# Exploration: merge-carrier-gateway

## Current State

### carrier-gateway (source)
- **Module**: `github.com/rechedev9/carrier-gateway`
- **Go version**: 1.25.0
- **Total LOC**: 6,626 (34 Go files: 16 production, 18 test)
- **Direct dependencies**: `lib/pq`, `prometheus/client_golang`, `prometheus/client_model`, `goleak`, `x/sync`, `x/time`
- **Architecture**: hexagonal — domain -> ports -> adapters/circuitbreaker/ratelimiter -> orchestrator -> handler -> middleware -> main
- **Entry point**: `cmd/carrier-gateway/main.go` (374 LOC)

### riskforge (target)
- **Module**: `github.com/rechedev9/riskforge`
- **Go version**: 1.24.1
- **Go code**: zero (go.mod only, no `.go` files)
- **Infrastructure**: 8 Terraform modules (cloud-run, spanner, pubsub, iam, networking, monitoring, kms, storage), 2 environments (dev, prod), CI/CD workflow
- **Spanner schema**: `Carriers` table + `AppetiteRules` table (interleaved, parent = Carriers)

---

## 2. Domain Model Mapping

### carrier-gateway domain types

| Type | File | Fields |
|------|------|--------|
| `Carrier` | `domain/carrier.go` | ID, Name, Capabilities ([]CoverageLine), Config (CarrierConfig) |
| `CarrierConfig` | `domain/carrier.go` | TimeoutHint, OpenTimeout, FailureThreshold, SuccessThreshold, HedgeMultiplier, EMAAlpha, EMAWindowSize, EMAWarmupObservations, RateLimit, Priority |
| `RateLimitConfig` | `domain/carrier.go` | TokensPerSecond, Burst |
| `CoverageLine` | `domain/quote.go` | typed string: "auto", "homeowners", "umbrella" |
| `Money` | `domain/quote.go` | Amount (int64 cents), Currency (string) |
| `QuoteRequest` | `domain/quote.go` | RequestID, ClientID, CoverageLines, Timeout |
| `QuoteResult` | `domain/quote.go` | RequestID, CarrierID, Premium (Money), ExpiresAt, CarrierRef, Latency, IsHedged |

### riskforge Spanner schema types (to be modeled in Go)

| Table | Columns |
|-------|---------|
| `Carriers` | CarrierId (STRING/UUID), Name, Code, IsActive, CreatedAt, UpdatedAt |
| `AppetiteRules` | RuleId, CarrierId (FK, interleaved), State (2-char), LineOfBusiness, ClassCode, MinPremium, MaxPremium, IsActive, EligibilityCriteria (JSON) |

### Mapping analysis

| Concept | carrier-gateway | riskforge Spanner | Compatibility |
|---------|----------------|-------------------|---------------|
| Carrier identity | `Carrier.ID` (string, e.g. "alpha") | `Carriers.CarrierId` (UUID string) | **Compatible** — both strings, riskforge uses UUIDs |
| Carrier name | `Carrier.Name` | `Carriers.Name` | Direct match |
| Carrier code | N/A | `Carriers.Code` | New field — no conflict |
| Active flag | N/A (all carriers hardcoded as active) | `Carriers.IsActive` | New field — adds dynamic enable/disable |
| Coverage lines | `Carrier.Capabilities` ([]CoverageLine enum) | `AppetiteRules.LineOfBusiness` (STRING) | **Semantic overlap** — CoverageLine is a typed string, LineOfBusiness is a free string. Need mapping layer or expand CoverageLine. |
| Geographic eligibility | N/A | `AppetiteRules.State` (2-char state code) | **New concept** — carrier-gateway has no geographic filtering |
| Class code | N/A | `AppetiteRules.ClassCode` | **New concept** — insurance class code for appetite matching |
| Premium bounds | N/A | `AppetiteRules.MinPremium`, `MaxPremium` | **New concept** — pre-filter by expected premium range |
| Eligibility criteria | N/A | `AppetiteRules.EligibilityCriteria` (JSON) | **New concept** — flexible JSON-based rules |
| Operational config | `CarrierConfig` (timeouts, breaker, hedge, rate) | N/A | **Carrier-gateway only** — not in Spanner schema |

### New domain types needed for merge

1. **`AppetiteRule`** — Go struct matching the Spanner schema: RuleID, CarrierID, State, LineOfBusiness, ClassCode, MinPremium, MaxPremium, IsActive, EligibilityCriteria
2. **`RiskClassification`** — input to the appetite matching engine: State, LineOfBusiness, ClassCode, EstimatedPremium (used to query AppetiteRules)
3. **`MatchResult`** — output of appetite pre-filter: CarrierID, RuleID, MatchScore

### QuoteRequest extension

The existing `QuoteRequest` needs additional fields for appetite pre-filtering:
- `State` (string, 2-char)
- `ClassCode` (string, optional)
- `EstimatedPremium` (Money, optional)

These are used BEFORE fan-out to filter carriers via AppetiteRules, then the remaining carriers go through the existing orchestrator pipeline.

---

## Relevant Files

### Copy as-is (minimal changes — rename module import path only)

| Package | Files | LOC | Notes |
|---------|-------|-----|-------|
| `internal/domain/` | carrier.go, quote.go, errors.go | 146 | Pure value types, zero deps. Add new types alongside. |
| `internal/ports/` | quote_port.go, repository_port.go, metrics_port.go | 110 | Interface-only. Add `AppetiteRepository` port. |
| `internal/circuitbreaker/` | breaker.go, breaker_test.go | 480 | Self-contained, imports only domain+ports. |
| `internal/ratelimiter/` | limiter.go, limiter_test.go | 169 | Self-contained, imports x/time + domain+ports. |
| `internal/metrics/` | prometheus.go, prometheus_test.go | 459 | Self-contained Prometheus recorder. |
| `internal/middleware/` | auth.go, audit.go, concurrency.go, security.go, middleware_test.go | 475 | All stdlib+x/time. No adaptation needed. |
| `internal/testutil/` | recorder.go | 74 | NoopRecorder for tests. |
| `internal/orchestrator/hedging.go` | hedging.go, hedging_test.go | 398 | EMATracker + hedgeMonitor. Self-contained. |
| `internal/adapter/adapter.go` | adapter.go, adapter_test.go | 288 | Generic adapter registry pattern. |
| `internal/adapter/mock_carrier.go` | mock_carrier.go, mock_carrier_test.go | 339 | Development/test mock carriers. |
| `internal/adapter/http_carrier.go` | http_carrier.go, http_carrier_test.go | 410 | Generic HTTP carrier client (retries, backoff). |
| `internal/adapter/delta_carrier.go` | delta_carrier.go | 88 | Delta-specific adapter. |
| `internal/cleanup/` | cleanup.go, ticker_test.go | 161 | Background expired-quote cleanup ticker. |

**Total copy-as-is: ~3,597 LOC** (rename import paths from `carrier-gateway` to `riskforge`)

### Needs adaptation

| Package | File | LOC | Change required |
|---------|------|-----|----------------|
| `internal/repository/postgres.go` | postgres.go | 170 | **Replace entirely** with Spanner adapter. Same `ports.QuoteRepository` interface, different SQL dialect and client library (`cloud.google.com/go/spanner`). |
| `internal/orchestrator/orchestrator.go` | orchestrator.go | 388 | **Modify `filterEligibleCarriers`** to call `AppetiteRepository.FindMatchingRules()` before capability filtering. Add appetite pre-filter step in `fanOut()`. |
| `internal/handler/http.go` | http.go | 333 | **Extend `quoteRequest` JSON** to accept State, ClassCode, EstimatedPremium. Update validation. Change `*sql.DB` readiness check to Spanner health check. |
| `cmd/carrier-gateway/main.go` | main.go | 374 | **Rewrite** — new entry point at `cmd/api/main.go`. Replace Postgres setup with Spanner client. Replace hardcoded carrier list with Spanner-backed carrier registry. Wire appetite repository. |

**Total needs adaptation: ~1,265 LOC**

### New code required

| Component | Estimated LOC | Purpose |
|-----------|--------------|---------|
| `internal/domain/appetite.go` | ~60 | AppetiteRule, RiskClassification, MatchResult types |
| `internal/ports/appetite_port.go` | ~25 | AppetiteRepository interface |
| `internal/adapter/spanner_quote.go` | ~200 | QuoteRepository backed by Spanner (replaces postgres.go) |
| `internal/adapter/spanner_appetite.go` | ~150 | AppetiteRepository backed by Spanner |
| `internal/adapter/spanner_carrier.go` | ~100 | CarrierRepository for loading carrier configs from Spanner |
| `cmd/api/main.go` | ~300 | Cloud Run entry point replacing cmd/carrier-gateway |
| `Dockerfile` | ~25 | Cloud Run Dockerfile (adapt existing) |

**Total new code: ~860 LOC**

### Drop (not needed in riskforge)

| File/Dir | Reason |
|----------|--------|
| `docker-compose.yml` | Replaced by Terraform + Cloud Run |
| `k8s/` (if any) | Replaced by Terraform modules |
| `.env.example` | Cloud Run uses Secret Manager / env vars via Terraform |
| `internal/repository/postgres.go` | Replaced by Spanner adapters |

---

## 4. Dependency Conflict Analysis

### Go version
- carrier-gateway: **1.25.0**
- riskforge: **1.24.1**
- **Action**: Upgrade riskforge to 1.25.0. No backward compatibility risk since riskforge has no Go code yet.

### Module path
- carrier-gateway: `github.com/rechedev9/carrier-gateway`
- riskforge: `github.com/rechedev9/riskforge`
- **Action**: All copied files need import path rewrite. Mechanical find-and-replace.

### Dependency overlap

| Dependency | carrier-gateway | riskforge | Conflict? |
|-----------|----------------|-----------|-----------|
| `github.com/lib/pq` | v1.11.2 (direct) | N/A | **Drop** — replaced by Spanner client |
| `github.com/prometheus/client_golang` | v1.23.2 | N/A | **Keep** — metrics stay |
| `golang.org/x/sync` | v0.20.0 | N/A | **Keep** — errgroup, singleflight |
| `golang.org/x/time` | v0.15.0 | N/A | **Keep** — rate limiter |
| `go.uber.org/goleak` | v1.3.0 | N/A | **Keep** — test-only dep |
| `cloud.google.com/go/spanner` | N/A | N/A (not yet) | **Add** — new Spanner adapter |

### New dependencies needed
- `cloud.google.com/go/spanner` — Spanner client library
- `google.golang.org/api` — GCP API helpers (likely transitive)
- `google.golang.org/grpc` — gRPC for Spanner (transitive)

No version conflicts since riskforge has zero Go dependencies currently.

---

## Risk Assessment

### Low risk
- **Domain types copy**: Pure value types with zero deps; mechanical import rename.
- **Circuit breaker, rate limiter, metrics**: Self-contained packages with no infra deps.
- **Middleware**: All stdlib-based; direct copy.
- **Go version upgrade**: No existing code to break.
- **Test portability**: All test files use stdlib `testing` + `testutil.NoopRecorder`.

### Medium risk
- **Orchestrator modification**: Adding appetite pre-filter to `filterEligibleCarriers` changes the carrier selection pipeline. The existing capability-based filter is 20 LOC; adding Spanner-backed appetite lookup adds a database call in the hot path. Mitigation: cache AppetiteRules in memory with periodic refresh.
- **Handler JSON extension**: Adding new fields to `quoteRequest` is backward-compatible (omitempty), but validation logic grows. Existing tests cover the old schema; new tests needed for appetite fields.
- **Spanner adapter complexity**: Spanner's SQL dialect differs from Postgres (e.g., no ON CONFLICT DO NOTHING, uses MERGE or INSERT OR UPDATE). The `QuoteRepository` interface is simple enough that reimplementation is straightforward, but the Spanner client API (`cloud.google.com/go/spanner`) uses mutations rather than SQL for writes.

### High risk
- **Carrier registration**: carrier-gateway hardcodes carriers in `buildCarriers()` (main.go). Riskforge loads carriers from Spanner. This is a fundamental change in how the carrier list is populated. The `Carrier` struct needs `CarrierConfig` populated from somewhere — Spanner schema has no columns for timeout hints, breaker thresholds, hedge multiplier, etc. **Decision needed**: store operational config in Spanner (new columns), in a config file, or in environment variables?
- **Integration testing**: carrier-gateway has e2e tests (`e2e_test.go`, `e2e_fuzz_test.go`) totaling 509 LOC that start the full server. These need a Spanner emulator or mock to run. The Spanner emulator has known limitations (no MERGE, limited JSON support).

---

## 6. File Inventory

### Files from carrier-gateway -> riskforge mapping

| Source | Target | Action |
|--------|--------|--------|
| `internal/domain/carrier.go` | `internal/domain/carrier.go` | Copy + rename imports |
| `internal/domain/quote.go` | `internal/domain/quote.go` | Copy + rename imports |
| `internal/domain/errors.go` | `internal/domain/errors.go` | Copy + rename imports |
| N/A | `internal/domain/appetite.go` | **New**: AppetiteRule, RiskClassification, MatchResult |
| `internal/ports/quote_port.go` | `internal/ports/quote_port.go` | Copy + rename imports |
| `internal/ports/repository_port.go` | `internal/ports/repository_port.go` | Copy + rename imports |
| `internal/ports/metrics_port.go` | `internal/ports/metrics_port.go` | Copy + rename imports |
| N/A | `internal/ports/appetite_port.go` | **New**: AppetiteRepository interface |
| `internal/adapter/adapter.go` | `internal/adapter/adapter.go` | Copy + rename imports |
| `internal/adapter/mock_carrier.go` | `internal/adapter/mock_carrier.go` | Copy + rename imports |
| `internal/adapter/http_carrier.go` | `internal/adapter/http_carrier.go` | Copy + rename imports |
| `internal/adapter/delta_carrier.go` | `internal/adapter/delta_carrier.go` | Copy + rename imports |
| `internal/repository/postgres.go` | **DROP** | Replaced by Spanner adapter |
| N/A | `internal/adapter/spanner_quote.go` | **New**: QuoteRepository over Spanner |
| N/A | `internal/adapter/spanner_appetite.go` | **New**: AppetiteRepository over Spanner |
| N/A | `internal/adapter/spanner_carrier.go` | **New**: CarrierRepository over Spanner |
| `internal/circuitbreaker/breaker.go` | `internal/circuitbreaker/breaker.go` | Copy + rename imports |
| `internal/ratelimiter/limiter.go` | `internal/ratelimiter/limiter.go` | Copy + rename imports |
| `internal/orchestrator/orchestrator.go` | `internal/orchestrator/orchestrator.go` | Copy + **modify** filterEligibleCarriers |
| `internal/orchestrator/hedging.go` | `internal/orchestrator/hedging.go` | Copy + rename imports |
| `internal/handler/http.go` | `internal/handler/http.go` | Copy + **modify** request schema, readiness |
| `internal/metrics/prometheus.go` | `internal/metrics/prometheus.go` | Copy + rename imports |
| `internal/middleware/auth.go` | `internal/middleware/auth.go` | Copy (no internal imports) |
| `internal/middleware/audit.go` | `internal/middleware/audit.go` | Copy (no internal imports) |
| `internal/middleware/concurrency.go` | `internal/middleware/concurrency.go` | Copy (no internal imports) |
| `internal/middleware/security.go` | `internal/middleware/security.go` | Copy (no internal imports) |
| `internal/cleanup/cleanup.go` | `internal/cleanup/cleanup.go` | Copy + rename imports |
| `internal/testutil/recorder.go` | `internal/testutil/recorder.go` | Copy + rename imports |
| `cmd/carrier-gateway/main.go` | `cmd/api/main.go` | **Rewrite**: Spanner client, dynamic carrier loading |
| `Dockerfile` | `Dockerfile` | Copy + **modify**: binary path, Cloud Run port |

### All test files follow their production files (same action).

### Files staying from riskforge (untouched)

| Path | Purpose |
|------|---------|
| `terraform/` (entire tree) | IaC modules — unchanged |
| `go.mod` | Updated: Go 1.25, new deps added |
| `.github/workflows/` | Extended with Go build/test job |
| `docs/` | Reference documentation |
| `scripts/` | committer, docs-list |
| `openspec/` | SDD artifacts |
| `CLAUDE.md` | Agent protocol |

---

## Open Questions (DEFERRED)

1. **CarrierConfig storage**: Where do operational parameters (timeout hints, breaker thresholds, hedge multiplier, rate limits) live? Options: (a) new Spanner columns on Carriers table, (b) JSON blob in Carriers table, (c) YAML/env config file.
2. **Appetite pre-filter caching**: Should AppetiteRules be loaded into memory at startup + refreshed periodically, or queried per-request from Spanner?
3. **Quotes table in Spanner**: Should the quote cache table be added to the existing Spanner DDL in Terraform, or managed by application-level migration?
4. **CoverageLine expansion**: carrier-gateway defines 3 coverage lines (auto, homeowners, umbrella). AppetiteRules.LineOfBusiness is a free STRING(100). Should CoverageLine remain a typed enum or become a free string?
5. **cmd/worker/**: CLAUDE.md mentions `cmd/worker/` for Pub/Sub subscriber. Is the worker part of this merge or a separate change?

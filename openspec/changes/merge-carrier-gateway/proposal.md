# Proposal: Merge Carrier Gateway

**Change ID**: merge-carrier-gateway
**Date**: 2026-03-22T20:00:00Z
**Status**: draft

---

## Intent

The riskforge repository has full GCP infrastructure (Terraform modules for Cloud Run, Spanner, Pub/Sub, IAM, etc.) but zero Go application code. The carrier-gateway repository has a production-grade multi-carrier quote aggregation engine (6,626 LOC, hexagonal architecture) but targets Postgres on bare metal. This change merges carrier-gateway into riskforge, replacing Postgres with Spanner, adding appetite-based pre-filtering from the existing Spanner schema, and producing a Cloud Run-deployable binary.

## Scope

### In Scope

- Copy 3,597 LOC of self-contained packages from carrier-gateway into riskforge (domain, ports, adapter, circuitbreaker, ratelimiter, metrics, middleware, orchestrator/hedging, cleanup, testutil) with import path rewrite from `github.com/rechedev9/carrier-gateway` to `github.com/rechedev9/riskforge`.
- Extend domain model: add `AppetiteRule`, `RiskClassification`, `MatchResult` types; extend `QuoteRequest` with `State`, `ClassCode`, `EstimatedPremium`; change `CoverageLine` from typed enum to `STRING(100)` free string alias.
- Add Spanner adapters: `QuoteRepository`, `AppetiteRepository`, `CarrierRepository` replacing Postgres persistence.
- Modify orchestrator: add appetite pre-filter step in `filterEligibleCarriers()` before capability match; add in-memory appetite cache with 60s TTL refresh.
- Modify handler: extend JSON request/response with appetite fields; replace `*sql.DB` readiness check with Spanner health check.
- New `cmd/api/main.go` entry point for Cloud Run; Dockerfile for multi-stage build.
- Store `CarrierConfig` as JSON blob column on existing Carriers table in Spanner DDL.
- Add `Quotes` table to Spanner DDL in `terraform/modules/spanner/main.tf`.
- Upgrade `go.mod` to Go 1.25.0; add Spanner, Prometheus, x/sync, x/time dependencies.
- Add Go build/test/lint CI job to `.github/workflows/`.

### Out of Scope

- `cmd/worker/` (Pub/Sub subscriber) -- separate change.
- carrier-gateway e2e/fuzz tests (509 LOC) -- require Spanner emulator setup; deferred.
- Postgres migration tooling or docker-compose -- dropped entirely.
- Terraform module changes beyond Spanner DDL additions.
- Cloud Endpoints / API Gateway / authentication changes.

## Approach

Bottom-up, 8-phase implementation. Each phase is independently verifiable with `go build ./...` or `go test ./...`.

1. **Copy portable code** -- mechanical import rename of 12 packages (3,597 LOC).
2. **Extend domain model** -- new types + QuoteRequest extension + CoverageLine relaxation.
3. **Spanner adapters** -- implement `QuoteRepository`, `AppetiteRepository`, `CarrierRepository` against Spanner client library.
4. **Modify orchestrator** -- add `AppetiteRepository` dependency, appetite cache, appetite pre-filter in `filterEligibleCarriers`.
5. **Modify handler** -- extend request schema, update validation, replace DB readiness.
6. **New entry point + Dockerfile** -- `cmd/api/main.go` wiring, Cloud Run Dockerfile.
7. **Spanner DDL update** -- `Config JSON` column on Carriers, Quotes table.
8. **CI update** -- Go build/test/lint workflow job.

### Key Decisions

| Decision | Choice | Rationale |
|---|---|---|
| CarrierConfig storage | JSON blob column on Carriers table | Avoids schema explosion for operational params; JSON is flexible for config evolution; Spanner JSON type supports indexing if needed later |
| Appetite caching | In-memory cache, 60s TTL refresh | Eliminates per-request Spanner query in hot path; 60s staleness is acceptable for appetite rules that change infrequently |
| CoverageLine type | Free string `STRING(100)` matching `LineOfBusiness` | Eliminates mapping layer between typed enum and Spanner column; new LOBs added without code changes |
| Quotes table | Spanner DDL in Terraform | Consistent with existing Carriers/AppetiteRules DDL management |
| Go version | 1.25.0 | Matches carrier-gateway; no existing Go code to break |
| cmd/worker/ | Out of scope | Separate concern; this merge focuses on the HTTP API path only |

## Affected Areas

| Area | Files | Change Type | Risk |
|---|---|---|---|
| internal/domain/ | 4 files (3 copy + 1 new) | copy + extend | low |
| internal/ports/ | 4 files (3 copy + 1 new) | copy + extend | low |
| internal/adapter/ | 7 files (4 copy + 3 new) | copy + create | medium |
| internal/circuitbreaker/ | 2 files | copy | low |
| internal/ratelimiter/ | 2 files | copy | low |
| internal/metrics/ | 2 files | copy | low |
| internal/middleware/ | 5 files | copy | low |
| internal/orchestrator/ | 4 files (2 copy + 2 modify) | copy + modify | high |
| internal/handler/ | 2 files | modify | medium |
| internal/cleanup/ | 2 files | copy | low |
| internal/testutil/ | 1 file | copy | low |
| cmd/api/ | 1 file (new) | create | medium |
| Dockerfile | 1 file (new) | create | low |
| go.mod | 1 file | modify | low |
| terraform/modules/spanner/main.tf | 1 file | modify | low |
| .github/workflows/ | 1 file (new) | create | low |

**Total files**: ~40
**New files**: ~7
**Copied files**: ~27
**Modified files**: ~6

## Risks

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Orchestrator appetite pre-filter adds latency to fan-out | low | medium | In-memory cache eliminates Spanner query; cache miss falls back to no-filter (all carriers eligible) |
| Spanner mutation semantics differ from Postgres SQL | medium | medium | QuoteRepository interface is simple (Save/FindByRequestID/DeleteExpired); Spanner mutations for writes, SQL for reads |
| CarrierConfig JSON deserialization mismatches | low | medium | Unit test JSON round-trip for all CarrierConfig fields; use `encoding/json` struct tags |
| CoverageLine relaxation breaks existing capability filter | low | high | String comparison is exact-match; existing "auto"/"homeowners"/"umbrella" values continue to work; validation moves to API boundary |
| Missing Spanner emulator for local testing | medium | medium | Unit tests use mock interfaces; integration tests deferred to CI with emulator |

**Overall Risk Level**: medium

## Rollback Plan

Revert the merge commit. No infrastructure state changes until Spanner DDL is applied via Terraform. The Dockerfile and cmd/api/ are inert until deployed to Cloud Run.

## Dependencies

### External Dependencies

| Package | Version | Purpose |
|---|---|---|
| cloud.google.com/go/spanner | latest | Spanner client |
| github.com/prometheus/client_golang | v1.23.2+ | Metrics |
| golang.org/x/sync | v0.20.0+ | errgroup, singleflight |
| golang.org/x/time | v0.15.0+ | Rate limiter |
| go.uber.org/goleak | v1.3.0 | Test-only goroutine leak detection |

### Infrastructure Dependencies

- Spanner DDL migration: `Config JSON` column on Carriers, new Quotes table
- No new environment variables until deployment
- No new GCP services (Spanner already enabled)

## Success Criteria

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes with all existing carrier-gateway unit tests adapted
- [ ] `go vet ./...` clean
- [ ] `filterEligibleCarriers` implements 3-stage pipeline: appetite match -> capability match -> circuit breaker state
- [ ] Spanner DDL in Terraform includes `Config JSON` on Carriers and new Quotes table
- [ ] `cmd/api/main.go` compiles and loads carriers from Spanner
- [ ] Dockerfile builds successfully
- [ ] CI workflow runs `go build`, `go test`, `go vet`

---

**Next Step**: Proceed to spec, design, and tasks.

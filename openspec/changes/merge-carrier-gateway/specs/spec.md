# Spec Index: Merge Carrier Gateway

**Change**: merge-carrier-gateway
**Date**: 2026-03-22T20:00:00Z
**Status**: draft
**Depends On**: propose.md

---

## Overview

Requirements for merging the carrier-gateway quote aggregation engine into riskforge, replacing Postgres with Spanner, adding appetite-based pre-filtering, and targeting Cloud Run deployment. Organized by the 8 implementation phases.

---

## Phase 1: Copy Portable Code

| ID | Requirement | Verification |
|---|---|---|
| REQ-COPY-001 | All 12 packages copied with import paths rewritten from `github.com/rechedev9/carrier-gateway` to `github.com/rechedev9/riskforge` | `grep -r "carrier-gateway" internal/` returns 0 matches |
| REQ-COPY-002 | All test files copied alongside production files | `go test ./internal/...` compiles (may not pass until Phase 2+) |
| REQ-COPY-003 | No `internal/repository/` directory exists (postgres.go dropped) | `ls internal/repository/` fails |
| REQ-COPY-004 | `go build ./internal/...` succeeds after import rename | `go build ./internal/...` exit 0 |

## Phase 2: Extend Domain Model

| ID | Requirement | Verification |
|---|---|---|
| REQ-DOM-001 | `AppetiteRule` struct exists in `internal/domain/appetite.go` with fields: RuleID, CarrierID, State, LineOfBusiness, ClassCode, MinPremium, MaxPremium, IsActive, EligibilityCriteria | `go vet ./internal/domain/` |
| REQ-DOM-002 | `RiskClassification` struct exists with fields: State, LineOfBusiness, ClassCode, EstimatedPremium | `go vet ./internal/domain/` |
| REQ-DOM-003 | `MatchResult` struct exists with fields: CarrierID, RuleID, MatchScore | `go vet ./internal/domain/` |
| REQ-DOM-004 | `QuoteRequest` extended with `State string`, `ClassCode string`, `EstimatedPremium Money` fields | `go build ./internal/domain/` |
| REQ-DOM-005 | `CoverageLine` type is `type CoverageLine = string` (free string alias); constants `CoverageLineAuto`, `CoverageLineHomeowners`, `CoverageLineUmbrella` retained as convenience values | `go build ./internal/...` |
| REQ-DOM-006 | `Carrier.Config` field type is `CarrierConfig` with JSON struct tags on all fields | JSON round-trip test |

## Phase 3: Spanner Adapters

| ID | Requirement | Verification |
|---|---|---|
| REQ-SPN-001 | `internal/adapter/spanner/client.go` provides a Spanner client wrapper accepting `projectID`, `instanceID`, `databaseID` | `go build ./internal/adapter/spanner/` |
| REQ-SPN-002 | `internal/adapter/spanner/quote_repo.go` implements `ports.QuoteRepository` using Spanner mutations for Save and SQL for FindByRequestID/DeleteExpired | `go build ./internal/adapter/spanner/` |
| REQ-SPN-003 | `internal/adapter/spanner/appetite_repo.go` implements `ports.AppetiteRepository` with `FindMatchingRules(ctx, RiskClassification) ([]AppetiteRule, error)` | `go build ./internal/adapter/spanner/` |
| REQ-SPN-004 | `internal/adapter/spanner/carrier_repo.go` loads active carriers from Spanner, deserializes `Config JSON` column into `domain.CarrierConfig` | `go build ./internal/adapter/spanner/` |
| REQ-SPN-005 | Save uses `InsertOrUpdate` mutations (Spanner equivalent of `ON CONFLICT DO NOTHING`) | Code review |
| REQ-SPN-006 | FindMatchingRules filters by `State`, `LineOfBusiness`, optional `ClassCode`, and premium range (`MinPremium <= EstimatedPremium <= MaxPremium`) with NULL-safe comparisons | Unit test |

## Phase 4: Modify Orchestrator

| ID | Requirement | Verification |
|---|---|---|
| REQ-ORCH-001 | `Orchestrator` struct has new `appetiteRepo ports.AppetiteRepository` field | `go build ./internal/orchestrator/` |
| REQ-ORCH-002 | `Orchestrator` struct has new `appetiteCache` field holding cached `[]domain.AppetiteRule` with `sync.RWMutex` protection | `go build ./internal/orchestrator/` |
| REQ-ORCH-003 | Cache refreshes every 60 seconds via background goroutine started by `New()` or explicit `StartCacheRefresh(ctx)` method | Unit test with mock AppetiteRepository |
| REQ-ORCH-004 | `filterEligibleCarriers` signature changes to accept `domain.QuoteRequest` (not just `[]CoverageLine`) | `go build ./internal/orchestrator/` |
| REQ-ORCH-005 | Filter pipeline is 3 stages in order: (1) appetite rule match from cache, (2) capability match using LineOfBusiness from matched rules, (3) circuit breaker state check | Unit test |
| REQ-ORCH-006 | When appetite cache is empty or appetite fields are zero-valued on request, filter falls back to capability-only match (backward compatible) | Unit test |
| REQ-ORCH-007 | `New()` constructor accepts optional `AppetiteRepository` parameter (nil disables appetite filtering) | `go build ./internal/orchestrator/` |

## Phase 5: Modify Handler

| ID | Requirement | Verification |
|---|---|---|
| REQ-HDL-001 | `quoteRequest` JSON struct adds `state string`, `class_code string`, `estimated_premium_cents int64` fields (all `omitempty`) | JSON decode test |
| REQ-HDL-002 | `state` validated as 2-character US state code when present | Unit test |
| REQ-HDL-003 | `estimated_premium_cents` validated as non-negative when present | Unit test |
| REQ-HDL-004 | `buildDomainRequest` populates `QuoteRequest.State`, `QuoteRequest.ClassCode`, `QuoteRequest.EstimatedPremium` | Unit test |
| REQ-HDL-005 | `coverage_lines` validation drops the enum whitelist; accepts any non-empty string up to 100 chars | Unit test |
| REQ-HDL-006 | `Handler.db *sql.DB` replaced with a `HealthChecker` interface; Spanner adapter implements `Ping(ctx) error` | `go build ./internal/handler/` |
| REQ-HDL-007 | `/readyz` calls `HealthChecker.Ping()` instead of `db.PingContext()` | Unit test |

## Phase 6: Entry Point + Dockerfile

| ID | Requirement | Verification |
|---|---|---|
| REQ-MAIN-001 | `cmd/api/main.go` reads `SPANNER_PROJECT`, `SPANNER_INSTANCE`, `SPANNER_DATABASE` env vars | `go build ./cmd/api/` |
| REQ-MAIN-002 | Carriers loaded from Spanner via `CarrierRepository.LoadActive()` at startup | Code review |
| REQ-MAIN-003 | Per-carrier infrastructure (breakers, limiters, trackers) built from `Carrier.Config` deserialized from Spanner JSON | Code review |
| REQ-MAIN-004 | `AppetiteRepository` wired into orchestrator; cache refresh started | Code review |
| REQ-MAIN-005 | Graceful shutdown closes Spanner client | Code review |
| REQ-MAIN-006 | Dockerfile uses multi-stage build: Go 1.25 builder -> `gcr.io/distroless/static-debian12` runtime | `docker build .` |
| REQ-MAIN-007 | Dockerfile exposes port 8080 (Cloud Run default) | Dockerfile inspection |

## Phase 7: Spanner DDL Update

| ID | Requirement | Verification |
|---|---|---|
| REQ-DDL-001 | Carriers table gains `Config JSON` column | `terraform validate` in spanner module |
| REQ-DDL-002 | Quotes table created with columns: QuoteId, RequestID, CarrierId, PremiumCents, Currency, CarrierRef, ExpiresAt, IsHedged, Latency, CreatedAt | `terraform validate` |
| REQ-DDL-003 | Quotes table primary key is `(QuoteId)` with secondary index on `(RequestID)` | DDL inspection |
| REQ-DDL-004 | Quotes table has `ExpiresAt` column for TTL-based cleanup | DDL inspection |

## Phase 8: CI Update

| ID | Requirement | Verification |
|---|---|---|
| REQ-CI-001 | `.github/workflows/go.yml` (or added job in existing workflow) runs on push/PR to main | Workflow file inspection |
| REQ-CI-002 | Job runs `go build ./...`, `go test ./...`, `go vet ./...` | Workflow file inspection |
| REQ-CI-003 | Job uses Go 1.25.0 | Workflow file inspection |
| REQ-CI-004 | Job caches Go modules and build cache | Workflow file inspection |

---

## Proposal Coverage

| Phase | Proposal Items | Requirements |
|---|---|---|
| Phase 1: Copy Portable Code | Copy 3,597 LOC, rename imports | REQ-COPY-001 to REQ-COPY-004 |
| Phase 2: Extend Domain Model | New types, QuoteRequest extension, CoverageLine change | REQ-DOM-001 to REQ-DOM-006 |
| Phase 3: Spanner Adapters | Replace Postgres with Spanner | REQ-SPN-001 to REQ-SPN-006 |
| Phase 4: Modify Orchestrator | Appetite pre-filter, cache | REQ-ORCH-001 to REQ-ORCH-007 |
| Phase 5: Modify Handler | Request extension, validation, health check | REQ-HDL-001 to REQ-HDL-007 |
| Phase 6: Entry Point + Dockerfile | Cloud Run main, Dockerfile | REQ-MAIN-001 to REQ-MAIN-007 |
| Phase 7: Spanner DDL | Config column, Quotes table | REQ-DDL-001 to REQ-DDL-004 |
| Phase 8: CI | Go workflow job | REQ-CI-001 to REQ-CI-004 |

**Total Requirements**: 38

---

## Out-of-Scope Confirmation

- `cmd/worker/` Pub/Sub subscriber -- separate change
- e2e/fuzz tests from carrier-gateway -- require Spanner emulator; deferred
- Postgres migration tooling -- dropped
- Authentication/authorization changes -- separate concern
- Terraform module changes beyond Spanner DDL -- separate change

# CLAUDE.md

## Agent Protocol
- Docs: run `scripts/docs-list` before deep work; honor `read_when` hints.
- "Make a note" => append to this file or relevant doc.
- Keep files <500 LOC; split when exceeded.
- Bugs: add regression test when it fits.

## Git
- Commit helper: `scripts/committer "type(scope): message" file1 file2`.
- Conventional Commits: `feat|fix|refactor|docs|test|chore|perf|build|ci|style(scope): summary`.
- Atomic commits: one concern per commit; list each path explicitly.
- Destructive ops forbidden unless explicit consent.

## Build / Test
- Full gate: `go test ./... && go vet ./...`
- Quick check: `go build ./...`
- Single test: `go test -run TestName ./path/to/package`
- Lint: `staticcheck ./...` (if installed)

## Architecture
- `cmd/api/` — Thin shell entry point (14 lines); delegates to internal/cli.
- `internal/cli/` — Server wiring, Spanner connection, signal handling.
- `internal/domain/` — Carrier, Quote, AppetiteRule, Money, CoverageLine, errors (zero deps).
- `internal/ports/` — Interfaces: CarrierPort, OrchestratorPort, QuoteRepository, AppetiteRepository.
- `internal/adapter/` — Mock carriers, HTTP carrier, generic adapter registry.
- `internal/adapter/spanner/` — QuoteRepo, CarrierRepo, AppetiteRepo (Spanner client).
- `internal/orchestrator/` — Fan-out, singleflight dedup, adaptive hedging (EMA p95).
- `internal/circuitbreaker/` — 3-state machine (Closed/Open/HalfOpen), atomic ops.
- `internal/ratelimiter/` — Token bucket via x/time/rate.
- `internal/handler/` — HTTP handler (POST /quotes, /healthz, /readyz, /metrics).
- `internal/middleware/` — API key auth, security headers, concurrency limiter, audit.
- `internal/metrics/` — Prometheus recorder (gauges, histograms, counters).
- `internal/cleanup/` — Background expired quote cleanup ticker.
- `terraform/` — 8 IaC modules: Cloud Run, Spanner, Pub/Sub, IAM, Networking, Monitoring, Storage, KMS.
- `cmd/worker/` — Planned (not yet implemented); Pub/Sub subscriber for async event processing.

## Session Continuity
- `/handoff`: read `docs/handoff.md` — dump state for next session.
- `/pickup`: read `docs/pickup.md` — rehydrate context when starting.

## Docs Convention
- Every `docs/*.md` needs YAML front-matter: `summary` + `read_when`.
- Run `scripts/docs-list` to verify compliance.

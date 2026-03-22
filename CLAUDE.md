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
- `cmd/api/` — Cloud Run HTTP server; REST API for appetite matching.
- `cmd/worker/` — Pub/Sub subscriber; async event processing.
- `internal/domain/` — Domain models: Carrier, AppetiteRule, RiskClassification, MatchResult.
- `internal/service/` — Business logic: matching engine, AI classification.
- `internal/adapter/` — Driven ports: Spanner repository, Pub/Sub publisher, Claude client.
- `internal/handler/` — HTTP handlers (driving port).
- `terraform/` — IaC: Cloud Run, Spanner, Pub/Sub, IAM, Monitoring.

## Session Continuity
- `/handoff`: read `docs/handoff.md` — dump state for next session.
- `/pickup`: read `docs/pickup.md` — rehydrate context when starting.

## Docs Convention
- Every `docs/*.md` needs YAML front-matter: `summary` + `read_when`.
- Run `scripts/docs-list` to verify compliance.

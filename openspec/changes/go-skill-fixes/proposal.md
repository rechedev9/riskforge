# Proposal: Go Skill Fixes

**Change ID**: go-skill-fixes
**Date**: 2026-03-22
**Status**: draft

---

## Intent

Address 5 Go skill audit violations in the riskforge carrier gateway. These are code quality and correctness issues — not feature work. The fixes improve testability (thin main), readability (config structs), safety (signal handling, type safety), and operational resilience (limiter cleanup).

## Scope

### In Scope

- **Fix 1 (CRITICAL)**: Extract `cmd/api/main.go` wiring to `internal/cli/run.go`; main.go becomes a 5-line thin shell calling `cli.Run()`.
- **Fix 2 (CRITICAL)**: Replace positional args in `orchestrator.New()` (9 params) and `handler.New()` (5 params) with config structs `OrchestratorConfig` and `HandlerConfig`.
- **Fix 3 (HIGH)**: Replace manual `signal.Notify` + channel with `signal.NotifyContext` (folded into Fix 1).
- **Fix 4 (HIGH)**: Change `EligibilityCriteria` from `map[string]any` to `json.RawMessage` for deferred parsing.
- **Fix 5 (HIGH)**: Add background cleanup goroutine to `authFailureLimiter` with 5-minute TTL, 60-second sweep interval.

### Out of Scope

- **Fix 6 (consumer-defined interfaces)**: SKIPPED — the `ports/` package pattern is valid for hexagonal architecture. Moving interfaces to consumers would break the established pattern.
- New features or API changes.
- `cmd/worker/` modifications.
- Terraform or CI changes.

## Approach

5 phases, bottom-up. Each phase is independently compilable and testable.

1. **Fix 4 — EligibilityCriteria type** (leaf change, no dependents)
2. **Fix 5 — Limiter cleanup** (self-contained in middleware)
3. **Fix 2 — Config structs** (orchestrator + handler + all call sites)
4. **Fix 1 + Fix 3 — Thin shell + signal.NotifyContext** (creates `internal/cli/`, rewrites `main.go`)
5. **Verify** — `go build ./...`, `go vet ./...`, `go test ./...`

## Risk

Low. All changes are internal refactoring. No public API, no wire protocol, no Terraform changes. Test suite validates correctness after each phase.

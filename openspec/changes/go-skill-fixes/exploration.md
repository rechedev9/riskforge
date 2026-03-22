# Exploration: go-skill-fixes

## Current State

The riskforge carrier gateway (`cmd/api/`) is a working Go 1.25 HTTP server with hexagonal architecture. A Go skill audit identified 6 violations; 5 are actionable (Fix 6 — consumer-defined interfaces — is skipped because the `ports/` package pattern is valid for hexagonal architecture).

### Violation Summary

| # | Violation | Severity | Current Code |
|---|-----------|----------|-------------|
| 1 | Fat main.go / thin shell | CRITICAL | `cmd/api/main.go` is 200 LOC with all wiring, env parsing, shutdown logic |
| 2 | Positional args in constructors | CRITICAL | `orchestrator.New()` takes 9 positional args; `handler.New()` takes 5 positional args |
| 3 | Manual signal handling | HIGH | `quit := make(chan os.Signal, 1)` + goroutine pattern instead of `signal.NotifyContext` |
| 4 | Untyped EligibilityCriteria | HIGH | `map[string]any` in `AppetiteRule` — loses schema info, forces type assertions |
| 5 | Unbounded per-IP limiter map | HIGH | `sync.Map` in `authFailureLimiter` grows forever — memory leak under attack |
| 6 | Provider-side interfaces (SKIPPED) | MEDIUM | Interfaces defined in `ports/` — valid hexagonal arch pattern |

### Quantified Impact

- **Fix 1**: `cmd/api/main.go` = 200 LOC; target = 5–15 LOC. Enables `main()` to be tested as a regular function.
- **Fix 2**: `orchestrator.New()` = 9 positional args on line 60–70 of `orchestrator.go`; `handler.New()` = 5 positional args on line 48 of `http.go`. Call sites: 4 for orchestrator (1 prod + 3 test), 2 for handler (1 prod + 1 test).
- **Fix 3**: 8 lines of manual signal boilerplate (lines 117–126 of `main.go`) replaced by 1 `signal.NotifyContext` call.
- **Fix 4**: `EligibilityCriteria map[string]any` on line 14 of `appetite.go`. Used in domain only; no call sites parse it yet.
- **Fix 5**: `authFailureLimiter.ips` is `sync.Map` (line 30 of `auth.go`). No eviction — an attacker spoofing IPs can grow this map unboundedly.

---

## Relevant Files

### Modified (existing)

| File | LOC | Role |
|------|-----|------|
| `cmd/api/main.go` | 200 | Entry point — rewritten to thin shell (Fixes 1, 3) |
| `internal/orchestrator/orchestrator.go` | ~300 | Orchestrator constructor — config struct (Fix 2) |
| `internal/handler/http.go` | ~200 | Handler constructor — config struct (Fix 2) |
| `internal/middleware/auth.go` | 163 | Auth middleware — limiter cleanup (Fix 5) |
| `internal/domain/appetite.go` | 25 | AppetiteRule — json.RawMessage (Fix 4) |

### Created (new)

| File | Role |
|------|------|
| `internal/cli/run.go` | Fat core: all wiring logic extracted from main.go (Fix 1) |

### Call Sites (must update)

| File | Call | Fix |
|------|------|-----|
| `cmd/api/main.go:76` | `orchestrator.New(...)` | 2 |
| `cmd/api/main.go:88` | `handler.New(...)` | 2 |
| `internal/orchestrator/orchestrator_test.go:106` | `orchestrator.New(...)` | 2 |
| `internal/orchestrator/orchestrator_concurrency_test.go:330` | `orchestrator.New(...)` | 2 |
| `internal/orchestrator/orchestrator_random_test.go:118` | `orchestrator.New(...)` | 2 |
| `internal/handler/http_test.go:43` | `handler.New(...)` | 2 |
| `cmd/api/main.go:97` | `middleware.RequireAPIKey(...)` | 5 (return cleanup func) |
| `internal/middleware/middleware_test.go` | `RequireAPIKey(...)` | 5 (if signature changes) |

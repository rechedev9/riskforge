# Implementation Tasks: Go Skill Fixes

**Change**: go-skill-fixes
**Date**: 2026-03-22
**Status**: draft
**Depends On**: propose.md, spec.md, design.md

---

## Phase 1: Fix 4 — EligibilityCriteria Type

Leaf change with no dependents. Safe to do first.

- [ ] 1.1 Change EligibilityCriteria to json.RawMessage

**File**: `internal/domain/appetite.go`

**Actions**:
1. Add `import "encoding/json"` to the file
2. Change `EligibilityCriteria map[string]any` to `EligibilityCriteria json.RawMessage`
3. Update the comment from `// parsed from JSON` to `// deferred parsing; unmarshal to concrete types when needed`

**Verify**:
```bash
go build ./internal/domain/
go vet ./internal/domain/
```

---

## Phase 2: Fix 5 — Per-IP Limiter Cleanup

Self-contained in middleware package. Changes `RequireAPIKey` return signature.

- [ ] 2.1 Add lastSeen tracking to authFailureLimiter

**File**: `internal/middleware/auth.go`

**Actions**:
1. Add `lastSeen sync.Map` field to `authFailureLimiter` struct
2. In `getOrCreate`, add `l.lastSeen.Store(ip, time.Now())` before the existing Load check
3. Add `startCleanup(ctx context.Context, interval, ttl time.Duration) func()` method:
   - Creates child context with cancel
   - Starts goroutine with `time.NewTicker(interval)`
   - On each tick, ranges `lastSeen`; deletes entries where `time.Since(ts) > ttl` from both maps
   - Returns the cancel func as stop handle
4. Add `"context"` to imports (already present, verify)

**Verify**:
```bash
go build ./internal/middleware/
```

- [ ] 2.2 Change RequireAPIKey return signature

**File**: `internal/middleware/auth.go`

**Actions**:
1. Change signature from `func RequireAPIKey(...) http.Handler` to `func RequireAPIKey(...) (http.Handler, func())`
2. After `limiter := newAuthFailureLimiter()`, add: `stopCleanup := limiter.startCleanup(context.Background(), 60*time.Second, 5*time.Minute)`
3. Change return to: `return http.HandlerFunc(func(...) { ... }), stopCleanup`

**Verify**:
```bash
go build ./internal/middleware/
```

- [ ] 2.3 Update RequireAPIKey call site in main.go

**File**: `cmd/api/main.go`

**Actions**:
1. Change `srv = middleware.RequireAPIKey(srv, apiKeys, skipPaths, log)` to:
   `authHandler, stopCleanup := middleware.RequireAPIKey(srv, apiKeys, skipPaths, log)`
   `defer stopCleanup()`
   `srv = authHandler`

**Verify**:
```bash
go build ./cmd/api/
```

- [ ] 2.4 Update RequireAPIKey call sites in tests

**File**: `internal/middleware/middleware_test.go`

**Actions**:
1. Find all calls to `RequireAPIKey` in tests
2. Update to capture both return values: `h, stop := middleware.RequireAPIKey(...)`
3. Add `defer stop()` after each

**Verify**:
```bash
go test ./internal/middleware/...
```

---

## Phase 3: Fix 2 — Config Structs

Modifies constructor signatures in orchestrator and handler. Touches 6 files total.

- [ ] 3.1 Add OrchestratorConfig struct and refactor New()

**File**: `internal/orchestrator/orchestrator.go`

**Actions**:
1. Add `OrchestratorConfig` struct above `New()` with exported fields:
   - `Carriers []domain.Carrier`
   - `Registry *adapter.Registry`
   - `Breakers map[string]*circuitbreaker.Breaker`
   - `Limiters map[string]*ratelimiter.Limiter`
   - `Trackers map[string]*EMATracker`
   - `Metrics ports.MetricsRecorder`
   - `Cfg Config`
   - `Log *slog.Logger`
   - `Repo ports.QuoteRepository`
2. Change `func New(carriers []domain.Carrier, ..., repo ports.QuoteRepository) *Orchestrator` to `func New(c OrchestratorConfig) *Orchestrator`
3. Update body: reference `c.Carriers`, `c.Registry`, etc.
4. Update doc comment on New()

**Verify**:
```bash
go build ./internal/orchestrator/
```

- [ ] 3.2 Update orchestrator test call sites

**Files**:
- `internal/orchestrator/orchestrator_test.go:106`
- `internal/orchestrator/orchestrator_concurrency_test.go:330`
- `internal/orchestrator/orchestrator_random_test.go:118`

**Actions**:
1. Replace positional args with `orchestrator.OrchestratorConfig{...}` struct literal
2. Map each positional arg to the named field

**Verify**:
```bash
go test ./internal/orchestrator/...
```

- [ ] 3.3 Add HandlerConfig struct and refactor New()

**File**: `internal/handler/http.go`

**Actions**:
1. Add `HandlerConfig` struct above `New()` with exported fields:
   - `Orch ports.OrchestratorPort`
   - `Metrics ports.MetricsRecorder`
   - `Gatherer prometheus.Gatherer`
   - `Log *slog.Logger`
   - `DB *sql.DB`
2. Change `func New(orch ports.OrchestratorPort, ..., db *sql.DB) *Handler` to `func New(c HandlerConfig) *Handler`
3. Update body: reference `c.Orch`, `c.Metrics`, etc.
4. Update doc comment on New()

**Verify**:
```bash
go build ./internal/handler/
```

- [ ] 3.4 Update handler test call site

**File**: `internal/handler/http_test.go:43`

**Actions**:
1. Replace `handler.New(orch, rec, reg, log, nil)` with `handler.New(handler.HandlerConfig{...})`

**Verify**:
```bash
go test ./internal/handler/...
```

- [ ] 3.5 Update main.go call sites for orchestrator.New and handler.New

**File**: `cmd/api/main.go`

**Actions**:
1. Replace `orchestrator.New(carriers, registry, ..., nil)` with `orchestrator.New(orchestrator.OrchestratorConfig{...})`
2. Replace `handler.New(orch, rec, promReg, log, nil)` with `handler.New(handler.HandlerConfig{...})`

**Verify**:
```bash
go build ./cmd/api/
```

---

## Phase 4: Fix 1 + Fix 3 — Thin Shell + signal.NotifyContext

Creates `internal/cli/run.go`, rewrites `cmd/api/main.go` to thin shell.

- [ ] 4.1 Create internal/cli/run.go

**File**: `internal/cli/run.go` (NEW)

**Actions**:
1. Create `internal/cli/` directory
2. Create `run.go` with package `cli`
3. Move all logic from current `main()` into `func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error`
4. Move `buildCarriers()` into the same file (unexported)
5. Add `envOrDefault(key, fallback string) string` helper using `os.Getenv`
6. Replace `os.Exit(1)` with `return fmt.Errorf(...)`
7. Replace manual signal channel with `signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)`
8. Use `slog.New(slog.NewJSONHandler(stdout, ...))` — write to the injected `stdout`
9. Use config structs from Fix 2 for orchestrator.New and handler.New
10. Handle RequireAPIKey's `(handler, stopFunc)` return from Fix 5

**Verify**:
```bash
go build ./internal/cli/
```

- [ ] 4.2 Rewrite cmd/api/main.go as thin shell

**File**: `cmd/api/main.go`

**Actions**:
1. Replace entire file with:
   ```go
   package main

   import (
       "context"
       "os"

       "github.com/rechedev9/riskforge/internal/cli"
   )

   func main() {
       if err := cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
           os.Exit(1)
       }
   }
   ```

**Verify**:
```bash
go build ./cmd/api/
```

---

## Phase 5: Full Verification

- [ ] 5.1 Build all packages

```bash
go build ./...
```

- [ ] 5.2 Vet all packages

```bash
go vet ./...
```

- [ ] 5.3 Run all tests

```bash
go test ./...
```

- [ ] 5.4 Verify main.go line count

```bash
wc -l cmd/api/main.go
# Expected: <= 15 lines
```

- [ ] 5.5 Verify no manual signal.Notify remains

```bash
grep -r "signal.Notify(" --include="*.go" .
# Expected: 0 matches (only signal.NotifyContext should exist)
```

- [ ] 5.6 Verify no map[string]any for EligibilityCriteria

```bash
grep -r "map\[string\]any" internal/domain/
# Expected: 0 matches
```

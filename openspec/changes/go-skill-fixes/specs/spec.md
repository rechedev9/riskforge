# Spec: Go Skill Fixes

**Change**: go-skill-fixes
**Date**: 2026-03-22
**Status**: draft
**Depends On**: propose.md

---

## Fix 1: Thin Shell / Fat Core

- [ ] `cmd/api/main.go` is <= 15 LOC (package declaration + import + main func)
- [ ] `main()` calls `cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)` and exits with code 1 on error
- [ ] `internal/cli/run.go` exists in package `cli`
- [ ] `cli.Run` signature: `func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error`
- [ ] All env parsing, carrier building, server wiring, and shutdown logic lives in `cli.Run`
- [ ] No `os.Exit` calls inside `cli.Run` — returns error instead
- [ ] `go build ./cmd/api/` produces a working binary

## Fix 2: Config Structs

### OrchestratorConfig

- [ ] `OrchestratorConfig` struct defined in `internal/orchestrator/orchestrator.go`
- [ ] Fields: `Carriers []domain.Carrier`, `Registry *adapter.Registry`, `Breakers map[string]*circuitbreaker.Breaker`, `Limiters map[string]*ratelimiter.Limiter`, `Trackers map[string]*EMATracker`, `Metrics ports.MetricsRecorder`, `Cfg Config`, `Log *slog.Logger`, `Repo ports.QuoteRepository`
- [ ] `orchestrator.New` signature changes to `func New(c OrchestratorConfig) *Orchestrator`
- [ ] All 4 call sites updated (1 prod, 3 test files)
- [ ] `go test ./internal/orchestrator/...` passes

### HandlerConfig

- [ ] `HandlerConfig` struct defined in `internal/handler/http.go`
- [ ] Fields: `Orch ports.OrchestratorPort`, `Metrics ports.MetricsRecorder`, `Gatherer prometheus.Gatherer`, `Log *slog.Logger`, `DB *sql.DB`
- [ ] `handler.New` signature changes to `func New(c HandlerConfig) *Handler`
- [ ] All 2 call sites updated (1 prod, 1 test file)
- [ ] `go test ./internal/handler/...` passes

## Fix 3: signal.NotifyContext

- [ ] No manual `signal.Notify` channel in codebase
- [ ] `signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)` used in `cli.Run`
- [ ] Context cancellation triggers `httpSrv.Shutdown`

## Fix 4: EligibilityCriteria Type

- [ ] `AppetiteRule.EligibilityCriteria` type is `json.RawMessage` (not `map[string]any`)
- [ ] Import `encoding/json` added to `internal/domain/appetite.go`
- [ ] `go build ./internal/domain/` succeeds
- [ ] No other files reference `EligibilityCriteria` as `map[string]any`

## Fix 5: Per-IP Limiter Cleanup

- [ ] `authFailureLimiter` struct gains a `lastSeen sync.Map` (maps IP -> `time.Time`)
- [ ] `getOrCreate` updates `lastSeen` on every call
- [ ] New method `startCleanup(ctx context.Context)` starts a background goroutine
- [ ] Cleanup goroutine runs every 60 seconds
- [ ] Entries with `lastSeen` older than 5 minutes are deleted from both `ips` and `lastSeen`
- [ ] Cleanup goroutine stops when context is cancelled
- [ ] `RequireAPIKey` returns `(http.Handler, func())` — the func stops the cleanup goroutine
- [ ] All call sites of `RequireAPIKey` updated to handle the cleanup func
- [ ] `go test ./internal/middleware/...` passes

## Cross-Cutting

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` succeeds
- [ ] `go test ./...` passes (all existing tests)
- [ ] No new packages added to `go.mod`

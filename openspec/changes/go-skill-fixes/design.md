# Technical Design: Go Skill Fixes

**Change**: go-skill-fixes
**Date**: 2026-03-22
**Status**: draft
**Depends On**: propose.md, spec.md

---

## Architecture Decisions

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | cli.Run signature | `func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error` | Testable without os.Exit; injectable writers for log capture; ctx enables signal.NotifyContext |
| 2 | Config struct naming | `OrchestratorConfig`, `HandlerConfig` | Co-located with the type they configure; avoids `opts` or `params` naming ambiguity |
| 3 | EligibilityCriteria type | `json.RawMessage` | Deferred parsing: callers decode into concrete types when needed. No schema loss. Zero-value is `nil` (same as `map[string]any` zero) |
| 4 | Limiter cleanup mechanism | Background goroutine with ticker + context cancellation | Simplest approach; no external dependencies; ticker is efficient; context cancellation ensures clean shutdown |
| 5 | RequireAPIKey return signature | `(http.Handler, func())` | Caller receives a stop function to halt the background cleanup goroutine on shutdown. Minimal API change |
| 6 | Fix 6 skipped | Keep ports/ package | Hexagonal architecture convention: interfaces at the boundary layer (ports) is the standard pattern. Moving to consumers would fragment the contract definitions |

---

## Fix 1 + Fix 3: Thin Shell + signal.NotifyContext

### internal/cli/run.go (new file)

```go
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rechedev9/riskforge/internal/adapter"
	"github.com/rechedev9/riskforge/internal/circuitbreaker"
	"github.com/rechedev9/riskforge/internal/domain"
	"github.com/rechedev9/riskforge/internal/handler"
	"github.com/rechedev9/riskforge/internal/metrics"
	"github.com/rechedev9/riskforge/internal/middleware"
	"github.com/rechedev9/riskforge/internal/orchestrator"
	"github.com/rechedev9/riskforge/internal/ratelimiter"
)

// Run is the fat core of the API server. It wires all dependencies, starts
// the HTTP server, and blocks until a termination signal is received.
// Returns nil on clean shutdown, error on failure.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	log := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	port := envOrDefault("PORT", "8080")

	apiKeys := strings.Split(envOrDefault("API_KEYS", ""), ",")
	if len(apiKeys) == 0 || apiKeys[0] == "" {
		return fmt.Errorf("API_KEYS environment variable required")
	}

	// signal.NotifyContext replaces manual channel + goroutine.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	promReg := prometheus.NewRegistry()
	rec := metrics.New(promReg)

	carriers := buildCarriers()

	registry := adapter.NewRegistry()
	breakers := make(map[string]*circuitbreaker.Breaker)
	limiters := make(map[string]*ratelimiter.Limiter)
	trackers := make(map[string]*orchestrator.EMATracker)

	for _, c := range carriers {
		breakers[c.ID] = circuitbreaker.New(c.ID, circuitbreaker.Config{
			FailureThreshold: c.Config.FailureThreshold,
			SuccessThreshold: c.Config.SuccessThreshold,
			OpenTimeout:      c.Config.OpenTimeout,
		}, rec)

		limiters[c.ID] = ratelimiter.New(c.ID, c.Config.RateLimit, rec)

		trackers[c.ID] = orchestrator.NewEMATracker(
			c.ID, c.Config.TimeoutHint, c.Config, rec,
		)

		mock := adapter.NewMockCarrier(c.ID, adapter.MockConfig{
			BaseLatency: c.Config.TimeoutHint,
			JitterMs:    10,
			FailureRate: 0.0,
		}, log)
		fn := adapter.RegisterMockCarrier(mock)
		registry.Register(c.ID, fn)
	}

	orch := orchestrator.New(orchestrator.OrchestratorConfig{
		Carriers: carriers,
		Registry: registry,
		Breakers: breakers,
		Limiters: limiters,
		Trackers: trackers,
		Metrics:  rec,
		Cfg:      orchestrator.Config{},
		Log:      log,
		Repo:     nil,
	})

	h := handler.New(handler.HandlerConfig{
		Orch:     orch,
		Metrics:  rec,
		Gatherer: promReg,
		Log:      log,
		DB:       nil,
	})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	skipPaths := []string{"/healthz", "/readyz", "/metrics"}

	var srv http.Handler = mux
	srv = middleware.LimitConcurrency(srv, 100, log)
	authHandler, stopCleanup := middleware.RequireAPIKey(srv, apiKeys, skipPaths, log)
	defer stopCleanup()
	srv = authHandler
	srv = middleware.SecurityHeaders(srv)
	srv = middleware.AuditLog(srv, log)

	httpSrv := &http.Server{
		Addr:         ":" + port,
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in background.
	errCh := make(chan error, 1)
	go func() {
		log.Info("starting server", "addr", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Block until signal or server error.
	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}
	return nil
}
```

### cmd/api/main.go (rewritten)

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

Key changes from current main.go:
- `os.Exit(1)` only in main(); `cli.Run` returns errors
- `signal.NotifyContext` replaces manual `quit` channel (Fix 3)
- `buildCarriers()` and `envOrDefault()` move to `internal/cli/` as unexported helpers

---

## Fix 2: Config Structs

### orchestrator.OrchestratorConfig

```go
// OrchestratorConfig holds all dependencies for constructing an Orchestrator.
type OrchestratorConfig struct {
	Carriers []domain.Carrier
	Registry *adapter.Registry
	Breakers map[string]*circuitbreaker.Breaker
	Limiters map[string]*ratelimiter.Limiter
	Trackers map[string]*EMATracker
	Metrics  ports.MetricsRecorder
	Cfg      Config
	Log      *slog.Logger
	Repo     ports.QuoteRepository // optional; nil disables persistence
}
```

### New() refactored

```go
func New(c OrchestratorConfig) *Orchestrator {
	if c.Cfg.HedgePollInterval <= 0 {
		c.Cfg.HedgePollInterval = defaultHedgePollInterval
	}
	return &Orchestrator{
		carriers: c.Carriers,
		registry: c.Registry,
		breakers: c.Breakers,
		limiters: c.Limiters,
		trackers: c.Trackers,
		metrics:  c.Metrics,
		repo:     c.Repo,
		cfg:      c.Cfg,
		log:      c.Log,
	}
}
```

### handler.HandlerConfig

```go
// HandlerConfig holds all dependencies for constructing a Handler.
type HandlerConfig struct {
	Orch     ports.OrchestratorPort
	Metrics  ports.MetricsRecorder
	Gatherer prometheus.Gatherer
	Log      *slog.Logger
	DB       *sql.DB // optional; nil when no database configured
}
```

### New() refactored

```go
func New(c HandlerConfig) *Handler {
	return &Handler{
		orch:     c.Orch,
		metrics:  c.Metrics,
		gatherer: c.Gatherer,
		log:      c.Log,
		db:       c.DB,
	}
}
```

### Call site updates

**orchestrator_test.go:106** — before:
```go
return orchestrator.New(
    f.carriers, f.registry, f.breakers, f.limiters, f.trackers,
    f.metrics, orchestrator.Config{HedgePollInterval: 5 * time.Millisecond},
    discardLog, nil,
)
```

After:
```go
return orchestrator.New(orchestrator.OrchestratorConfig{
    Carriers: f.carriers,
    Registry: f.registry,
    Breakers: f.breakers,
    Limiters: f.limiters,
    Trackers: f.trackers,
    Metrics:  f.metrics,
    Cfg:      orchestrator.Config{HedgePollInterval: 5 * time.Millisecond},
    Log:      discardLog,
    Repo:     nil,
})
```

**handler/http_test.go:43** — before:
```go
return handler.New(orch, rec, reg, log, nil)
```

After:
```go
return handler.New(handler.HandlerConfig{
    Orch:     orch,
    Metrics:  rec,
    Gatherer: reg,
    Log:      log,
    DB:       nil,
})
```

---

## Fix 4: EligibilityCriteria Type

### internal/domain/appetite.go

```go
package domain

import "encoding/json"

type AppetiteRule struct {
	RuleID              string
	CarrierID           string
	State               string
	LineOfBusiness      string
	ClassCode           string
	MinPremium          float64
	MaxPremium          float64
	IsActive            bool
	EligibilityCriteria json.RawMessage // deferred parsing; was map[string]any
}
```

`json.RawMessage` is `[]byte` underneath — it preserves the raw JSON for callers to unmarshal into concrete types when needed. Zero value is `nil`, same semantics as the old `map[string]any` zero.

---

## Fix 5: Per-IP Limiter Cleanup

### authFailureLimiter changes

```go
type authFailureLimiter struct {
	ips      sync.Map // IP -> *rate.Limiter
	lastSeen sync.Map // IP -> time.Time
	rate     rate.Limit
	burst    int
}

func (l *authFailureLimiter) getOrCreate(ip string) *rate.Limiter {
	// Update last-seen timestamp on every access.
	l.lastSeen.Store(ip, time.Now())

	if v, ok := l.ips.Load(ip); ok {
		return v.(*rate.Limiter)
	}
	lim := rate.NewLimiter(l.rate, l.burst)
	actual, _ := l.ips.LoadOrStore(ip, lim)
	return actual.(*rate.Limiter)
}

// startCleanup runs a background goroutine that evicts entries older than ttl
// every interval. Returns a stop function.
func (l *authFailureLimiter) startCleanup(ctx context.Context, interval, ttl time.Duration) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				l.lastSeen.Range(func(key, value any) bool {
					if now.Sub(value.(time.Time)) > ttl {
						l.ips.Delete(key)
						l.lastSeen.Delete(key)
					}
					return true
				})
			}
		}
	}()
	return cancel
}
```

### RequireAPIKey signature change

```go
// RequireAPIKey returns the auth middleware handler and a stop function.
// The stop function must be called on shutdown to halt the background
// limiter cleanup goroutine.
func RequireAPIKey(next http.Handler, keys []string, skipPaths []string, log *slog.Logger) (http.Handler, func()) {
	// ... same key/skip setup ...

	limiter := newAuthFailureLimiter()
	stopCleanup := limiter.startCleanup(context.Background(), 60*time.Second, 5*time.Minute)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ... same handler logic ...
	}), stopCleanup
}
```

---

## Dependency Graph

```
Fix 4 (appetite.go)           — leaf, no dependents
Fix 5 (auth.go)               — leaf, signature change affects main.go call site
Fix 2 (config structs)        — modifies orchestrator.go, http.go + all call sites
Fix 1 + Fix 3 (cli/run.go)   — depends on Fix 2 (uses config structs) + Fix 5 (handles stopCleanup)
```

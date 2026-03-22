// Package cli wires all dependencies and runs the API server.
// main.go delegates to Run — keeping the entry point thin and testable.
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rechedev9/riskforge/internal/adapter"
	spanneradapter "github.com/rechedev9/riskforge/internal/adapter/spanner"
	"github.com/rechedev9/riskforge/internal/circuitbreaker"
	"github.com/rechedev9/riskforge/internal/cleanup"
	"github.com/rechedev9/riskforge/internal/domain"
	"github.com/rechedev9/riskforge/internal/handler"
	"github.com/rechedev9/riskforge/internal/metrics"
	"github.com/rechedev9/riskforge/internal/middleware"
	"github.com/rechedev9/riskforge/internal/orchestrator"
	"github.com/rechedev9/riskforge/internal/ports"
	"github.com/rechedev9/riskforge/internal/ratelimiter"
)

// Run is the fat core of the API server. It wires all dependencies, starts
// the HTTP server, and blocks until a termination signal is received.
// Returns nil on clean shutdown, error on failure.
func Run(ctx context.Context, _ []string, stdout, _ io.Writer) error {
	log := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	port := envOrDefault("PORT", "8080")

	apiKeys := strings.Split(envOrDefault("API_KEYS", ""), ",")
	if len(apiKeys) == 0 || apiKeys[0] == "" {
		return fmt.Errorf("API_KEYS environment variable required")
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	promReg := prometheus.NewRegistry()
	rec := metrics.New(promReg)

	// Spanner connection (optional — falls back to mock carriers).
	var quoteRepo ports.QuoteRepository
	spannerProject := os.Getenv("SPANNER_PROJECT")
	spannerInstance := os.Getenv("SPANNER_INSTANCE")
	spannerDB := os.Getenv("SPANNER_DATABASE")

	var carriers []domain.Carrier

	if spannerProject != "" && spannerInstance != "" && spannerDB != "" {
		spannerClient, err := spanneradapter.NewClient(ctx, spannerProject, spannerInstance, spannerDB)
		if err != nil {
			return fmt.Errorf("spanner connect: %w", err)
		}
		defer spannerClient.Close()
		log.Info("connected to spanner", "project", spannerProject, "instance", spannerInstance, "db", spannerDB)

		// Load carriers from Spanner.
		carrierRepo := spanneradapter.NewCarrierRepo(spannerClient)
		dbCarriers, err := carrierRepo.ListActive(ctx)
		if err != nil {
			log.Warn("failed to load carriers from spanner, using mock carriers", "err", err)
			carriers = buildCarriers()
		} else if len(dbCarriers) == 0 {
			log.Warn("no carriers in spanner, using mock carriers")
			carriers = buildCarriers()
		} else {
			carriers = dbCarriers
			log.Info("loaded carriers from spanner", "count", len(carriers))
		}

		qr := spanneradapter.NewQuoteRepo(spannerClient)
		quoteRepo = qr

		// Start expired quote cleanup.
		ticker := cleanup.New(qr, 5*time.Minute, log)
		go ticker.Start(ctx)
	} else {
		log.Info("no spanner config, using mock carriers")
		carriers = buildCarriers()
	}

	registry := adapter.NewRegistry()
	breakers := make(map[string]*circuitbreaker.Breaker)
	limiters := make(map[string]*ratelimiter.Limiter)
	trackers := make(map[string]*orchestrator.EMATracker)

	for _, c := range carriers {
		// Apply defaults for carriers loaded from Spanner without full config.
		cfg := c.Config
		if cfg.FailureThreshold == 0 {
			cfg.FailureThreshold = 5
		}
		if cfg.SuccessThreshold == 0 {
			cfg.SuccessThreshold = 2
		}
		if cfg.OpenTimeout == 0 {
			cfg.OpenTimeout = 30 * time.Second
		}
		if cfg.TimeoutHint == 0 {
			cfg.TimeoutHint = 200 * time.Millisecond
		}
		if cfg.HedgeMultiplier == 0 {
			cfg.HedgeMultiplier = 1.5
		}
		if cfg.EMAWindowSize == 0 {
			cfg.EMAWindowSize = 19
		}
		if cfg.EMAWarmupObservations == 0 {
			cfg.EMAWarmupObservations = 10
		}
		if cfg.RateLimit.TokensPerSecond == 0 {
			cfg.RateLimit.TokensPerSecond = 100
		}
		if cfg.RateLimit.Burst == 0 {
			cfg.RateLimit.Burst = 10
		}

		breakers[c.ID] = circuitbreaker.New(c.ID, circuitbreaker.Config{
			FailureThreshold: cfg.FailureThreshold,
			SuccessThreshold: cfg.SuccessThreshold,
			OpenTimeout:      cfg.OpenTimeout,
		}, rec)

		limiters[c.ID] = ratelimiter.New(c.ID, cfg.RateLimit, rec)

		trackers[c.ID] = orchestrator.NewEMATracker(
			c.ID,
			cfg.TimeoutHint,
			cfg,
			rec,
		)

		// Mock carriers for all (real HTTP carriers would be wired per-carrier config).
		mock := adapter.NewMockCarrier(c.ID, adapter.MockConfig{
			BaseLatency: cfg.TimeoutHint,
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
		Repo:     quoteRepo,
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
	authHandler, stopAuth := middleware.RequireAPIKey(srv, apiKeys, skipPaths, log)
	defer stopAuth()
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

	errCh := make(chan error, 1)
	go func() {
		log.Info("starting server", "addr", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

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
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func buildCarriers() []domain.Carrier {
	return []domain.Carrier{
		{
			ID:   "alpha",
			Name: "Alpha Insurance",
			Capabilities: []domain.CoverageLine{
				domain.CoverageLineAuto,
				domain.CoverageLineHomeowners,
			},
			Config: domain.CarrierConfig{
				TimeoutHint:           100 * time.Millisecond,
				FailureThreshold:      5,
				SuccessThreshold:      2,
				OpenTimeout:           30 * time.Second,
				HedgeMultiplier:       1.5,
				EMAWindowSize:         19,
				EMAWarmupObservations: 10,
				Priority:              1,
				RateLimit:             domain.RateLimitConfig{TokensPerSecond: 100, Burst: 10},
			},
		},
		{
			ID:   "beta",
			Name: "Beta Insurance",
			Capabilities: []domain.CoverageLine{
				domain.CoverageLineAuto,
				domain.CoverageLineHomeowners,
				domain.CoverageLineUmbrella,
			},
			Config: domain.CarrierConfig{
				TimeoutHint:           200 * time.Millisecond,
				FailureThreshold:      5,
				SuccessThreshold:      2,
				OpenTimeout:           30 * time.Second,
				HedgeMultiplier:       1.5,
				EMAWindowSize:         19,
				EMAWarmupObservations: 10,
				Priority:              2,
				RateLimit:             domain.RateLimitConfig{TokensPerSecond: 50, Burst: 5},
			},
		},
		{
			ID:   "gamma",
			Name: "Gamma Insurance",
			Capabilities: []domain.CoverageLine{
				domain.CoverageLineHomeowners,
				domain.CoverageLineUmbrella,
			},
			Config: domain.CarrierConfig{
				TimeoutHint:           800 * time.Millisecond,
				FailureThreshold:      5,
				SuccessThreshold:      2,
				OpenTimeout:           30 * time.Second,
				HedgeMultiplier:       1.5,
				EMAWindowSize:         19,
				EMAWarmupObservations: 10,
				Priority:              3,
				RateLimit:             domain.RateLimitConfig{TokensPerSecond: 100, Burst: 10},
			},
		},
	}
}

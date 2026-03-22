package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
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

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	apiKeys := strings.Split(os.Getenv("API_KEYS"), ",")
	if len(apiKeys) == 0 || apiKeys[0] == "" {
		log.Error("API_KEYS environment variable required")
		os.Exit(1)
	}

	// Prometheus registry — used for both metric recording and HTTP exposition.
	promReg := prometheus.NewRegistry()
	rec := metrics.New(promReg)

	// Build mock carriers for now -- real carriers loaded from Spanner in future phase.
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
			c.ID,
			c.Config.TimeoutHint,
			c.Config,
			rec,
		)

		mock := adapter.NewMockCarrier(c.ID, adapter.MockConfig{
			BaseLatency: c.Config.TimeoutHint,
			JitterMs:    10,
			FailureRate: 0.0,
		}, log)
		fn := adapter.RegisterMockCarrier(mock)
		registry.Register(c.ID, fn)
	}

	orch := orchestrator.New(
		carriers,
		registry,
		breakers,
		limiters,
		trackers,
		rec,
		orchestrator.Config{},
		log,
		nil, // no QuoteRepository for now
	)

	h := handler.New(orch, rec, promReg, log, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	skipPaths := []string{"/healthz", "/readyz", "/metrics"}

	var srv http.Handler = mux
	srv = middleware.LimitConcurrency(srv, 100, log)
	srv = middleware.RequireAPIKey(srv, apiKeys, skipPaths, log)
	srv = middleware.SecurityHeaders(srv)
	srv = middleware.AuditLog(srv, log)

	httpSrv := &http.Server{
		Addr:         ":" + port,
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info("starting server", "addr", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Error("shutdown error", "err", err)
	}
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
				RateLimit: domain.RateLimitConfig{
					TokensPerSecond: 100,
					Burst:           10,
				},
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
				RateLimit: domain.RateLimitConfig{
					TokensPerSecond: 50,
					Burst:           5,
				},
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
				RateLimit: domain.RateLimitConfig{
					TokensPerSecond: 100,
					Burst:           10,
				},
			},
		},
	}
}

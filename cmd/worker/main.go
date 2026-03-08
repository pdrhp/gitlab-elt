package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/robfig/cron/v3"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/pdrhp/gitlab-elt/internal/config"
	"github.com/pdrhp/gitlab-elt/internal/discovery"
	"github.com/pdrhp/gitlab-elt/internal/gitlab"
	"github.com/pdrhp/gitlab-elt/internal/health"
	"github.com/pdrhp/gitlab-elt/internal/mapper"
	"github.com/pdrhp/gitlab-elt/internal/metrics"
	"github.com/pdrhp/gitlab-elt/internal/repository"
	"github.com/pdrhp/gitlab-elt/internal/scheduler"
	"github.com/pdrhp/gitlab-elt/internal/sync"
	"github.com/pdrhp/gitlab-elt/internal/telemetry"
	"github.com/pdrhp/gitlab-elt/internal/transformer"
)

func main() {
	// Setup structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("starting gitlab-elt worker")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Build shared OTEL resource used by tracing.
	otelResource, err := metrics.BuildResource(cfg.Worker.OTel.Resource)
	if err != nil {
		slog.Error("failed to build otel resource", "error", err)
	}

	tracer := noop.NewTracerProvider().Tracer("github.com/pdrhp/gitlab-elt/worker")
	tracerProvider, tracerShutdown, err := telemetry.BuildTracerProvider(context.Background(), cfg.Worker.OTel.Tracing, otelResource, logger)
	if err != nil {
		slog.Error("failed to configure otlp tracer", "error", err)
	} else {
		if tracerProvider != nil {
			tracer = tracerProvider.Tracer("github.com/pdrhp/gitlab-elt/worker")
		}
		if tracerShutdown != nil {
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := tracerShutdown(shutdownCtx); err != nil {
					slog.Warn("otlp tracer shutdown failed", "error", err)
				}
			}()
		}
	}

	// Connect to PostgreSQL
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.Postgres.DSN())
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to database")

	// Initialize repository
	queries := repository.New(pool)

	// Initialize State Mapper and load mappings from DB
	stateMapper := mapper.New()
	if err := stateMapper.LoadFromDB(context.Background(), queries); err != nil {
		slog.Error("failed to load state mappings", "error", err)
		os.Exit(1)
	}

	// Initialize observability registry and optional OTLP exporter
	otelMeter, otelShutdown, err := metrics.BuildOTLPMeter(context.Background(), cfg.Worker.OTel.Metrics)
	if err != nil {
		slog.Error("failed to configure OTLP metrics exporter", "error", err)
	}
	if otelShutdown != nil {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := otelShutdown(shutdownCtx); err != nil {
				slog.Warn("otlp meter shutdown failed", "error", err)
			}
		}()
	}

	promRegistry := prometheus.NewRegistry()
	obs := metrics.New(promRegistry, otelMeter)
	obs.SetProjectsMonitored(float64(len(cfg.Gitlab.ProjectIDs)))

	// Initialize GitLab client
	gitlabClient := gitlab.NewClient(
		cfg.Gitlab.BaseURL,
		cfg.Gitlab.Token,
		gitlab.WithRateLimit(cfg.Gitlab.RateLimit),
		gitlab.WithMaxRetries(cfg.Gitlab.RetryMax),
		gitlab.WithMetrics(obs),
		gitlab.WithTracer(tracer),
	)

	// Initialize services
	discoverySvc := discovery.NewService(gitlabClient, queries, cfg.Gitlab.GroupIDs, obs, tracer)
	extractor := sync.NewExtractor(gitlabClient, queries, obs, tracer)
	transformerSvc := transformer.NewService(queries, stateMapper, obs, tracer)

	// Sync function: extract Bronze + transform to Silver
	syncFunc := func(ctx context.Context) error {
		ctx, span := tracer.Start(ctx, "sync.cycle",
			trace.WithAttributes(
				attribute.String("scheduler.mode", "peak"),
			),
		)
		defer span.End()

		slog.Info("sync: starting extraction + transformation cycle")

		// Step 1: Bronze extraction
		if err := extractor.ExtractAll(ctx); err != nil {
			slog.Error("sync: extraction failed", "error", err)
			span.RecordError(err)
			// Continue to transformation even if extraction partially failed
		}

		// Step 2: Silver transformation
		processed, err := transformerSvc.TransformAll(ctx)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("transformation failed: %w", err)
		}

		span.SetAttributes(attribute.Int("events.transformed", processed))
		slog.Info("sync: cycle complete", "events_transformed", processed)
		return nil
	}

	// Discovery function
	discoverFunc := func(ctx context.Context) error {
		ctx, span := tracer.Start(ctx, "discovery.run")
		defer span.End()

		err := discoverySvc.Run(ctx)
		if err != nil {
			span.RecordError(err)
		}
		return err
	}

	// Start healthcheck HTTP server with metrics endpoint
	mux := http.NewServeMux()
	metricsHandler := promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{})
	health.RegisterHandlers(mux, pool, metricsHandler)

	healthServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Worker.HealthPort),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("healthcheck server started", "port", cfg.Worker.HealthPort)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("healthcheck server error", "error", err)
		}
	}()

	// Setup scheduler
	cronLogger := cron.VerbosePrintfLogger(newCronLogger(logger))
	sched := scheduler.New(
		scheduler.Config{
			SyncPeak:    cfg.Scheduler.SyncPeak,
			SyncOffPeak: cfg.Scheduler.SyncOffPeak,
			Discovery:   cfg.Scheduler.Discovery,
		},
		syncFunc,
		discoverFunc,
		cronLogger,
	)

	if err := sched.Start(); err != nil {
		slog.Error("failed to start scheduler", "error", err)
		os.Exit(1)
	}

	// Run discovery once on startup
	go func() {
		slog.Info("running initial discovery on startup")
		if err := discoverFunc(context.Background()); err != nil {
			slog.Error("initial discovery failed", "error", err)
		}
	}()

	slog.Info("gitlab-elt worker ready",
		"health_port", cfg.Worker.HealthPort,
		"sync_peak", cfg.Scheduler.SyncPeak,
		"sync_offpeak", cfg.Scheduler.SyncOffPeak,
		"discovery", cfg.Scheduler.Discovery,
		"rate_limit", cfg.Gitlab.RateLimit,
		"groups", len(cfg.Gitlab.GroupIDs),
		"projects", len(cfg.Gitlab.ProjectIDs),
	)

	// Graceful shutdown: wait for SIGINT or SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	slog.Info("received shutdown signal", "signal", sig.String())

	// Stop scheduler and wait for running jobs
	stopCtx := sched.Stop()
	select {
	case <-stopCtx.Done():
		slog.Info("all cron jobs completed")
	case <-time.After(30 * time.Second):
		slog.Warn("shutdown timeout exceeded, forcing exit")
	}

	// Shutdown health server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("healthcheck server shutdown error", "error", err)
	}

	slog.Info("gitlab-elt worker stopped")
}

// cronLogger adapts slog to robfig/cron's Printf interface.
type cronLogger struct {
	logger *slog.Logger
}

func newCronLogger(logger *slog.Logger) *cronLogger {
	return &cronLogger{logger: logger}
}

func (l *cronLogger) Printf(format string, v ...interface{}) {
	l.logger.Info(fmt.Sprintf(format, v...))
}

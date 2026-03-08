package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/pdrhp/gitlab-elt/internal/config"
	"github.com/pdrhp/gitlab-elt/internal/discovery"
	"github.com/pdrhp/gitlab-elt/internal/gitlab"
	"github.com/pdrhp/gitlab-elt/internal/mapper"
	"github.com/pdrhp/gitlab-elt/internal/repository"
	"github.com/pdrhp/gitlab-elt/internal/sync"
	"github.com/pdrhp/gitlab-elt/internal/transformer"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("starting gitlab-elt backfill")
	start := time.Now()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

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

	queries := repository.New(pool)

	// Load state mapper
	stateMapper := mapper.New()
	if err := stateMapper.LoadFromDB(ctx, queries); err != nil {
		slog.Error("failed to load state mappings", "error", err)
		os.Exit(1)
	}

	// Use conservative rate limit for backfill (lower than normal to be safe)
	rateLimit := cfg.Gitlab.RateLimit
	if rateLimit > 10 {
		rateLimit = 10 // Cap at 10 req/s for backfill
	}

	tracer := noop.NewTracerProvider().Tracer("github.com/pdrhp/gitlab-elt/backfill")
	gitlabClient := gitlab.NewClient(
		cfg.Gitlab.BaseURL,
		cfg.Gitlab.Token,
		gitlab.WithRateLimit(rateLimit),
		gitlab.WithMaxRetries(cfg.Gitlab.RetryMax),
		gitlab.WithTracer(tracer),
	)

	// Step 1: Discovery
	slog.Info("backfill: step 1/3 - discovering projects")
	discoverySvc := discovery.NewService(gitlabClient, queries, cfg.Gitlab.GroupIDs, nil, tracer)
	if err := discoverySvc.Run(ctx); err != nil {
		slog.Error("backfill: discovery failed", "error", err)
		os.Exit(1)
	}

	// Step 2: Bronze Extraction (all projects, full history since epoch)
	slog.Info("backfill: step 2/3 - extracting Bronze data")
	extractor := sync.NewExtractor(gitlabClient, queries, nil, tracer)
	if err := extractor.ExtractAll(ctx); err != nil {
		slog.Error("backfill: extraction failed (some projects may have failed)", "error", err)
		// Continue to transformation
	}

	// Step 3: Silver Transformation
	slog.Info("backfill: step 3/3 - transforming to Silver")
	transformerSvc := transformer.NewService(queries, stateMapper, nil, tracer)
	processed, err := transformerSvc.TransformAll(ctx)
	if err != nil {
		slog.Error("backfill: transformation failed", "error", err)
		os.Exit(1)
	}

	duration := time.Since(start)
	slog.Info("backfill: complete",
		"events_processed", processed,
		"duration", duration.String(),
		"rate_limit", fmt.Sprintf("%d req/s", rateLimit),
	)
}

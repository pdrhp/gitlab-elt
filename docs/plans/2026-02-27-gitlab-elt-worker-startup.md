# GitLab ELT Worker - Project Startup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bootstrap a Go project from scratch that acts as a long-lived daemon, extracting GitLab issue events incrementally and loading them into PostgreSQL using an ELT pattern.

**Architecture:** The worker is a single-binary daemon that uses `robfig/cron/v3` for internal scheduling, `pgx/v5` + `sqlc` for type-safe database access, and `golang-migrate` for schema versioning. It polls the GitLab REST API with `updated_after` for incremental extraction and writes raw event data into PostgreSQL. Graceful shutdown is handled via OS signal interception.

**Tech Stack:** Go 1.22+, PostgreSQL 16, Docker Compose, robfig/cron/v3, pgx/v5, sqlc, golang-migrate, godotenv

---

## Pre-Requisites

Before starting, ensure these tools are installed:

- **Go 1.22+**: https://go.dev/doc/install
- **Docker + Docker Compose**: For local PostgreSQL
- **sqlc CLI**: `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`
- **golang-migrate CLI**: `go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`

---

## Task 1: Initialize Go Module and Directory Structure

**Files:**
- Create: `go.mod` (via `go mod init`)
- Create: All directories in the project layout

**Step 1: Initialize Go module**

Run:
```bash
cd /home/pedrohenrique/projects/go/pdrhp-app-gitlab-elt
go mod init github.com/pdrhp/gitlab-elt
```

Expected: `go.mod` file created with module name.

**Step 2: Create directory structure**

Run:
```bash
mkdir -p cmd/worker
mkdir -p internal/config
mkdir -p internal/domain
mkdir -p internal/gitlab
mkdir -p internal/repository
mkdir -p internal/service
mkdir -p db/migrations
mkdir -p db/query
```

Expected: All directories created. Verify with `find . -type d | sort`.

**Step 3: Create placeholder main.go**

Create file `cmd/worker/main.go`:
```go
package main

import "fmt"

func main() {
	fmt.Println("gitlab-elt worker starting...")
}
```

**Step 4: Verify the project compiles**

Run:
```bash
go run cmd/worker/main.go
```

Expected output: `gitlab-elt worker starting...`

**Step 5: Commit**

```bash
git init
git add .
git commit -m "chore: initialize Go module and project directory structure"
```

---

## Task 2: Docker Compose for Local PostgreSQL

**Files:**
- Create: `docker-compose.yml`
- Create: `.env.example`
- Create: `.gitignore`

**Step 1: Create .gitignore**

Create file `.gitignore`:
```
# Binaries
*.exe
*.exe~
*.dll
*.so
*.dylib
bin/

# Test binary
*.test

# Output of go coverage
*.out

# Environment
.env

# IDE
.idea/
.vscode/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db
```

**Step 2: Create .env.example**

Create file `.env.example`:
```env
# PostgreSQL
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=gitlab_elt
POSTGRES_PASSWORD=gitlab_elt_dev
POSTGRES_DB=gitlab_elt

# GitLab
GITLAB_BASE_URL=https://gitlab.com
GITLAB_TOKEN=your-gitlab-token-here
GITLAB_PROJECT_IDS=123,456

# Worker
WORKER_CRON_SCHEDULE=*/15 * * * *
```

**Step 3: Create docker-compose.yml**

Create file `docker-compose.yml`:
```yaml
services:
  postgres:
    image: postgres:16-alpine
    container_name: gitlab-elt-postgres
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-gitlab_elt}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-gitlab_elt_dev}
      POSTGRES_DB: ${POSTGRES_DB:-gitlab_elt}
    ports:
      - "${POSTGRES_PORT:-5432}:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-gitlab_elt}"]
      interval: 5s
      timeout: 5s
      retries: 5

volumes:
  pgdata:
```

**Step 4: Create .env file for local development**

Run:
```bash
cp .env.example .env
```

**Step 5: Start PostgreSQL and verify**

Run:
```bash
docker compose up -d
docker compose ps
```

Expected: `gitlab-elt-postgres` container running and healthy.

**Step 6: Test database connectivity**

Run:
```bash
docker compose exec postgres psql -U gitlab_elt -d gitlab_elt -c "SELECT 1;"
```

Expected: Query returns `1`.

**Step 7: Commit**

```bash
git add .
git commit -m "chore: add docker-compose for local PostgreSQL and env config"
```

---

## Task 3: Configuration Loading Module

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: Install dependency**

Run:
```bash
go get github.com/joho/godotenv
```

**Step 2: Write the failing test**

Create file `internal/config/config_test.go`:
```go
package config_test

import (
	"os"
	"testing"

	"github.com/pdrhp/gitlab-elt/internal/config"
)

func TestLoad_FromEnvVars(t *testing.T) {
	// Set required env vars
	os.Setenv("POSTGRES_HOST", "localhost")
	os.Setenv("POSTGRES_PORT", "5432")
	os.Setenv("POSTGRES_USER", "testuser")
	os.Setenv("POSTGRES_PASSWORD", "testpass")
	os.Setenv("POSTGRES_DB", "testdb")
	os.Setenv("GITLAB_BASE_URL", "https://gitlab.example.com")
	os.Setenv("GITLAB_TOKEN", "test-token")
	os.Setenv("GITLAB_PROJECT_IDS", "10,20,30")
	os.Setenv("WORKER_CRON_SCHEDULE", "*/5 * * * *")

	defer func() {
		os.Unsetenv("POSTGRES_HOST")
		os.Unsetenv("POSTGRES_PORT")
		os.Unsetenv("POSTGRES_USER")
		os.Unsetenv("POSTGRES_PASSWORD")
		os.Unsetenv("POSTGRES_DB")
		os.Unsetenv("GITLAB_BASE_URL")
		os.Unsetenv("GITLAB_TOKEN")
		os.Unsetenv("GITLAB_PROJECT_IDS")
		os.Unsetenv("WORKER_CRON_SCHEDULE")
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Postgres.Host != "localhost" {
		t.Errorf("expected host localhost, got %s", cfg.Postgres.Host)
	}
	if cfg.Postgres.Port != 5432 {
		t.Errorf("expected port 5432, got %d", cfg.Postgres.Port)
	}
	if cfg.Gitlab.BaseURL != "https://gitlab.example.com" {
		t.Errorf("expected base url https://gitlab.example.com, got %s", cfg.Gitlab.BaseURL)
	}
	if cfg.Gitlab.Token != "test-token" {
		t.Errorf("expected token test-token, got %s", cfg.Gitlab.Token)
	}
	if len(cfg.Gitlab.ProjectIDs) != 3 {
		t.Fatalf("expected 3 project IDs, got %d", len(cfg.Gitlab.ProjectIDs))
	}
	if cfg.Gitlab.ProjectIDs[0] != 10 {
		t.Errorf("expected first project ID 10, got %d", cfg.Gitlab.ProjectIDs[0])
	}
	if cfg.Worker.CronSchedule != "*/5 * * * *" {
		t.Errorf("expected cron schedule */5 * * * *, got %s", cfg.Worker.CronSchedule)
	}
}

func TestLoad_MissingRequiredVar(t *testing.T) {
	// Clear all env vars to ensure failure
	os.Unsetenv("POSTGRES_HOST")
	os.Unsetenv("GITLAB_TOKEN")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for missing required env vars, got nil")
	}
}

func TestPostgresConfig_DSN(t *testing.T) {
	pg := config.PostgresConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "user",
		Password: "pass",
		DB:       "mydb",
	}

	expected := "postgres://user:pass@localhost:5432/mydb?sslmode=disable"
	if pg.DSN() != expected {
		t.Errorf("expected DSN %s, got %s", expected, pg.DSN())
	}
}
```

**Step 3: Run test to verify it fails**

Run:
```bash
go test ./internal/config/... -v
```

Expected: FAIL - package `config` not found or types undefined.

**Step 4: Write the implementation**

Create file `internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Postgres PostgresConfig
	Gitlab   GitlabConfig
	Worker   WorkerConfig
}

type PostgresConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DB       string
}

func (p PostgresConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		p.User, p.Password, p.Host, p.Port, p.DB)
}

type GitlabConfig struct {
	BaseURL    string
	Token      string
	ProjectIDs []int
}

type WorkerConfig struct {
	CronSchedule string
}

func Load() (*Config, error) {
	// Attempt to load .env file; ignore error if file doesn't exist
	_ = godotenv.Load()

	pgPort, err := requireEnvInt("POSTGRES_PORT")
	if err != nil {
		return nil, err
	}

	projectIDs, err := parseIntList(requireEnv("GITLAB_PROJECT_IDS"))
	if err != nil {
		return nil, fmt.Errorf("invalid GITLAB_PROJECT_IDS: %w", err)
	}

	pgHost := requireEnv("POSTGRES_HOST")
	if pgHost == "" {
		return nil, fmt.Errorf("POSTGRES_HOST is required")
	}

	pgUser := requireEnv("POSTGRES_USER")
	if pgUser == "" {
		return nil, fmt.Errorf("POSTGRES_USER is required")
	}

	pgPassword := requireEnv("POSTGRES_PASSWORD")
	if pgPassword == "" {
		return nil, fmt.Errorf("POSTGRES_PASSWORD is required")
	}

	pgDB := requireEnv("POSTGRES_DB")
	if pgDB == "" {
		return nil, fmt.Errorf("POSTGRES_DB is required")
	}

	gitlabBaseURL := requireEnv("GITLAB_BASE_URL")
	if gitlabBaseURL == "" {
		return nil, fmt.Errorf("GITLAB_BASE_URL is required")
	}

	gitlabToken := requireEnv("GITLAB_TOKEN")
	if gitlabToken == "" {
		return nil, fmt.Errorf("GITLAB_TOKEN is required")
	}

	cronSchedule := os.Getenv("WORKER_CRON_SCHEDULE")
	if cronSchedule == "" {
		cronSchedule = "*/15 * * * *" // default: every 15 minutes
	}

	return &Config{
		Postgres: PostgresConfig{
			Host:     pgHost,
			Port:     pgPort,
			User:     pgUser,
			Password: pgPassword,
			DB:       pgDB,
		},
		Gitlab: GitlabConfig{
			BaseURL:    gitlabBaseURL,
			Token:      gitlabToken,
			ProjectIDs: projectIDs,
		},
		Worker: WorkerConfig{
			CronSchedule: cronSchedule,
		},
	}, nil
}

func requireEnv(key string) string {
	return os.Getenv(key)
}

func requireEnvInt(key string) (int, error) {
	val := os.Getenv(key)
	if val == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return n, nil
}

func parseIntList(s string) ([]int, error) {
	if s == "" {
		return nil, fmt.Errorf("empty list")
	}
	parts := strings.Split(s, ",")
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q: %w", p, err)
		}
		result = append(result, n)
	}
	return result, nil
}
```

**Step 5: Run tests to verify they pass**

Run:
```bash
go mod tidy
go test ./internal/config/... -v
```

Expected: All 3 tests PASS.

**Step 6: Commit**

```bash
git add .
git commit -m "feat: add configuration loading module with env var support"
```

---

## Task 4: Database Migrations - Initial Schema

**Files:**
- Create: `db/migrations/000001_create_sync_state_table.up.sql`
- Create: `db/migrations/000001_create_sync_state_table.down.sql`
- Create: `db/migrations/000002_create_issue_events_table.up.sql`
- Create: `db/migrations/000002_create_issue_events_table.down.sql`

**Step 1: Create first migration - sync_state table**

This table tracks the last successful sync timestamp per project, enabling incremental extraction.

Create file `db/migrations/000001_create_sync_state_table.up.sql`:
```sql
CREATE TABLE IF NOT EXISTS sync_state (
    id              SERIAL PRIMARY KEY,
    project_id      INTEGER NOT NULL UNIQUE,
    last_synced_at  TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sync_state_project_id ON sync_state(project_id);

COMMENT ON TABLE sync_state IS 'Tracks the last successful sync timestamp per GitLab project for incremental extraction.';
```

Create file `db/migrations/000001_create_sync_state_table.down.sql`:
```sql
DROP TABLE IF EXISTS sync_state;
```

**Step 2: Create second migration - issue_events table**

This is the core ELT table: raw, immutable event records from GitLab.

Create file `db/migrations/000002_create_issue_events_table.up.sql`:
```sql
CREATE TABLE IF NOT EXISTS issue_events (
    id                  BIGSERIAL PRIMARY KEY,
    gitlab_event_id     BIGINT NOT NULL,
    project_id          INTEGER NOT NULL,
    issue_iid           INTEGER NOT NULL,
    action              VARCHAR(50) NOT NULL,
    author_username     VARCHAR(255),
    label_name          VARCHAR(255),
    created_at_gitlab   TIMESTAMPTZ NOT NULL,
    raw_payload         JSONB NOT NULL,
    ingested_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_gitlab_event UNIQUE (gitlab_event_id, project_id)
);

CREATE INDEX idx_issue_events_project_id ON issue_events(project_id);
CREATE INDEX idx_issue_events_issue_iid ON issue_events(project_id, issue_iid);
CREATE INDEX idx_issue_events_created_at ON issue_events(created_at_gitlab);
CREATE INDEX idx_issue_events_action ON issue_events(action);

COMMENT ON TABLE issue_events IS 'Immutable log of all GitLab issue resource events. Raw ELT data - never updated, only appended.';
COMMENT ON COLUMN issue_events.raw_payload IS 'Full JSON response from GitLab API for this event, preserved for future reprocessing.';
```

Create file `db/migrations/000002_create_issue_events_table.down.sql`:
```sql
DROP TABLE IF EXISTS issue_events;
```

**Step 3: Run migrations against local database**

Run:
```bash
export DATABASE_URL="postgres://gitlab_elt:gitlab_elt_dev@localhost:5432/gitlab_elt?sslmode=disable"
migrate -path db/migrations -database "$DATABASE_URL" up
```

Expected output:
```
1/u create_sync_state_table (Xms)
2/u create_issue_events_table (Xms)
```

**Step 4: Verify tables exist**

Run:
```bash
docker compose exec postgres psql -U gitlab_elt -d gitlab_elt -c "\dt"
```

Expected: Tables `sync_state`, `issue_events`, and `schema_migrations` listed.

**Step 5: Test rollback works**

Run:
```bash
migrate -path db/migrations -database "$DATABASE_URL" down -all
migrate -path db/migrations -database "$DATABASE_URL" up
```

Expected: Down runs without error, up recreates both tables.

**Step 6: Commit**

```bash
git add .
git commit -m "feat: add database migrations for sync_state and issue_events tables"
```

---

## Task 5: sqlc Configuration and Query Generation

**Files:**
- Create: `sqlc.yaml`
- Create: `db/query/sync_state.sql`
- Create: `db/query/issue_events.sql`
- Generated: `internal/repository/*.go` (auto-generated by sqlc)

**Step 1: Create sqlc.yaml**

Create file `sqlc.yaml`:
```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "db/query/"
    schema: "db/migrations/"
    gen:
      go:
        package: "repository"
        out: "internal/repository"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_interface: true
        emit_empty_slices: true
```

**Step 2: Install pgx dependency**

Run:
```bash
go get github.com/jackc/pgx/v5
```

**Step 3: Write sync_state queries**

Create file `db/query/sync_state.sql`:
```sql
-- name: GetSyncState :one
SELECT * FROM sync_state
WHERE project_id = $1
LIMIT 1;

-- name: UpsertSyncState :one
INSERT INTO sync_state (project_id, last_synced_at, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (project_id)
DO UPDATE SET
    last_synced_at = EXCLUDED.last_synced_at,
    updated_at = NOW()
RETURNING *;
```

**Step 4: Write issue_events queries**

Create file `db/query/issue_events.sql`:
```sql
-- name: InsertIssueEvent :one
INSERT INTO issue_events (
    gitlab_event_id,
    project_id,
    issue_iid,
    action,
    author_username,
    label_name,
    created_at_gitlab,
    raw_payload
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (gitlab_event_id, project_id) DO NOTHING
RETURNING *;

-- name: BulkInsertIssueEvent :exec
INSERT INTO issue_events (
    gitlab_event_id,
    project_id,
    issue_iid,
    action,
    author_username,
    label_name,
    created_at_gitlab,
    raw_payload
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (gitlab_event_id, project_id) DO NOTHING;

-- name: CountEventsByProject :one
SELECT COUNT(*) FROM issue_events
WHERE project_id = $1;
```

**Step 5: Run sqlc generate**

Run:
```bash
sqlc generate
```

Expected: No errors. Files generated in `internal/repository/`:
- `db.go`
- `models.go`
- `sync_state.sql.go`
- `issue_events.sql.go`
- `querier.go`

**Step 6: Verify generated code compiles**

Run:
```bash
go mod tidy
go build ./...
```

Expected: Clean build, no errors.

**Step 7: Commit**

```bash
git add .
git commit -m "feat: add sqlc config and SQL queries for sync_state and issue_events"
```

---

## Task 6: Domain Types and Interfaces

**Files:**
- Create: `internal/domain/models.go`
- Create: `internal/domain/interfaces.go`

**Step 1: Create domain models**

Create file `internal/domain/models.go`:
```go
package domain

import "time"

// GitlabResourceEvent represents a single resource event from the GitLab API.
// See: https://docs.gitlab.com/ee/api/resource_events.html
type GitlabResourceEvent struct {
	ID        int64     `json:"id"`
	User      User      `json:"user"`
	Action    string    `json:"action"`
	Label     *Label    `json:"label,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

type Label struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GitlabIssue represents the minimal issue data needed for event extraction.
type GitlabIssue struct {
	IID       int       `json:"iid"`
	ProjectID int       `json:"project_id"`
	UpdatedAt time.Time `json:"updated_at"`
}
```

**Step 2: Create interfaces**

Create file `internal/domain/interfaces.go`:
```go
package domain

import (
	"context"
	"time"
)

// GitlabClient defines the contract for interacting with the GitLab API.
type GitlabClient interface {
	// ListIssues fetches issues updated after the given timestamp for a project.
	ListIssues(ctx context.Context, projectID int, updatedAfter time.Time) ([]GitlabIssue, error)

	// ListLabelEvents fetches label events for a specific issue.
	ListLabelEvents(ctx context.Context, projectID int, issueIID int) ([]GitlabResourceEvent, error)
}

// EventRepository defines the contract for persisting issue events.
type EventRepository interface {
	// SaveEvent persists a single issue event (idempotent via upsert).
	SaveEvent(ctx context.Context, event IssueEventRecord) error

	// GetLastSyncedAt returns the last sync timestamp for a project.
	GetLastSyncedAt(ctx context.Context, projectID int) (time.Time, error)

	// UpdateLastSyncedAt updates the sync cursor for a project.
	UpdateLastSyncedAt(ctx context.Context, projectID int, syncedAt time.Time) error
}

// IssueEventRecord is the flattened record ready for database insertion.
type IssueEventRecord struct {
	GitlabEventID   int64
	ProjectID       int
	IssueIID        int
	Action          string
	AuthorUsername  string
	LabelName      *string
	CreatedAtGitlab time.Time
	RawPayload     []byte
}
```

**Step 3: Verify it compiles**

Run:
```bash
go build ./internal/domain/...
```

Expected: Clean build.

**Step 4: Commit**

```bash
git add .
git commit -m "feat: add domain models and interfaces for GitLab ELT"
```

---

## Task 7: Makefile for Developer Experience

**Files:**
- Create: `Makefile`

**Step 1: Create Makefile**

Create file `Makefile`:
```makefile
.PHONY: help build run test lint clean docker-up docker-down migrate-up migrate-down migrate-create sqlc-generate

# Load .env if it exists
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the worker binary
	go build -o bin/worker cmd/worker/main.go

run: ## Run the worker locally
	go run cmd/worker/main.go

test: ## Run all tests
	go test ./... -v -race

clean: ## Remove build artifacts
	rm -rf bin/

docker-up: ## Start local infrastructure (PostgreSQL)
	docker compose up -d

docker-down: ## Stop local infrastructure
	docker compose down

migrate-up: ## Run all database migrations
	migrate -path db/migrations -database "$(DATABASE_URL)" -verbose up

migrate-down: ## Rollback all database migrations
	migrate -path db/migrations -database "$(DATABASE_URL)" -verbose down -all

migrate-create: ## Create a new migration (usage: make migrate-create name=create_foo_table)
	migrate create -ext sql -dir db/migrations -seq $(name)

sqlc-generate: ## Generate Go code from SQL queries
	sqlc generate

setup: docker-up migrate-up sqlc-generate build ## Full local setup: start DB, run migrations, generate code, build
```

**Step 2: Test the Makefile**

Run:
```bash
make help
```

Expected: Formatted list of all available make targets.

**Step 3: Commit**

```bash
git add Makefile
git commit -m "chore: add Makefile with dev workflow commands"
```

---

## Task 8: Worker Entrypoint with Cron and Graceful Shutdown

**Files:**
- Modify: `cmd/worker/main.go`

**Step 1: Install cron dependency**

Run:
```bash
go get github.com/robfig/cron/v3
```

**Step 2: Write the main.go with cron + graceful shutdown**

Replace `cmd/worker/main.go` with:
```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"

	"github.com/pdrhp/gitlab-elt/internal/config"
	"github.com/pdrhp/gitlab-elt/internal/repository"
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

	// Initialize repository (sqlc-generated queries)
	queries := repository.New(pool)
	_ = queries // will be used by the service layer in future tasks

	// Setup cron scheduler
	c := cron.New(cron.WithLogger(cron.VerbosePrintfLogger(
		newCronLogger(logger),
	)))

	_, err = c.AddFunc(cfg.Worker.CronSchedule, func() {
		slog.Info("sync job triggered", "schedule", cfg.Worker.CronSchedule)
		// TODO: invoke service.Sync() here
		slog.Info("sync job placeholder completed")
	})
	if err != nil {
		slog.Error("failed to register cron job", "error", err)
		os.Exit(1)
	}

	c.Start()
	slog.Info("cron scheduler started", "schedule", cfg.Worker.CronSchedule)

	// Graceful shutdown: wait for SIGINT or SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	slog.Info("received shutdown signal", "signal", sig.String())

	// Stop cron and wait for running jobs to finish
	stopCtx := c.Stop()
	select {
	case <-stopCtx.Done():
		slog.Info("all cron jobs completed")
	case <-time.After(30 * time.Second):
		slog.Warn("shutdown timeout exceeded, forcing exit")
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
```

**Step 3: Tidy modules**

Run:
```bash
go mod tidy
```

**Step 4: Verify it compiles**

Run:
```bash
go build -o bin/worker cmd/worker/main.go
```

Expected: Binary created at `bin/worker` with no errors.

**Step 5: Integration smoke test**

Make sure PostgreSQL is running, `.env` is configured, and migrations have been applied. Then:

Run:
```bash
./bin/worker &
sleep 3
kill %1
```

Expected: Logs show "starting gitlab-elt worker", "connected to database", "cron scheduler started", then on kill: "received shutdown signal", "gitlab-elt worker stopped".

**Step 6: Commit**

```bash
git add .
git commit -m "feat: add worker entrypoint with cron scheduler and graceful shutdown"
```

---

## Task 9: Final Verification and Cleanup

**Step 1: Run full test suite**

Run:
```bash
make test
```

Expected: All tests pass.

**Step 2: Run full local setup from scratch**

Run:
```bash
make docker-down
docker volume rm $(docker volume ls -q --filter name=gitlab-elt) 2>/dev/null || true
make setup
```

Expected: PostgreSQL starts, migrations apply, sqlc generates, binary builds.

**Step 3: Verify the binary runs end-to-end**

Run:
```bash
make run
```

Expected: Worker starts, connects to DB, starts cron, waits for signals.

**Step 4: Final commit**

```bash
git add .
git commit -m "chore: project startup complete - ready for GitLab client implementation"
```

---

## Summary of What's Built

After completing all tasks, the project has:

| Component | Status |
|---|---|
| Go module initialized | Done |
| Directory structure (Standard Layout) | Done |
| Docker Compose for PostgreSQL | Done |
| Environment config (.env + godotenv) | Done |
| Config loading with validation + tests | Done |
| Database migrations (sync_state + issue_events) | Done |
| sqlc config + generated repository code | Done |
| Domain models and interfaces | Done |
| Makefile with full dev workflow | Done |
| Worker entrypoint with cron + graceful shutdown | Done |
| Structured logging (slog/JSON) | Done |

## What Comes Next (Not in This Plan)

1. **GitLab HTTP Client** (`internal/gitlab/`) - Implement the `GitlabClient` interface with pagination, retry, and rate limiting
2. **Service Layer** (`internal/service/`) - Implement the sync orchestrator that ties GitLab client to repository
3. **Integration Tests** - Test the full flow with a test database
4. **Dockerfile** - Multi-stage build for production deployment

# Phase 1: Foundation Completion - Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete all remaining Phase 1 Foundation tasks so the project has full Bronze/Silver/Config sqlc queries, a working healthcheck HTTP endpoint, and a validated Docker + migration setup.

**Architecture:** The project already has Go module structure, domain interfaces, migrations (10 up/down), config loading, main.go with cron + graceful shutdown, and sqlc generating code for 2 tables (sync_state, issue_events). We need to: (1) add sqlc queries for ALL remaining tables (raw_projects, raw_events, Silver projects/issues/issue_events/issue_comments, Config state_mapping/metadata_mapping/unknown_labels_log), (2) build an HTTP healthcheck endpoint, (3) validate the full Docker + migrations flow works end-to-end.

**Tech Stack:** Go 1.25, pgx/v5, sqlc, PostgreSQL 16, robfig/cron/v3, slog, Docker Compose

**Important note on sqlc schema conflict:** The current `sqlc.yaml` points `schema` at `db/migrations/` which contains ALL 10 migrations. Migration `000006_create_issue_events_table.up.sql` creates a Silver `issue_events` table with different columns (issue_id FK, mapped_canonical_state, is_noise, cycle_count) than what the current `db/query/issue_events.sql` expects (the current queries target a Bronze-style issue_events with raw_payload, action, etc.). This conflict must be resolved before adding new queries. The approach: rename the Bronze queries file to `raw_issue_events.sql` targeting the `raw_events` table, and create new Silver queries for the proper `issue_events` table.

---

## Pre-Requisites

Before starting, make sure you have:
- Go 1.25+ installed (`go version`)
- Docker and Docker Compose installed (`docker compose version`)
- `sqlc` CLI installed (`sqlc version` - need v1.30+). Install: `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`
- `migrate` CLI installed (`migrate -version`). Install: `go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`
- The project cloned at your working directory
- `.env` file created from `.env.example` with real or local dev values

---

## Task 1: Validate Docker Compose + Migrations Flow

**Goal:** Ensure the full local dev environment boots cleanly.

**Files:**
- Read: `docker-compose.yml`
- Read: `Makefile`
- Read: `db/migrations/*.up.sql`

**Step 1: Start PostgreSQL**

```bash
make docker-up
```

Expected: Container `gitlab-elt-postgres` starts. Verify:

```bash
docker compose ps
```

Expected output includes `gitlab-elt-postgres` with status `healthy`.

**Step 2: Apply all migrations**

```bash
make migrate-up
```

Expected: All 10 migrations apply without errors. Output should show:
```
000001_create_raw_projects_table.up.sql
000002_create_raw_events_table.up.sql
000003_create_sync_state_table.up.sql
...
000010_create_unknown_labels_log_table.up.sql
```

**Step 3: Verify tables exist**

```bash
docker compose exec postgres psql -U gitlab_elt -c "\dt"
```

Expected: 10 tables listed: `raw_projects`, `raw_events`, `sync_state`, `projects`, `issues`, `issue_events`, `issue_comments`, `state_mapping`, `metadata_mapping`, `unknown_labels_log`, plus `schema_migrations`.

**Step 4: Apply seed data**

```bash
docker compose exec -T postgres psql -U gitlab_elt < db/seeds/state_mappings.sql
```

Expected: No errors. Verify:

```bash
docker compose exec postgres psql -U gitlab_elt -c "SELECT COUNT(*) FROM state_mapping;"
docker compose exec postgres psql -U gitlab_elt -c "SELECT COUNT(*) FROM metadata_mapping;"
```

Expected: ~40 state mappings, ~20 metadata mappings.

**Step 5: Verify build compiles**

```bash
make build
```

Expected: Binary created at `bin/worker` without errors.

**Step 6: Run tests**

```bash
make test
```

Expected: 3 tests pass (config tests).

**Step 7: Commit (if anything was fixed)**

Only commit if you had to fix something. If everything passes cleanly, no commit needed.

---

## Task 2: Resolve sqlc Schema Conflict and Restructure Queries

**Goal:** The current `db/query/issue_events.sql` writes queries against a simplified Bronze-style `issue_events` table (with `raw_payload`, `action`, `author_username`, `label_name`). But migration `000006` defines the Silver `issue_events` table with different columns (`issue_id`, `mapped_canonical_state`, `is_noise`, `cycle_count`, `raw_label_added`, `raw_label_removed`). sqlc will fail when it sees conflicting schemas. We need to restructure the queries to match the actual migration schemas.

**Analysis:** Looking at the migrations:
- `000002` creates `raw_events` (Bronze) with `raw_payload JSONB`, `event_type`, `processed`
- `000006` creates `issue_events` (Silver) with `issue_id FK`, `mapped_canonical_state`, `is_noise`, `cycle_count`

The current `issue_events.sql` queries are actually Bronze-level queries that should target `raw_events`, not Silver `issue_events`. The current sqlc-generated code works because sqlc only sees the table name `issue_events` once in the old-style query file, but migration 000006 redefines `issue_events` as a Silver table with FKs. This means `sqlc generate` is likely using migration 000006's schema, and the queries are mismatched.

**Resolution:** Delete the old `issue_events.sql` query file (it targets a non-existent Bronze schema for issue_events). Create new query files that match the actual migration schemas:
- `raw_events.sql` - queries for `raw_events` table (Bronze)
- `raw_projects.sql` - queries for `raw_projects` table (Bronze)
- Keep `sync_state.sql` as-is (it's correct)
- `projects.sql` - queries for Silver `projects` table
- `issues.sql` - queries for Silver `issues` table
- `issue_events.sql` - queries for Silver `issue_events` table (matching migration 000006)
- `issue_comments.sql` - queries for Silver `issue_comments` table
- `state_mapping.sql` - queries for Config `state_mapping` table
- `metadata_mapping.sql` - queries for Config `metadata_mapping` table
- `unknown_labels_log.sql` - queries for Config `unknown_labels_log` table

**Files:**
- Delete: `db/query/issue_events.sql` (old, mismatched)
- Create: `db/query/raw_events.sql`
- Create: `db/query/raw_projects.sql`
- Keep: `db/query/sync_state.sql` (already correct)
- Create: `db/query/projects.sql`
- Create: `db/query/issues.sql`
- Create: `db/query/issue_events.sql` (new, matching Silver schema)
- Create: `db/query/issue_comments.sql`
- Create: `db/query/state_mapping.sql`
- Create: `db/query/metadata_mapping.sql`
- Create: `db/query/unknown_labels_log.sql`
- Regenerate: `internal/repository/*` (sqlc output)

### Step 1: Delete old mismatched query file

```bash
rm db/query/issue_events.sql
```

### Step 2: Create `db/query/raw_projects.sql`

Write to `db/query/raw_projects.sql`:

```sql
-- name: UpsertRawProject :one
-- Insere ou atualiza um projeto na camada Bronze.
-- Usado pelo Discovery Service ao descobrir projetos novos.
INSERT INTO raw_projects (id, name, path, raw_metadata, last_synced_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    path = EXCLUDED.path,
    raw_metadata = EXCLUDED.raw_metadata,
    updated_at = NOW()
RETURNING *;

-- name: GetRawProject :one
SELECT * FROM raw_projects WHERE id = $1;

-- name: ListRawProjects :many
-- Lista todos os projetos Bronze, ordenados por nome.
SELECT * FROM raw_projects ORDER BY name;

-- name: ListRawProjectsDueSync :many
-- Lista projetos que precisam de sync (last_synced_at antes do threshold).
SELECT * FROM raw_projects
WHERE last_synced_at < $1
ORDER BY last_synced_at ASC;

-- name: UpdateRawProjectLastSynced :exec
-- Atualiza o cursor de sincronizacao apos extração Bronze bem-sucedida.
UPDATE raw_projects
SET last_synced_at = $1, updated_at = NOW()
WHERE id = $2;
```

### Step 3: Create `db/query/raw_events.sql`

Write to `db/query/raw_events.sql`:

```sql
-- name: InsertRawEvent :one
-- Insere um evento bruto na camada Bronze. Append-only, imutavel.
INSERT INTO raw_events (gitlab_event_id, project_id, issue_iid, event_type, raw_payload)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: BulkInsertRawEvent :exec
-- Insere evento bruto (usado em batch). ON CONFLICT ignora duplicatas.
INSERT INTO raw_events (gitlab_event_id, project_id, issue_iid, event_type, raw_payload)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT DO NOTHING;

-- name: ListUnprocessedRawEvents :many
-- Lista eventos Bronze ainda nao transformados para Silver.
-- Usado pelo Transformer para processar em batch.
SELECT * FROM raw_events
WHERE processed = FALSE
ORDER BY fetched_at ASC
LIMIT $1;

-- name: ListUnprocessedRawEventsByProject :many
-- Lista eventos nao processados de um projeto especifico.
SELECT * FROM raw_events
WHERE processed = FALSE AND project_id = $1
ORDER BY fetched_at ASC
LIMIT $2;

-- name: MarkRawEventProcessed :exec
-- Marca evento como processado apos transformacao Silver.
UPDATE raw_events SET processed = TRUE WHERE id = $1;

-- name: MarkRawEventsProcessedBatch :exec
-- Marca multiplos eventos como processados.
UPDATE raw_events SET processed = TRUE WHERE id = ANY($1::bigint[]);

-- name: CountRawEventsByProject :one
SELECT COUNT(*) FROM raw_events WHERE project_id = $1;

-- name: CountUnprocessedRawEvents :one
SELECT COUNT(*) FROM raw_events WHERE processed = FALSE;
```

### Step 4: Create `db/query/projects.sql`

Write to `db/query/projects.sql`:

```sql
-- name: UpsertProject :one
-- Insere ou atualiza um projeto na camada Silver.
-- Usado pelo Transformer ao normalizar raw_projects -> projects.
INSERT INTO projects (id, name, path, last_synced_at, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    path = EXCLUDED.path,
    last_synced_at = EXCLUDED.last_synced_at,
    updated_at = NOW()
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1;

-- name: ListProjects :many
SELECT * FROM projects ORDER BY name;
```

### Step 5: Create `db/query/issues.sql`

Write to `db/query/issues.sql`:

```sql
-- name: UpsertIssue :one
-- Insere ou atualiza uma issue na camada Silver.
INSERT INTO issues (
    gitlab_issue_id, project_id, iid, title,
    current_canonical_state, metadata_labels, assignees,
    gitlab_created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
ON CONFLICT (gitlab_issue_id) DO UPDATE SET
    title = EXCLUDED.title,
    current_canonical_state = EXCLUDED.current_canonical_state,
    metadata_labels = EXCLUDED.metadata_labels,
    assignees = EXCLUDED.assignees,
    updated_at = NOW()
RETURNING *;

-- name: GetIssueByGitlabID :one
SELECT * FROM issues WHERE gitlab_issue_id = $1;

-- name: GetIssueByProjectAndIID :one
SELECT * FROM issues WHERE project_id = $1 AND iid = $2;

-- name: ListIssuesByProject :many
SELECT * FROM issues WHERE project_id = $1 ORDER BY iid;

-- name: UpdateIssueCanonicalState :exec
-- Atualiza o cache de estado canonico da issue.
UPDATE issues SET current_canonical_state = $1, updated_at = NOW()
WHERE id = $2;
```

### Step 6: Create `db/query/issue_events.sql` (new Silver version)

Write to `db/query/issue_events.sql`:

```sql
-- name: InsertIssueEvent :one
-- Insere um evento normalizado na camada Silver.
INSERT INTO issue_events (
    gitlab_event_id, issue_id, project_id, issue_iid,
    author_name, raw_label_added, raw_label_removed,
    mapped_canonical_state, event_timestamp, is_noise, cycle_count
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (gitlab_event_id, project_id) DO NOTHING
RETURNING *;

-- name: ListIssueEventsByIssue :many
-- Lista eventos de uma issue especifica, ordenados por timestamp.
SELECT * FROM issue_events
WHERE issue_id = $1
ORDER BY event_timestamp ASC;

-- name: ListIssueEventsByProject :many
-- Lista eventos de um projeto, ordenados por timestamp.
SELECT * FROM issue_events
WHERE project_id = $1
ORDER BY event_timestamp ASC;

-- name: CountIssueEventsByProject :one
SELECT COUNT(*) FROM issue_events WHERE project_id = $1;

-- name: GetLatestIssueEvent :one
-- Retorna o evento mais recente de uma issue (para calcular estado atual).
SELECT * FROM issue_events
WHERE issue_id = $1 AND is_noise = FALSE
ORDER BY event_timestamp DESC
LIMIT 1;
```

### Step 7: Create `db/query/issue_comments.sql`

Write to `db/query/issue_comments.sql`:

```sql
-- name: InsertIssueComment :one
-- Insere um comentario normalizado na camada Silver.
INSERT INTO issue_comments (
    gitlab_note_id, issue_id, author_name, body, comment_timestamp
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (gitlab_note_id) DO NOTHING
RETURNING *;

-- name: ListIssueCommentsByIssue :many
SELECT * FROM issue_comments
WHERE issue_id = $1
ORDER BY comment_timestamp ASC;
```

### Step 8: Create `db/query/state_mapping.sql`

Write to `db/query/state_mapping.sql`:

```sql
-- name: ListStateMappings :many
-- Lista todos os mapeamentos de estado. Usado pelo StateMapper no startup.
SELECT * FROM state_mapping ORDER BY canonical_state, gitlab_label_name;

-- name: GetStateMappingByLabel :one
SELECT * FROM state_mapping WHERE gitlab_label_name = $1;

-- name: UpsertStateMapping :one
INSERT INTO state_mapping (gitlab_label_name, canonical_state, description, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (gitlab_label_name) DO UPDATE SET
    canonical_state = EXCLUDED.canonical_state,
    description = EXCLUDED.description,
    updated_at = NOW()
RETURNING *;
```

### Step 9: Create `db/query/metadata_mapping.sql`

Write to `db/query/metadata_mapping.sql`:

```sql
-- name: ListMetadataMappings :many
-- Lista todos os mapeamentos de metadados. Usado pelo StateMapper no startup.
SELECT * FROM metadata_mapping ORDER BY metadata_key, gitlab_label_name;

-- name: GetMetadataMappingByLabel :one
SELECT * FROM metadata_mapping WHERE gitlab_label_name = $1;

-- name: UpsertMetadataMapping :one
INSERT INTO metadata_mapping (gitlab_label_name, metadata_key)
VALUES ($1, $2)
ON CONFLICT (gitlab_label_name) DO UPDATE SET
    metadata_key = EXCLUDED.metadata_key
RETURNING *;
```

### Step 10: Create `db/query/unknown_labels_log.sql`

Write to `db/query/unknown_labels_log.sql`:

```sql
-- name: UpsertUnknownLabel :one
-- Registra ou incrementa contagem de uma label desconhecida.
INSERT INTO unknown_labels_log (label_name, occurrence_count, first_seen_at, last_seen_at)
VALUES ($1, 1, NOW(), NOW())
ON CONFLICT (label_name) DO UPDATE SET
    occurrence_count = unknown_labels_log.occurrence_count + 1,
    last_seen_at = NOW()
RETURNING *;

-- name: ListUnknownLabels :many
-- Lista labels desconhecidas ordenadas por frequencia (mais comuns primeiro).
SELECT * FROM unknown_labels_log
ORDER BY occurrence_count DESC;

-- name: CountUnknownLabels :one
SELECT COUNT(*) FROM unknown_labels_log;
```

### Step 11: Run sqlc generate

```bash
sqlc generate
```

Expected: Code generated without errors in `internal/repository/`. New files should appear for each query file.

If sqlc fails, check the error message. Common issues:
- Column name mismatch between query and migration schema
- Missing table referenced in a query
- Type mismatch

### Step 12: Verify the build compiles

```bash
go build ./...
```

Expected: No compilation errors.

### Step 13: Commit

```bash
git add db/query/ internal/repository/
git commit -m "feat: add sqlc queries for all Bronze/Silver/Config tables

- Remove old mismatched issue_events queries (Bronze-style on Silver table)
- Add raw_projects.sql: CRUD queries for Bronze project cache
- Add raw_events.sql: insert, batch, list unprocessed, mark processed
- Add projects.sql: upsert/list for Silver projects
- Add issues.sql: upsert/get/list for Silver issues
- Add issue_events.sql: Silver events with mapped_canonical_state
- Add issue_comments.sql: Silver comments insert/list
- Add state_mapping.sql: list/get/upsert for config mappings
- Add metadata_mapping.sql: list/get/upsert for metadata labels
- Add unknown_labels_log.sql: upsert (increment count) and list"
```

---

## Task 3: Update Domain Interfaces to Match New Repository

**Goal:** The `internal/domain/interfaces.go` currently defines `EventRepository` and `IssueEventRecord` that match the old Bronze-style queries. Update the domain to properly represent the new Bronze/Silver architecture with separate interfaces.

**Files:**
- Modify: `internal/domain/interfaces.go`
- Modify: `internal/domain/models.go`

### Step 1: Rewrite `internal/domain/models.go`

Replace the full content of `internal/domain/models.go` with:

```go
package domain

import "time"

// =============================================================================
// GitLab API Models (raw response types)
// =============================================================================

// GitlabProject represents a project from the GitLab API.
type GitlabProject struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	PathWithNamespace string `json:"path_with_namespace"`
}

// GitlabIssue represents an issue from the GitLab API.
type GitlabIssue struct {
	ID        int       `json:"id"`   // Global ID
	IID       int       `json:"iid"`  // Project-scoped number (#215)
	ProjectID int       `json:"project_id"`
	Title     string    `json:"title"`
	State     string    `json:"state"` // opened, closed
	Labels    []string  `json:"labels"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GitlabLabelEvent represents a resource_label_event from the GitLab API.
type GitlabLabelEvent struct {
	ID           int64     `json:"id"`
	User         User      `json:"user"`
	Action       string    `json:"action"` // add, remove
	Label        Label     `json:"label"`
	CreatedAt    time.Time `json:"created_at"`
	ResourceType string    `json:"resource_type"`
}

// GitlabNote represents a note (comment) from the GitLab API.
type GitlabNote struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	Author    User      `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	System    bool      `json:"system"`
}

// User represents a GitLab user.
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// Label represents a GitLab label.
type Label struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
```

### Step 2: Rewrite `internal/domain/interfaces.go`

Replace the full content of `internal/domain/interfaces.go` with:

```go
package domain

import (
	"context"
	"time"
)

// GitlabClient defines the contract for interacting with the GitLab API.
type GitlabClient interface {
	// ListGroupProjects lists all projects in a GitLab group.
	ListGroupProjects(ctx context.Context, groupID int) ([]GitlabProject, error)

	// ListIssues fetches issues updated after the given timestamp for a project.
	ListIssues(ctx context.Context, projectID int, updatedAfter time.Time) ([]GitlabIssue, error)

	// ListLabelEvents fetches label events for a specific issue.
	ListLabelEvents(ctx context.Context, projectID int, issueIID int) ([]GitlabLabelEvent, error)

	// ListNotes fetches comments for a specific issue.
	ListNotes(ctx context.Context, projectID int, issueIID int) ([]GitlabNote, error)
}
```

### Step 3: Verify compilation

```bash
go build ./...
```

Expected: Passes. The `main.go` does not reference `domain.EventRepository` or `domain.IssueEventRecord` directly, so removing them should not break anything.

If compilation fails, check what references the old types and update accordingly.

### Step 4: Run tests

```bash
make test
```

Expected: All 3 config tests still pass. Domain has no tests (pure data types).

### Step 5: Commit

```bash
git add internal/domain/
git commit -m "refactor: update domain models for Bronze/Silver architecture

- Add GitlabProject, GitlabNote models for discovery and comments
- Update GitlabClient interface with ListGroupProjects and ListNotes
- Remove old EventRepository interface (replaced by sqlc-generated code)
- Remove IssueEventRecord (replaced by sqlc param structs)"
```

---

## Task 4: Implement Healthcheck HTTP Endpoint

**Goal:** Create a `/health` endpoint that returns 200 when the worker is healthy (can reach PostgreSQL) and 503 when unhealthy. This is critical for container orchestration (Docker healthcheck, Kubernetes probes).

**Files:**
- Create: `internal/health/handler.go`
- Create: `internal/health/handler_test.go`
- Modify: `cmd/worker/main.go` (wire healthcheck)

### Step 1: Write the failing test

Create `internal/health/handler_test.go`:

```go
package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pdrhp/gitlab-elt/internal/health"
)

// mockPinger implements health.Pinger for testing.
type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(ctx context.Context) error {
	return m.err
}

func TestHealthHandler_Healthy(t *testing.T) {
	h := health.NewHandler(&mockPinger{err: nil})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp health.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected status 'healthy', got %q", resp.Status)
	}

	if resp.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestHealthHandler_Unhealthy(t *testing.T) {
	h := health.NewHandler(&mockPinger{err: errors.New("connection refused")})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}

	var resp health.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "unhealthy" {
		t.Errorf("expected status 'unhealthy', got %q", resp.Status)
	}

	if resp.Error == "" {
		t.Error("expected non-empty error field")
	}
}

func TestHealthHandler_MethodNotAllowed(t *testing.T) {
	h := health.NewHandler(&mockPinger{err: nil})

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}
```

### Step 2: Run the test to verify it fails

```bash
go test ./internal/health/... -v
```

Expected: FAIL - package `health` does not exist yet.

### Step 3: Implement the healthcheck handler

Create `internal/health/handler.go`:

```go
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Pinger is an interface for checking database connectivity.
// *pgxpool.Pool satisfies this interface.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Response is the JSON response from the health endpoint.
type Response struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Error     string `json:"error,omitempty"`
}

// Handler is the HTTP handler for the /health endpoint.
type Handler struct {
	pinger Pinger
}

// NewHandler creates a new health check handler.
func NewHandler(pinger Pinger) *Handler {
	return &Handler{pinger: pinger}
}

// ServeHTTP handles HTTP requests to the health endpoint.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	resp := Response{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	if err := h.pinger.Ping(ctx); err != nil {
		resp.Status = "unhealthy"
		resp.Error = err.Error()
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		resp.Status = "healthy"
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(resp)
}
```

### Step 4: Run the tests to verify they pass

```bash
go test ./internal/health/... -v
```

Expected: 3 tests pass:
```
--- PASS: TestHealthHandler_Healthy
--- PASS: TestHealthHandler_Unhealthy
--- PASS: TestHealthHandler_MethodNotAllowed
```

### Step 5: Commit

```bash
git add internal/health/
git commit -m "feat: add healthcheck HTTP endpoint with DB ping

- GET /health returns 200 + {status: healthy} when DB is reachable
- Returns 503 + {status: unhealthy, error: ...} when DB is down
- Uses Pinger interface (pgxpool.Pool satisfies it)
- 3 unit tests: healthy, unhealthy, method not allowed"
```

---

## Task 5: Wire Healthcheck into main.go

**Goal:** Start the healthcheck HTTP server in main.go alongside the cron scheduler. It should run on a configurable port (default 8080) and shut down gracefully.

**Files:**
- Modify: `internal/config/config.go` (add HealthPort)
- Modify: `internal/config/config_test.go` (test default port)
- Modify: `cmd/worker/main.go` (start HTTP server)

### Step 1: Add HealthPort to config

In `internal/config/config.go`, add `HealthPort` to `WorkerConfig`:

Find:
```go
type WorkerConfig struct {
	CronSchedule string
}
```

Replace with:
```go
type WorkerConfig struct {
	CronSchedule string
	HealthPort   int
}
```

In the `Load()` function, after the `cronSchedule` block, add:

Find:
```go
	cronSchedule := os.Getenv("WORKER_CRON_SCHEDULE")
	if cronSchedule == "" {
		cronSchedule = "*/15 * * * *" // default: every 15 minutes
	}
```

After that block (before the `return &Config{` line), add:

```go
	healthPort := 8080
	if v := os.Getenv("HEALTH_PORT"); v != "" {
		healthPort, err = strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("HEALTH_PORT must be an integer: %w", err)
		}
	}
```

And in the return statement, update `Worker`:

Find:
```go
		Worker: WorkerConfig{
			CronSchedule: cronSchedule,
		},
```

Replace with:
```go
		Worker: WorkerConfig{
			CronSchedule: cronSchedule,
			HealthPort:   healthPort,
		},
```

### Step 2: Add test for default health port

In `internal/config/config_test.go`, add to the `TestLoad_FromEnvVars` function, after the cron schedule assertion:

Find:
```go
	if cfg.Worker.CronSchedule != "*/5 * * * *" {
		t.Errorf("expected cron schedule */5 * * * *, got %s", cfg.Worker.CronSchedule)
	}
}
```

Replace with:
```go
	if cfg.Worker.CronSchedule != "*/5 * * * *" {
		t.Errorf("expected cron schedule */5 * * * *, got %s", cfg.Worker.CronSchedule)
	}
	if cfg.Worker.HealthPort != 8080 {
		t.Errorf("expected default health port 8080, got %d", cfg.Worker.HealthPort)
	}
}
```

### Step 3: Run config tests

```bash
go test ./internal/config/... -v
```

Expected: All tests pass (including the new health port assertion).

### Step 4: Wire healthcheck server into main.go

Replace the full content of `cmd/worker/main.go` with:

```go
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
	"github.com/robfig/cron/v3"

	"github.com/pdrhp/gitlab-elt/internal/config"
	"github.com/pdrhp/gitlab-elt/internal/health"
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

	// Start healthcheck HTTP server
	healthHandler := health.NewHandler(pool)
	mux := http.NewServeMux()
	mux.Handle("/health", healthHandler)

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
```

### Step 5: Verify build

```bash
go build ./...
```

Expected: Compiles without errors.

### Step 6: Run all tests

```bash
make test
```

Expected: All 6 tests pass (3 config + 3 health).

### Step 7: Manual smoke test (optional but recommended)

```bash
make docker-up && sleep 3 && make migrate-up
```

In one terminal:
```bash
make run
```

In another terminal:
```bash
curl -s http://localhost:8080/health | jq .
```

Expected:
```json
{
  "status": "healthy",
  "timestamp": "2026-02-28T..."
}
```

Stop the worker with Ctrl+C. Expected: graceful shutdown logs.

### Step 8: Update .env.example

Add to `.env.example`:

```
# Health
HEALTH_PORT=8080
```

### Step 9: Commit

```bash
git add cmd/worker/main.go internal/config/ .env.example
git commit -m "feat: wire healthcheck server into main.go

- Add HEALTH_PORT config (default 8080)
- Start HTTP server with /health endpoint
- Graceful shutdown includes health server
- Smoke test: curl localhost:8080/health returns healthy"
```

---

## Task 6: Add Makefile Targets for Seeds and Health Check

**Goal:** Improve DX with convenience targets for common operations.

**Files:**
- Modify: `Makefile`

### Step 1: Add new targets to Makefile

Add the following after the existing targets (before the last line or at the end):

```makefile
seed: ## Apply seed data (state and metadata mappings)
	docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) < db/seeds/state_mappings.sql

health: ## Check worker health endpoint
	@curl -sf http://localhost:$(HEALTH_PORT)/health | python3 -m json.tool || echo "Health check failed (is the worker running?)"

reset-db: docker-down docker-up ## Reset database (destroy and recreate)
	@echo "Waiting for PostgreSQL to be ready..."
	@sleep 3
	@$(MAKE) migrate-up
	@$(MAKE) seed
	@echo "Database reset complete."
```

Also add default for HEALTH_PORT near the DATABASE_URL line:

```makefile
HEALTH_PORT ?= 8080
```

### Step 2: Test the new targets

```bash
make help
```

Expected: New targets `seed`, `health`, `reset-db` appear in help output.

### Step 3: Commit

```bash
git add Makefile
git commit -m "feat: add seed, health, and reset-db Makefile targets

- make seed: applies state/metadata mapping seed data
- make health: curls the healthcheck endpoint
- make reset-db: full DB reset (down, up, migrate, seed)"
```

---

## Task 7: Full Integration Smoke Test

**Goal:** Verify the entire Phase 1 stack works end-to-end: Docker up, migrations, seeds, sqlc code compiles, worker starts, healthcheck responds, graceful shutdown works.

**Files:** None (validation only)

### Step 1: Clean slate

```bash
make docker-down
make clean
```

### Step 2: Full setup

```bash
make setup
```

Expected: Docker starts, migrations apply, sqlc generates, binary builds.

### Step 3: Apply seeds

```bash
make seed
```

Expected: No errors.

### Step 4: Start worker

In a terminal:
```bash
make run
```

Expected logs (JSON format):
- `starting gitlab-elt worker`
- `connected to database`
- `healthcheck server started`
- `cron scheduler started`

### Step 5: Test health endpoint

In another terminal:
```bash
make health
```

Expected:
```json
{
    "status": "healthy",
    "timestamp": "..."
}
```

### Step 6: Verify all tests pass

```bash
make test
```

Expected: 6 tests pass (3 config + 3 health), all green.

### Step 7: Graceful shutdown test

Send SIGINT to the running worker (Ctrl+C in the terminal).

Expected logs:
- `received shutdown signal`
- `all cron jobs completed`
- `gitlab-elt worker stopped`

### Step 8: Verify DB data

```bash
docker compose exec postgres psql -U gitlab_elt -c "SELECT canonical_state, COUNT(*) FROM state_mapping GROUP BY canonical_state ORDER BY canonical_state;"
docker compose exec postgres psql -U gitlab_elt -c "SELECT metadata_key, COUNT(*) FROM metadata_mapping GROUP BY metadata_key ORDER BY metadata_key;"
```

Expected: State mappings grouped by canonical state, metadata grouped by key.

### Step 9: Final commit (if any fixes were needed)

```bash
git add -A
git commit -m "fix: address issues found during Phase 1 integration smoke test"
```

Only commit if you had to fix something. If everything passed cleanly, Phase 1 is complete.

---

## Summary

| Task | What | Estimated Time |
|------|------|---------------|
| 1 | Validate Docker + Migrations flow | 15 min |
| 2 | Restructure sqlc queries (all 10 tables) | 45-60 min |
| 3 | Update domain interfaces for Bronze/Silver | 15-20 min |
| 4 | Implement healthcheck endpoint + tests | 20-30 min |
| 5 | Wire healthcheck into main.go + config | 20-30 min |
| 6 | Add Makefile DX targets | 10-15 min |
| 7 | Full integration smoke test | 15-20 min |
| **Total** | | **~2.5-3 hours** |

After all 7 tasks complete, Phase 1 Foundation is **done** and the project is ready for Phase 2 (GitLab Client + Core ELT Pipeline).

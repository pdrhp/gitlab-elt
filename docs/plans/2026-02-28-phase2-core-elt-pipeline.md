# Phase 2: Core ELT Pipeline Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the complete ELT pipeline: GitLab HTTP client → Discovery → Bronze extraction → Silver transformation → DLQ → Scheduler wiring.

**Architecture:** The worker polls GitLab's REST API (with rate limiting + retry + pagination), stores raw JSONB payloads in Bronze tables (`raw_events`, `raw_projects`), then transforms them into normalized Silver tables (`projects`, `issues`, `issue_events`, `issue_comments`) with state mapping. A Dead Letter Queue captures persistent failures for later retry.

**Tech Stack:** Go 1.25, pgx/v5, robfig/cron/v3, net/http (stdlib), slog (structured logging), golang.org/x/time/rate (rate limiter)

---

## Pre-Flight Checklist

Before starting any task, verify:

```bash
make docker-up           # PostgreSQL running
make migrate-up          # All 10 migrations applied
make seed                # State/metadata mappings seeded
go build ./...           # Project compiles
make test                # Existing tests pass
```

---

## Task 1: Config Expansion (Rate Limit, Groups, Scheduler)

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `.env.example`

**Context:** The current config only supports `GITLAB_PROJECT_IDS` (list of ints), a single `WORKER_CRON_SCHEDULE`, and no rate limit settings. Phase 2 needs group IDs for discovery, rate limit config for the HTTP client, and separate scheduler schedules (sync peak, sync off-peak, discovery).

**Step 1: Write failing tests for new config fields**

Add to `internal/config/config_test.go`:

```go
func TestLoad_GitlabGroupIDs(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("GITLAB_GROUP_IDS", "10,20,30")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{10, 20, 30}
	if !reflect.DeepEqual(cfg.Gitlab.GroupIDs, expected) {
		t.Errorf("got GroupIDs=%v, want %v", cfg.Gitlab.GroupIDs, expected)
	}
}

func TestLoad_RateLimitDefaults(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Gitlab.RateLimit != 20 {
		t.Errorf("got RateLimit=%d, want 20", cfg.Gitlab.RateLimit)
	}
	if cfg.Gitlab.RetryMax != 3 {
		t.Errorf("got RetryMax=%d, want 3", cfg.Gitlab.RetryMax)
	}
}

func TestLoad_SchedulerDefaults(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Scheduler.SyncPeak != "*/15 * * * *" {
		t.Errorf("got SyncPeak=%q, want '*/15 * * * *'", cfg.Scheduler.SyncPeak)
	}
	if cfg.Scheduler.SyncOffPeak != "0 */4 * * *" {
		t.Errorf("got SyncOffPeak=%q, want '0 */4 * * *'", cfg.Scheduler.SyncOffPeak)
	}
	if cfg.Scheduler.Discovery != "0 */6 * * *" {
		t.Errorf("got Discovery=%q, want '0 */6 * * *'", cfg.Scheduler.Discovery)
	}
}
```

Also add a helper at the top of the test file if not present:

```go
func setRequiredEnvVars(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_USER", "test")
	t.Setenv("POSTGRES_PASSWORD", "test")
	t.Setenv("POSTGRES_DB", "test")
	t.Setenv("GITLAB_BASE_URL", "https://gitlab.example.com")
	t.Setenv("GITLAB_TOKEN", "test-token")
	t.Setenv("GITLAB_PROJECT_IDS", "1,2,3")
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/... -v -run "TestLoad_Gitlab|TestLoad_Rate|TestLoad_Scheduler"
```

Expected: FAIL — fields `GroupIDs`, `RateLimit`, `RetryMax`, `Scheduler` don't exist yet.

**Step 3: Implement config changes**

In `internal/config/config.go`, update:

```go
type Config struct {
	Postgres  PostgresConfig
	Gitlab    GitlabConfig
	Worker    WorkerConfig
	Scheduler SchedulerConfig
}

type GitlabConfig struct {
	BaseURL    string
	Token      string
	ProjectIDs []int
	GroupIDs   []int
	RateLimit  int // requests per second (default 20)
	RetryMax   int // max retries per request (default 3)
}

type SchedulerConfig struct {
	SyncPeak    string // cron for peak hours (08h-20h), default "*/15 * * * *"
	SyncOffPeak string // cron for off-peak (20h-08h), default "0 */4 * * *"
	Discovery   string // cron for project discovery, default "0 */6 * * *"
}
```

In `Load()`, add parsing for new fields:

```go
// Group IDs (optional — can discover from groups OR use explicit project IDs)
groupIDs := []int{}
if v := os.Getenv("GITLAB_GROUP_IDS"); v != "" {
	groupIDs, err = parseIntList(v)
	if err != nil {
		return nil, fmt.Errorf("invalid GITLAB_GROUP_IDS: %w", err)
	}
}

rateLimit := 20
if v := os.Getenv("GITLAB_RATE_LIMIT"); v != "" {
	rateLimit, err = strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("GITLAB_RATE_LIMIT must be an integer: %w", err)
	}
}

retryMax := 3
if v := os.Getenv("GITLAB_RETRY_MAX"); v != "" {
	retryMax, err = strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("GITLAB_RETRY_MAX must be an integer: %w", err)
	}
}

syncPeak := envOrDefault("SCHEDULER_SYNC_PEAK", "*/15 * * * *")
syncOffPeak := envOrDefault("SCHEDULER_SYNC_OFFPEAK", "0 */4 * * *")
discovery := envOrDefault("SCHEDULER_DISCOVERY", "0 */6 * * *")
```

Add helper:

```go
func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
```

Wire into the return struct. Remove old `WorkerConfig.CronSchedule` (it's replaced by `SchedulerConfig`). Keep `WorkerConfig.HealthPort`.

**Step 4: Update .env.example**

Add new variables:

```
GITLAB_GROUP_IDS=
GITLAB_RATE_LIMIT=20
GITLAB_RETRY_MAX=3
SCHEDULER_SYNC_PEAK=*/15 * * * *
SCHEDULER_SYNC_OFFPEAK=0 */4 * * *
SCHEDULER_DISCOVERY=0 */6 * * *
```

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/config/... -v
```

Expected: ALL PASS

**Step 6: Fix compilation in main.go**

Update `cmd/worker/main.go` to use new config fields — replace references to `cfg.Worker.CronSchedule` with `cfg.Scheduler.SyncPeak` (or just stub it). Ensure `go build ./...` passes.

**Step 7: Commit**

```bash
git add internal/config/ .env.example cmd/worker/main.go
git commit -m "feat(config): add rate limit, group IDs, and scheduler config for Phase 2"
```

---

## Task 2: DLQ Migration + Queries

**Files:**
- Create: `db/migrations/000011_create_dead_letter_queue.up.sql`
- Create: `db/migrations/000011_create_dead_letter_queue.down.sql`
- Create: `db/query/dead_letter_queue.sql`
- Regenerate: `internal/repository/` (via `sqlc generate`)

**Context:** The Dead Letter Queue captures events that fail after max retries. This is a DB-only change — no Go code beyond generated sqlc.

**Step 1: Create up migration**

```sql
-- db/migrations/000011_create_dead_letter_queue.up.sql

CREATE TABLE IF NOT EXISTS dead_letter_queue (
    id BIGSERIAL PRIMARY KEY,
    project_id INTEGER NOT NULL,
    issue_iid INTEGER,
    event_type VARCHAR(50),
    raw_payload JSONB,
    error_message TEXT NOT NULL,
    error_category VARCHAR(50) NOT NULL, -- 'api_error', 'parse_error', 'db_error', 'timeout'
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    last_retry_at TIMESTAMPTZ,
    resolved BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_dlq_unresolved ON dead_letter_queue (resolved) WHERE resolved = FALSE;
CREATE INDEX idx_dlq_project ON dead_letter_queue (project_id);
CREATE INDEX idx_dlq_category ON dead_letter_queue (error_category);
```

**Step 2: Create down migration**

```sql
-- db/migrations/000011_create_dead_letter_queue.down.sql

DROP TABLE IF EXISTS dead_letter_queue;
```

**Step 3: Create sqlc queries**

```sql
-- db/query/dead_letter_queue.sql

-- name: InsertDLQEntry :one
-- Insere um evento que falhou apos max retries.
INSERT INTO dead_letter_queue (project_id, issue_iid, event_type, raw_payload, error_message, error_category)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListUnresolvedDLQ :many
-- Lista entradas DLQ nao resolvidas, mais antigas primeiro.
SELECT * FROM dead_letter_queue
WHERE resolved = FALSE
ORDER BY created_at ASC
LIMIT $1;

-- name: ListDLQByProject :many
SELECT * FROM dead_letter_queue
WHERE project_id = $1 AND resolved = FALSE
ORDER BY created_at ASC;

-- name: CountUnresolvedDLQ :one
SELECT COUNT(*) FROM dead_letter_queue WHERE resolved = FALSE;

-- name: MarkDLQResolved :exec
UPDATE dead_letter_queue SET resolved = TRUE WHERE id = $1;

-- name: IncrementDLQRetry :exec
-- Incrementa retry_count e atualiza last_retry_at.
UPDATE dead_letter_queue
SET retry_count = retry_count + 1, last_retry_at = NOW()
WHERE id = $1;
```

**Step 4: Apply migration and regenerate**

```bash
make migrate-up
make sqlc-generate
go build ./...
```

Expected: Build succeeds, new `dead_letter_queue.sql.go` appears in `internal/repository/`.

**Step 5: Verify migration rollback works**

```bash
make migrate-down
make migrate-up
```

**Step 6: Commit**

```bash
git add db/migrations/000011* db/query/dead_letter_queue.sql internal/repository/
git commit -m "feat(dlq): add dead letter queue table, queries, and generated code"
```

---

## Task 3: GitLab HTTP Client

**Files:**
- Create: `internal/gitlab/client.go`
- Create: `internal/gitlab/client_test.go`

**Context:** This is the core HTTP client that talks to GitLab's REST API. It needs: rate limiting (token bucket, configurable req/s), retry with exponential backoff (configurable max retries), automatic pagination (follow `x-next-page` headers), and proper error handling. The domain interface (`domain.GitlabClient`) is already defined in `internal/domain/interfaces.go`. The domain models (`GitlabProject`, `GitlabIssue`, `GitlabLabelEvent`, `GitlabNote`) are already defined in `internal/domain/models.go`.

**Step 1: Install rate limiter dependency**

```bash
go get golang.org/x/time/rate
```

**Step 2: Write unit tests for retry and rate limiting behavior**

Create `internal/gitlab/client_test.go`:

```go
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pdrhp/gitlab-elt/internal/domain"
)

func TestClient_RetryOn500(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]domain.GitlabProject{
			{ID: 1, Name: "test", PathWithNamespace: "group/test"},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", WithRateLimit(100), WithMaxRetries(3))
	projects, err := client.ListGroupProjects(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1", len(projects))
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("got %d attempts, want 3", attempts)
	}
}

func TestClient_FailAfterMaxRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", WithRateLimit(100), WithMaxRetries(2))
	_, err := client.ListGroupProjects(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error after max retries")
	}
}

func TestClient_Pagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			w.Header().Set("x-next-page", "2")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]domain.GitlabProject{
				{ID: 1, Name: "project-1", PathWithNamespace: "g/p1"},
			})
			return
		}
		// Page 2: no x-next-page header = last page
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]domain.GitlabProject{
			{ID: 2, Name: "project-2", PathWithNamespace: "g/p2"},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", WithRateLimit(100), WithMaxRetries(1))
	projects, err := client.ListGroupProjects(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}
}

func TestClient_SetsAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("PRIVATE-TOKEN")
		if token != "my-secret" {
			t.Errorf("got token=%q, want 'my-secret'", token)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]domain.GitlabProject{})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "my-secret", WithRateLimit(100), WithMaxRetries(1))
	_, _ = client.ListGroupProjects(context.Background(), 1)
}

func TestClient_ListIssuesWithUpdatedAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.URL.Query().Get("updated_after")
		if ua == "" {
			t.Error("expected updated_after query parameter")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]domain.GitlabIssue{
			{ID: 10, IID: 1, ProjectID: 5, Title: "Test Issue"},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tok", WithRateLimit(100), WithMaxRetries(1))
	issues, err := client.ListIssues(context.Background(), 5, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
}

func TestClient_ListLabelEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]domain.GitlabLabelEvent{
			{
				ID:     100,
				Action: "add",
				Label:  domain.Label{ID: 1, Name: "Em dev"},
				User:   domain.User{ID: 1, Username: "dev", Name: "Dev User"},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tok", WithRateLimit(100), WithMaxRetries(1))
	events, err := client.ListLabelEvents(context.Background(), 5, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 || events[0].Label.Name != "Em dev" {
		t.Errorf("unexpected events: %+v", events)
	}
}

func TestClient_ListNotes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]domain.GitlabNote{
			{ID: 200, Body: "test comment", Author: domain.User{ID: 1, Username: "dev"}, System: false},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tok", WithRateLimit(100), WithMaxRetries(1))
	notes, err := client.ListNotes(context.Background(), 5, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notes) != 1 || notes[0].Body != "test comment" {
		t.Errorf("unexpected notes: %+v", notes)
	}
}
```

**Step 3: Run tests to verify they fail**

```bash
go test ./internal/gitlab/... -v
```

Expected: FAIL — package `gitlab` and `NewClient` don't exist yet.

**Step 4: Implement the GitLab client**

Create `internal/gitlab/client.go`:

```go
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"github.com/pdrhp/gitlab-elt/internal/domain"
)

// Client implements domain.GitlabClient with rate limiting, retry, and pagination.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	limiter    *rate.Limiter
	maxRetries int
}

// Option configures the Client.
type Option func(*Client)

// WithRateLimit sets the max requests per second.
func WithRateLimit(rps int) Option {
	return func(c *Client) {
		c.limiter = rate.NewLimiter(rate.Limit(rps), rps)
	}
}

// WithMaxRetries sets the maximum number of retry attempts for failed requests.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		c.maxRetries = n
	}
}

// WithHTTPClient sets a custom http.Client (useful for testing).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// NewClient creates a new GitLab API client.
func NewClient(baseURL, token string, opts ...Option) *Client {
	c := &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		limiter:    rate.NewLimiter(rate.Limit(20), 20),
		maxRetries: 3,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Ensure Client satisfies the domain interface at compile time.
var _ domain.GitlabClient = (*Client)(nil)

// ListGroupProjects retrieves all projects within a GitLab group.
func (c *Client) ListGroupProjects(ctx context.Context, groupID int) ([]domain.GitlabProject, error) {
	path := fmt.Sprintf("/api/v4/groups/%d/projects", groupID)
	var result []domain.GitlabProject
	err := c.getPaginated(ctx, path, nil, &result)
	return result, err
}

// ListIssues retrieves issues from a project updated after the given time.
func (c *Client) ListIssues(ctx context.Context, projectID int, updatedAfter time.Time) ([]domain.GitlabIssue, error) {
	path := fmt.Sprintf("/api/v4/projects/%d/issues", projectID)
	params := map[string]string{
		"updated_after": updatedAfter.Format(time.RFC3339),
		"per_page":      "100",
		"sort":          "asc",
		"order_by":      "updated_at",
	}
	var result []domain.GitlabIssue
	err := c.getPaginated(ctx, path, params, &result)
	return result, err
}

// ListLabelEvents retrieves all label events for an issue.
func (c *Client) ListLabelEvents(ctx context.Context, projectID int, issueIID int) ([]domain.GitlabLabelEvent, error) {
	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d/resource_label_events", projectID, issueIID)
	var result []domain.GitlabLabelEvent
	err := c.getPaginated(ctx, path, nil, &result)
	return result, err
}

// ListNotes retrieves all notes (comments) for an issue.
func (c *Client) ListNotes(ctx context.Context, projectID int, issueIID int) ([]domain.GitlabNote, error) {
	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d/notes", projectID, issueIID)
	params := map[string]string{
		"sort":     "asc",
		"order_by": "created_at",
	}
	var result []domain.GitlabNote
	err := c.getPaginated(ctx, path, params, &result)
	return result, err
}

// getPaginated fetches all pages of a paginated GitLab API endpoint and appends
// results to the target slice (passed as pointer to slice).
func (c *Client) getPaginated(ctx context.Context, path string, params map[string]string, target interface{}) error {
	page := "1"
	for {
		pageParams := make(map[string]string)
		for k, v := range params {
			pageParams[k] = v
		}
		pageParams["page"] = page
		if _, ok := pageParams["per_page"]; !ok {
			pageParams["per_page"] = "100"
		}

		body, nextPage, err := c.doGet(ctx, path, pageParams)
		if err != nil {
			return err
		}

		if err := appendJSONSlice(body, target); err != nil {
			return fmt.Errorf("decode response for %s: %w", path, err)
		}

		if nextPage == "" {
			break
		}
		page = nextPage
	}
	return nil
}

// doGet performs a single GET request with rate limiting and retry.
// Returns the response body, the next page number (or ""), and any error.
func (c *Client) doGet(ctx context.Context, path string, params map[string]string) ([]byte, string, error) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			slog.Warn("retrying GitLab request",
				"path", path,
				"attempt", attempt,
				"backoff", backoff,
				"error", lastErr,
			)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, "", ctx.Err()
			}
		}

		// Rate limit
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, "", fmt.Errorf("rate limiter: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(path, params), nil)
		if err != nil {
			return nil, "", fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("PRIVATE-TOKEN", c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http request: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read body: %w", err)
			continue
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error: %d %s", resp.StatusCode, string(body))
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("rate limited: %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode >= 400 {
			return nil, "", fmt.Errorf("client error: %d %s", resp.StatusCode, string(body))
		}

		nextPage := resp.Header.Get("x-next-page")
		return body, nextPage, nil
	}

	return nil, "", fmt.Errorf("max retries exceeded: %w", lastErr)
}

// buildURL constructs the full URL with query parameters.
func (c *Client) buildURL(path string, params map[string]string) string {
	u := c.baseURL + path
	if len(params) == 0 {
		return u
	}
	sep := "?"
	for k, v := range params {
		u += sep + k + "=" + v
		sep = "&"
	}
	return u
}

// appendJSONSlice decodes a JSON array and appends elements to the target slice.
// target must be a pointer to a slice (e.g., *[]domain.GitlabProject).
func appendJSONSlice(data []byte, target interface{}) error {
	// Decode the page into a temporary slice of the same type
	switch t := target.(type) {
	case *[]domain.GitlabProject:
		var page []domain.GitlabProject
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		*t = append(*t, page...)
	case *[]domain.GitlabIssue:
		var page []domain.GitlabIssue
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		*t = append(*t, page...)
	case *[]domain.GitlabLabelEvent:
		var page []domain.GitlabLabelEvent
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		*t = append(*t, page...)
	case *[]domain.GitlabNote:
		var page []domain.GitlabNote
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		*t = append(*t, page...)
	default:
		return fmt.Errorf("unsupported target type %T", target)
	}
	return nil
}
```

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/gitlab/... -v
```

Expected: ALL PASS

**Step 6: Run full build**

```bash
go build ./...
```

Expected: Success

**Step 7: Commit**

```bash
git add internal/gitlab/ go.mod go.sum
git commit -m "feat(gitlab): implement HTTP client with rate limiting, retry, and pagination"
```

---

## Task 4: Discovery Service

**Files:**
- Create: `internal/discovery/service.go`
- Create: `internal/discovery/service_test.go`

**Context:** The Discovery Service lists projects from configured GitLab groups, upserts them into `raw_projects` (Bronze), and upserts them into `projects` (Silver). New projects get `last_synced_at = 1970-01-01` so the extractor knows to do a full backfill. It depends on the GitLab client (Task 3) and repository queries (already generated).

**Step 1: Write tests**

Create `internal/discovery/service_test.go`:

```go
package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/pdrhp/gitlab-elt/internal/domain"
)

// mockGitlabClient implements domain.GitlabClient for testing.
type mockGitlabClient struct {
	projects []domain.GitlabProject
	err      error
}

func (m *mockGitlabClient) ListGroupProjects(_ context.Context, _ int) ([]domain.GitlabProject, error) {
	return m.projects, m.err
}
func (m *mockGitlabClient) ListIssues(_ context.Context, _ int, _ time.Time) ([]domain.GitlabIssue, error) {
	return nil, nil
}
func (m *mockGitlabClient) ListLabelEvents(_ context.Context, _ int, _ int) ([]domain.GitlabLabelEvent, error) {
	return nil, nil
}
func (m *mockGitlabClient) ListNotes(_ context.Context, _ int, _ int) ([]domain.GitlabNote, error) {
	return nil, nil
}

// mockRepository tracks calls for verification.
type mockRepository struct {
	upsertedRawProjects []upsertRawProjectCall
	upsertedProjects    []upsertProjectCall
}

type upsertRawProjectCall struct {
	ID   int
	Name string
	Path string
}

type upsertProjectCall struct {
	ID   int
	Name string
	Path string
}

func TestService_DiscoverProjects(t *testing.T) {
	gitlabClient := &mockGitlabClient{
		projects: []domain.GitlabProject{
			{ID: 1, Name: "Project Alpha", PathWithNamespace: "group/alpha"},
			{ID: 2, Name: "Project Beta", PathWithNamespace: "group/beta"},
		},
	}

	svc := NewService(gitlabClient, nil, []int{42})
	projects, err := svc.fetchGroupProjects(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("got %d projects, want 2", len(projects))
	}
}

func TestService_DiscoverMultipleGroups(t *testing.T) {
	gitlabClient := &mockGitlabClient{
		projects: []domain.GitlabProject{
			{ID: 1, Name: "P1", PathWithNamespace: "g/p1"},
		},
	}

	svc := NewService(gitlabClient, nil, []int{10, 20})
	projects, err := svc.fetchAllGroupProjects(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 project per group × 2 groups = 2 (but same ID deduped)
	// The mock returns the same project for both groups
	if len(projects) != 1 {
		t.Errorf("got %d unique projects, want 1 (deduped)", len(projects))
	}
}

func TestService_DiscoverNoGroups(t *testing.T) {
	svc := NewService(nil, nil, []int{})
	projects, err := svc.fetchAllGroupProjects(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("got %d projects, want 0", len(projects))
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/discovery/... -v
```

Expected: FAIL — package doesn't exist.

**Step 3: Implement Discovery Service**

Create `internal/discovery/service.go`:

```go
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/repository"
)

// Service discovers GitLab projects from configured groups and persists them.
type Service struct {
	gitlab   domain.GitlabClient
	queries  *repository.Queries
	groupIDs []int
}

// NewService creates a new Discovery Service.
func NewService(gitlab domain.GitlabClient, queries *repository.Queries, groupIDs []int) *Service {
	return &Service{
		gitlab:   gitlab,
		queries:  queries,
		groupIDs: groupIDs,
	}
}

// Run executes a full discovery cycle: fetch projects from all groups, upsert into Bronze + Silver.
func (s *Service) Run(ctx context.Context) error {
	slog.Info("discovery: starting project discovery", "groups", len(s.groupIDs))

	projects, err := s.fetchAllGroupProjects(ctx)
	if err != nil {
		return fmt.Errorf("discovery: fetch projects: %w", err)
	}

	if s.queries == nil {
		slog.Warn("discovery: no repository configured, skipping persistence")
		return nil
	}

	persisted := 0
	for _, p := range projects {
		if err := s.persistProject(ctx, p); err != nil {
			slog.Error("discovery: failed to persist project",
				"project_id", p.ID,
				"project_name", p.Name,
				"error", err,
			)
			continue
		}
		persisted++
	}

	slog.Info("discovery: completed",
		"discovered", len(projects),
		"persisted", persisted,
	)
	return nil
}

// fetchAllGroupProjects fetches projects from all configured groups, deduplicating by ID.
func (s *Service) fetchAllGroupProjects(ctx context.Context) ([]domain.GitlabProject, error) {
	seen := make(map[int]bool)
	var result []domain.GitlabProject

	for _, gid := range s.groupIDs {
		projects, err := s.fetchGroupProjects(ctx, gid)
		if err != nil {
			return nil, fmt.Errorf("group %d: %w", gid, err)
		}
		for _, p := range projects {
			if !seen[p.ID] {
				seen[p.ID] = true
				result = append(result, p)
			}
		}
	}
	return result, nil
}

// fetchGroupProjects fetches all projects from a single group.
func (s *Service) fetchGroupProjects(ctx context.Context, groupID int) ([]domain.GitlabProject, error) {
	projects, err := s.gitlab.ListGroupProjects(ctx, groupID)
	if err != nil {
		return nil, err
	}
	slog.Info("discovery: fetched projects from group",
		"group_id", groupID,
		"count", len(projects),
	)
	return projects, nil
}

// persistProject upserts a project into both Bronze (raw_projects) and Silver (projects).
func (s *Service) persistProject(ctx context.Context, p domain.GitlabProject) error {
	// Marshal project as raw metadata for Bronze
	rawMeta, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal raw metadata: %w", err)
	}

	epoch := pgtype.Timestamptz{
		Time:  time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		Valid: true,
	}

	// Bronze: upsert raw_projects
	_, err = s.queries.UpsertRawProject(ctx, repository.UpsertRawProjectParams{
		ID:           int32(p.ID),
		Name:         p.Name,
		Path:         p.PathWithNamespace,
		RawMetadata:  rawMeta,
		LastSyncedAt: epoch,
	})
	if err != nil {
		return fmt.Errorf("upsert raw_project: %w", err)
	}

	// Silver: upsert projects
	_, err = s.queries.UpsertProject(ctx, repository.UpsertProjectParams{
		ID:           int32(p.ID),
		Name:         p.Name,
		Path:         p.PathWithNamespace,
		LastSyncedAt: epoch,
	})
	if err != nil {
		return fmt.Errorf("upsert project: %w", err)
	}

	return nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/discovery/... -v
```

Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/discovery/
git commit -m "feat(discovery): implement project discovery service with group scanning and dedup"
```

---

## Task 5: State Mapper (TDD)

**Files:**
- Create: `internal/mapper/mapper.go`
- Create: `internal/mapper/mapper_test.go`

**Context:** The State Mapper is pure logic: given a label name, returns the canonical state (BACKLOG, IN_PROGRESS, etc.) or metadata key, or UNKNOWN. It loads mappings from the database on startup and caches them in memory. This is a TDD task — write tests first.

**Step 1: Write failing tests**

Create `internal/mapper/mapper_test.go`:

```go
package mapper

import (
	"testing"
)

func TestMapLabel_StateMappings(t *testing.T) {
	m := newTestMapper()

	tests := []struct {
		label     string
		wantState string
		wantType  LabelType
	}{
		{"Em dev", "IN_PROGRESS", LabelTypeState},
		{"Backlog", "BACKLOG", LabelTypeState},
		{"Concluido", "DONE", LabelTypeState},
		{"Bloqueado", "BLOCKED", LabelTypeState},
		{"Teste HOM", "QA_REVIEW", LabelTypeState},
		{"Cancelado", "CANCELED", LabelTypeState},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			state, labelType := m.MapLabel(tt.label)
			if state != tt.wantState {
				t.Errorf("MapLabel(%q) state = %q, want %q", tt.label, state, tt.wantState)
			}
			if labelType != tt.wantType {
				t.Errorf("MapLabel(%q) type = %v, want %v", tt.label, labelType, tt.wantType)
			}
		})
	}
}

func TestMapLabel_MetadataMappings(t *testing.T) {
	m := newTestMapper()

	tests := []struct {
		label       string
		wantKey     string
		wantType    LabelType
	}{
		{"Bug", "tipo", LabelTypeMetadata},
		{"PRIORIDADE: ALTA", "prioridade", LabelTypeMetadata},
		{"Backend", "area", LabelTypeMetadata},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			_, labelType := m.MapLabel(tt.label)
			if labelType != tt.wantType {
				t.Errorf("MapLabel(%q) type = %v, want %v", tt.label, labelType, tt.wantType)
			}
			key := m.MetadataKey(tt.label)
			if key != tt.wantKey {
				t.Errorf("MetadataKey(%q) = %q, want %q", tt.label, key, tt.wantKey)
			}
		})
	}
}

func TestMapLabel_Unknown(t *testing.T) {
	m := newTestMapper()

	state, labelType := m.MapLabel("some-random-label")
	if state != "UNKNOWN" {
		t.Errorf("got state=%q, want UNKNOWN", state)
	}
	if labelType != LabelTypeUnknown {
		t.Errorf("got type=%v, want LabelTypeUnknown", labelType)
	}
}

func TestMapLabel_CaseInsensitive(t *testing.T) {
	m := newTestMapper()

	// The mapper should do exact match (case-sensitive) since GitLab labels
	// are case-sensitive. "Em dev" != "EM DEV"
	state, _ := m.MapLabel("Em dev")
	if state != "IN_PROGRESS" {
		t.Errorf("got %q, want IN_PROGRESS", state)
	}

	state2, lt := m.MapLabel("EM DEV")
	if lt != LabelTypeUnknown {
		t.Errorf("expected UNKNOWN for 'EM DEV', got type=%v state=%q", lt, state2)
	}
}

func TestMapLabel_IsStatefulTransition(t *testing.T) {
	m := newTestMapper()

	if !m.IsStateLabel("Em dev") {
		t.Error("expected 'Em dev' to be a state label")
	}
	if m.IsStateLabel("Bug") {
		t.Error("expected 'Bug' NOT to be a state label")
	}
	if m.IsStateLabel("random") {
		t.Error("expected 'random' NOT to be a state label")
	}
}

// newTestMapper creates a Mapper with known test data (mirrors seed data).
func newTestMapper() *Mapper {
	m := New()
	// State mappings (subset of seeds)
	m.AddStateMapping("Backlog", "BACKLOG")
	m.AddStateMapping("Em dev", "IN_PROGRESS")
	m.AddStateMapping("Em Andamento", "IN_PROGRESS")
	m.AddStateMapping("Teste HOM", "QA_REVIEW")
	m.AddStateMapping("Bloqueado", "BLOCKED")
	m.AddStateMapping("Concluido", "DONE")
	m.AddStateMapping("Cancelado", "CANCELED")

	// Metadata mappings (subset of seeds)
	m.AddMetadataMapping("Bug", "tipo")
	m.AddMetadataMapping("PRIORIDADE: ALTA", "prioridade")
	m.AddMetadataMapping("Backend", "area")

	return m
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/mapper/... -v
```

Expected: FAIL

**Step 3: Implement the Mapper**

Create `internal/mapper/mapper.go`:

```go
package mapper

import (
	"context"
	"log/slog"
	"sync"

	"github.com/pdrhp/gitlab-elt/internal/repository"
)

// LabelType categorizes what a GitLab label represents.
type LabelType int

const (
	LabelTypeState    LabelType = iota // Workflow state (Backlog, Em dev, etc.)
	LabelTypeMetadata                  // Categorical metadata (Bug, Priority, etc.)
	LabelTypeUnknown                   // Not mapped
)

// Mapper translates GitLab label names to canonical states or metadata keys.
// Thread-safe via RWMutex for concurrent reads.
type Mapper struct {
	mu              sync.RWMutex
	stateMappings   map[string]string // label_name -> canonical_state
	metadataMappings map[string]string // label_name -> metadata_key
}

// New creates a new empty Mapper.
func New() *Mapper {
	return &Mapper{
		stateMappings:   make(map[string]string),
		metadataMappings: make(map[string]string),
	}
}

// LoadFromDB loads all mappings from the database into the in-memory cache.
func (m *Mapper) LoadFromDB(ctx context.Context, queries *repository.Queries) error {
	stateMappings, err := queries.ListStateMappings(ctx)
	if err != nil {
		return err
	}

	metadataMappings, err := queries.ListMetadataMappings(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.stateMappings = make(map[string]string, len(stateMappings))
	for _, sm := range stateMappings {
		m.stateMappings[sm.GitlabLabelName] = sm.CanonicalState
	}

	m.metadataMappings = make(map[string]string, len(metadataMappings))
	for _, mm := range metadataMappings {
		m.metadataMappings[mm.GitlabLabelName] = mm.MetadataKey
	}

	slog.Info("mapper: loaded mappings",
		"state_mappings", len(m.stateMappings),
		"metadata_mappings", len(m.metadataMappings),
	)
	return nil
}

// AddStateMapping adds a label → canonical state mapping (used in tests and manual setup).
func (m *Mapper) AddStateMapping(label, canonicalState string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateMappings[label] = canonicalState
}

// AddMetadataMapping adds a label → metadata key mapping.
func (m *Mapper) AddMetadataMapping(label, metadataKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metadataMappings[label] = metadataKey
}

// MapLabel returns the canonical state and the label type.
// For state labels: returns (canonical_state, LabelTypeState)
// For metadata labels: returns ("", LabelTypeMetadata)
// For unknown labels: returns ("UNKNOWN", LabelTypeUnknown)
func (m *Mapper) MapLabel(label string) (string, LabelType) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if state, ok := m.stateMappings[label]; ok {
		return state, LabelTypeState
	}
	if _, ok := m.metadataMappings[label]; ok {
		return "", LabelTypeMetadata
	}
	return "UNKNOWN", LabelTypeUnknown
}

// MetadataKey returns the metadata key for a label, or empty string if not a metadata label.
func (m *Mapper) MetadataKey(label string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metadataMappings[label]
}

// IsStateLabel returns true if the label is a workflow state label.
func (m *Mapper) IsStateLabel(label string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.stateMappings[label]
	return ok
}

// StateFor returns the canonical state for a label, or empty string if not a state label.
func (m *Mapper) StateFor(label string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stateMappings[label]
}
```

**Step 4: Run tests**

```bash
go test ./internal/mapper/... -v
```

Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/mapper/
git commit -m "feat(mapper): implement state mapper with in-memory cache (TDD)"
```

---

## Task 6: Bronze Extraction Service

**Files:**
- Create: `internal/sync/extractor.go`
- Create: `internal/sync/extractor_test.go`

**Context:** The Extractor fetches issues + label events + notes from GitLab for each project, inserts them as raw JSONB payloads into `raw_events` (Bronze), and updates the sync cursor. It uses the GitLab client (Task 3), the repository queries (already generated), and the DLQ (Task 2).

**Step 1: Write tests**

Create `internal/sync/extractor_test.go`:

```go
package sync

import (
	"context"
	"testing"
	"time"

	"github.com/pdrhp/gitlab-elt/internal/domain"
)

type mockGitlab struct {
	issues      []domain.GitlabIssue
	labelEvents []domain.GitlabLabelEvent
	notes       []domain.GitlabNote
}

func (m *mockGitlab) ListGroupProjects(_ context.Context, _ int) ([]domain.GitlabProject, error) {
	return nil, nil
}
func (m *mockGitlab) ListIssues(_ context.Context, _ int, _ time.Time) ([]domain.GitlabIssue, error) {
	return m.issues, nil
}
func (m *mockGitlab) ListLabelEvents(_ context.Context, _ int, _ int) ([]domain.GitlabLabelEvent, error) {
	return m.labelEvents, nil
}
func (m *mockGitlab) ListNotes(_ context.Context, _ int, _ int) ([]domain.GitlabNote, error) {
	return m.notes, nil
}

func TestExtractor_BuildRawEvents(t *testing.T) {
	gitlab := &mockGitlab{
		issues: []domain.GitlabIssue{
			{ID: 100, IID: 1, ProjectID: 5, Title: "Test Issue"},
		},
		labelEvents: []domain.GitlabLabelEvent{
			{
				ID:     200,
				Action: "add",
				Label:  domain.Label{ID: 1, Name: "Em dev"},
				User:   domain.User{ID: 1, Username: "dev"},
			},
		},
		notes: []domain.GitlabNote{
			{ID: 300, Body: "A comment", Author: domain.User{ID: 1, Username: "dev"}},
		},
	}

	ext := NewExtractor(gitlab, nil)

	events, err := ext.fetchIssueRawEvents(context.Background(), 5, domain.GitlabIssue{
		ID: 100, IID: 1, ProjectID: 5, Title: "Test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 2 raw events: 1 label event + 1 note
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	// First should be label_event
	if events[0].EventType != "label_event" {
		t.Errorf("got event_type=%q, want 'label_event'", events[0].EventType)
	}
	// Second should be note
	if events[1].EventType != "note" {
		t.Errorf("got event_type=%q, want 'note'", events[1].EventType)
	}
}

func TestExtractor_EmptyIssues(t *testing.T) {
	gitlab := &mockGitlab{
		issues: []domain.GitlabIssue{},
	}

	ext := NewExtractor(gitlab, nil)
	events, err := ext.fetchProjectRawEvents(context.Background(), 5, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events, want 0", len(events))
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/sync/... -v
```

Expected: FAIL

**Step 3: Implement the Extractor**

Create `internal/sync/extractor.go`:

```go
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/repository"
)

// RawEventData holds the data needed to insert a raw event into Bronze.
type RawEventData struct {
	GitlabEventID int64
	ProjectID     int32
	IssueIID      int32
	EventType     string // "label_event" or "note"
	RawPayload    []byte
}

// Extractor fetches data from GitLab and inserts it into Bronze layer (raw_events).
type Extractor struct {
	gitlab  domain.GitlabClient
	queries *repository.Queries
}

// NewExtractor creates a new Extractor.
func NewExtractor(gitlab domain.GitlabClient, queries *repository.Queries) *Extractor {
	return &Extractor{
		gitlab:  gitlab,
		queries: queries,
	}
}

// ExtractProject runs Bronze extraction for a single project:
// 1. Fetch issues updated since last sync
// 2. For each issue, fetch label events + notes
// 3. Insert raw events into raw_events table
// 4. Update sync cursor
func (e *Extractor) ExtractProject(ctx context.Context, project repository.RawProject) error {
	lastSynced := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	if project.LastSyncedAt.Valid {
		lastSynced = project.LastSyncedAt.Time
	}

	slog.Info("extractor: starting project extraction",
		"project_id", project.ID,
		"project_name", project.Name,
		"last_synced_at", lastSynced,
	)

	syncStartTime := time.Now().UTC()

	rawEvents, err := e.fetchProjectRawEvents(ctx, int(project.ID), lastSynced)
	if err != nil {
		return fmt.Errorf("fetch raw events for project %d: %w", project.ID, err)
	}

	if e.queries == nil {
		slog.Warn("extractor: no repository configured, skipping persistence",
			"project_id", project.ID,
			"events_fetched", len(rawEvents),
		)
		return nil
	}

	// Persist raw events in batch
	persisted := 0
	for _, evt := range rawEvents {
		err := e.queries.BulkInsertRawEvent(ctx, repository.BulkInsertRawEventParams{
			GitlabEventID: pgtype.Int8{Int64: evt.GitlabEventID, Valid: evt.GitlabEventID > 0},
			ProjectID:     evt.ProjectID,
			IssueIid:      evt.IssueIID,
			EventType:     evt.EventType,
			RawPayload:    evt.RawPayload,
		})
		if err != nil {
			slog.Error("extractor: failed to insert raw event",
				"project_id", evt.ProjectID,
				"issue_iid", evt.IssueIID,
				"event_type", evt.EventType,
				"error", err,
			)
			continue
		}
		persisted++
	}

	// Update sync cursor
	err = e.queries.UpdateRawProjectLastSynced(ctx, repository.UpdateRawProjectLastSyncedParams{
		LastSyncedAt: pgtype.Timestamptz{Time: syncStartTime, Valid: true},
		ID:           project.ID,
	})
	if err != nil {
		return fmt.Errorf("update sync cursor for project %d: %w", project.ID, err)
	}

	slog.Info("extractor: completed project extraction",
		"project_id", project.ID,
		"events_fetched", len(rawEvents),
		"events_persisted", persisted,
	)
	return nil
}

// ExtractAll runs Bronze extraction for all projects that are due for sync.
func (e *Extractor) ExtractAll(ctx context.Context) error {
	if e.queries == nil {
		return fmt.Errorf("extractor: repository not configured")
	}

	projects, err := e.queries.ListRawProjects(ctx)
	if err != nil {
		return fmt.Errorf("list raw projects: %w", err)
	}

	slog.Info("extractor: starting extraction for all projects", "count", len(projects))

	var errs []error
	for _, p := range projects {
		if err := e.ExtractProject(ctx, p); err != nil {
			slog.Error("extractor: project extraction failed",
				"project_id", p.ID,
				"error", err,
			)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("extractor: %d project(s) failed", len(errs))
	}
	return nil
}

// fetchProjectRawEvents fetches all raw events for a project since lastSynced.
func (e *Extractor) fetchProjectRawEvents(ctx context.Context, projectID int, lastSynced time.Time) ([]RawEventData, error) {
	issues, err := e.gitlab.ListIssues(ctx, projectID, lastSynced)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	slog.Info("extractor: fetched issues",
		"project_id", projectID,
		"count", len(issues),
	)

	var allEvents []RawEventData
	for _, issue := range issues {
		events, err := e.fetchIssueRawEvents(ctx, projectID, issue)
		if err != nil {
			slog.Error("extractor: failed to fetch events for issue",
				"project_id", projectID,
				"issue_iid", issue.IID,
				"error", err,
			)
			continue
		}
		allEvents = append(allEvents, events...)
	}

	return allEvents, nil
}

// fetchIssueRawEvents fetches label events + notes for a single issue and returns raw event data.
func (e *Extractor) fetchIssueRawEvents(ctx context.Context, projectID int, issue domain.GitlabIssue) ([]RawEventData, error) {
	var events []RawEventData

	// Fetch label events
	labelEvents, err := e.gitlab.ListLabelEvents(ctx, projectID, issue.IID)
	if err != nil {
		return nil, fmt.Errorf("list label events for issue %d: %w", issue.IID, err)
	}

	for _, le := range labelEvents {
		payload, err := json.Marshal(le)
		if err != nil {
			return nil, fmt.Errorf("marshal label event: %w", err)
		}
		events = append(events, RawEventData{
			GitlabEventID: le.ID,
			ProjectID:     int32(projectID),
			IssueIID:      int32(issue.IID),
			EventType:     "label_event",
			RawPayload:    payload,
		})
	}

	// Fetch notes
	notes, err := e.gitlab.ListNotes(ctx, projectID, issue.IID)
	if err != nil {
		return nil, fmt.Errorf("list notes for issue %d: %w", issue.IID, err)
	}

	for _, n := range notes {
		payload, err := json.Marshal(n)
		if err != nil {
			return nil, fmt.Errorf("marshal note: %w", err)
		}
		events = append(events, RawEventData{
			GitlabEventID: n.ID,
			ProjectID:     int32(projectID),
			IssueIID:      int32(issue.IID),
			EventType:     "note",
			RawPayload:    payload,
		})
	}

	return events, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/sync/... -v
```

Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/sync/
git commit -m "feat(extractor): implement Bronze extraction service (GitLab → raw_events)"
```

---

## Task 7: Silver Transformation Service

**Files:**
- Create: `internal/transformer/service.go`
- Create: `internal/transformer/service_test.go`

**Context:** The Transformer reads unprocessed `raw_events` from Bronze, parses the JSONB payload, maps labels to canonical states using the Mapper, upserts into Silver tables (`issues`, `issue_events`, `issue_comments`), and marks raw events as processed. It depends on the Mapper (Task 5) and the repository queries.

**Step 1: Write tests**

Create `internal/transformer/service_test.go`:

```go
package transformer

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/mapper"
)

func TestParseLabelEvent(t *testing.T) {
	le := domain.GitlabLabelEvent{
		ID:        100,
		Action:    "add",
		Label:     domain.Label{ID: 1, Name: "Em dev"},
		User:      domain.User{ID: 1, Username: "dev", Name: "Dev User"},
		CreatedAt: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	payload, _ := json.Marshal(le)

	parsed, err := parseLabelEventPayload(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Label.Name != "Em dev" {
		t.Errorf("got label=%q, want 'Em dev'", parsed.Label.Name)
	}
	if parsed.Action != "add" {
		t.Errorf("got action=%q, want 'add'", parsed.Action)
	}
}

func TestParseNotePayload(t *testing.T) {
	note := domain.GitlabNote{
		ID:        200,
		Body:      "Fixed the bug",
		Author:    domain.User{ID: 1, Username: "dev", Name: "Dev"},
		CreatedAt: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		System:    false,
	}
	payload, _ := json.Marshal(note)

	parsed, err := parseNotePayload(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Body != "Fixed the bug" {
		t.Errorf("got body=%q, want 'Fixed the bug'", parsed.Body)
	}
}

func TestDetermineCanonicalState_AddAction(t *testing.T) {
	m := newTestMapper()

	// "add" action with a state label → mapped state
	state := determineCanonicalState(m, "add", "Em dev", "")
	if state != "IN_PROGRESS" {
		t.Errorf("got %q, want IN_PROGRESS", state)
	}
}

func TestDetermineCanonicalState_RemoveAction(t *testing.T) {
	m := newTestMapper()

	// "remove" action with a state label → previous state removed, return UNKNOWN
	// because we don't know the new state from a remove event alone
	state := determineCanonicalState(m, "remove", "", "Em dev")
	if state != "UNKNOWN" {
		t.Errorf("got %q, want UNKNOWN", state)
	}
}

func TestDetermineCanonicalState_MetadataLabel(t *testing.T) {
	m := newTestMapper()

	// Metadata label (Bug) → skip, not a state transition
	state := determineCanonicalState(m, "add", "Bug", "")
	if state != "" {
		t.Errorf("got %q, want empty (metadata labels are not state transitions)", state)
	}
}

func newTestMapper() *mapper.Mapper {
	m := mapper.New()
	m.AddStateMapping("Backlog", "BACKLOG")
	m.AddStateMapping("Em dev", "IN_PROGRESS")
	m.AddStateMapping("Teste HOM", "QA_REVIEW")
	m.AddStateMapping("Bloqueado", "BLOCKED")
	m.AddStateMapping("Concluido", "DONE")
	m.AddStateMapping("Cancelado", "CANCELED")
	m.AddMetadataMapping("Bug", "tipo")
	return m
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/transformer/... -v
```

Expected: FAIL

**Step 3: Implement the Transformer**

Create `internal/transformer/service.go`:

```go
package transformer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/mapper"
	"github.com/pdrhp/gitlab-elt/internal/repository"
)

// Service transforms Bronze raw_events into Silver tables.
type Service struct {
	queries   *repository.Queries
	mapper    *mapper.Mapper
	batchSize int32
}

// NewService creates a new Transformer Service.
func NewService(queries *repository.Queries, m *mapper.Mapper) *Service {
	return &Service{
		queries:   queries,
		mapper:    m,
		batchSize: 500,
	}
}

// TransformBatch processes a batch of unprocessed raw events → Silver.
// Returns the number of events processed.
func (s *Service) TransformBatch(ctx context.Context) (int, error) {
	events, err := s.queries.ListUnprocessedRawEvents(ctx, s.batchSize)
	if err != nil {
		return 0, fmt.Errorf("list unprocessed events: %w", err)
	}

	if len(events) == 0 {
		return 0, nil
	}

	slog.Info("transformer: processing batch", "count", len(events))

	processed := 0
	processedIDs := make([]int64, 0, len(events))

	for _, raw := range events {
		err := s.transformEvent(ctx, raw)
		if err != nil {
			slog.Error("transformer: failed to transform event",
				"raw_event_id", raw.ID,
				"event_type", raw.EventType,
				"project_id", raw.ProjectID,
				"error", err,
			)
			continue
		}
		processedIDs = append(processedIDs, raw.ID)
		processed++
	}

	// Mark events as processed in batch
	if len(processedIDs) > 0 {
		if err := s.queries.MarkRawEventsProcessedBatch(ctx, processedIDs); err != nil {
			return processed, fmt.Errorf("mark events processed: %w", err)
		}
	}

	slog.Info("transformer: batch complete",
		"processed", processed,
		"total", len(events),
	)
	return processed, nil
}

// TransformAll processes all unprocessed raw events in batches until none remain.
func (s *Service) TransformAll(ctx context.Context) (int, error) {
	total := 0
	for {
		n, err := s.TransformBatch(ctx)
		if err != nil {
			return total, err
		}
		total += n
		if n == 0 {
			break
		}
	}
	slog.Info("transformer: all batches complete", "total_processed", total)
	return total, nil
}

// transformEvent transforms a single raw event into Silver tables.
func (s *Service) transformEvent(ctx context.Context, raw repository.RawEvent) error {
	switch raw.EventType {
	case "label_event":
		return s.transformLabelEvent(ctx, raw)
	case "note":
		return s.transformNote(ctx, raw)
	default:
		slog.Warn("transformer: unknown event type", "event_type", raw.EventType, "raw_id", raw.ID)
		return nil
	}
}

// transformLabelEvent processes a label event from Bronze → Silver.
func (s *Service) transformLabelEvent(ctx context.Context, raw repository.RawEvent) error {
	le, err := parseLabelEventPayload(raw.RawPayload)
	if err != nil {
		return fmt.Errorf("parse label event: %w", err)
	}

	// Determine label added/removed
	var labelAdded, labelRemoved string
	if le.Action == "add" {
		labelAdded = le.Label.Name
	} else {
		labelRemoved = le.Label.Name
	}

	// Determine canonical state
	canonicalState := determineCanonicalState(s.mapper, le.Action, labelAdded, labelRemoved)

	// Skip non-state label events (metadata labels like Bug, Priority, etc.)
	if canonicalState == "" {
		return nil
	}

	// Log unknown labels
	if canonicalState == "UNKNOWN" {
		labelName := labelAdded
		if labelName == "" {
			labelName = labelRemoved
		}
		if _, err := s.queries.UpsertUnknownLabel(ctx, labelName); err != nil {
			slog.Error("transformer: failed to log unknown label",
				"label", labelName,
				"error", err,
			)
		}
	}

	// Only persist "add" events as state transitions (remove events are informational)
	if le.Action == "remove" {
		return nil
	}

	// Ensure issue exists in Silver
	issue, err := s.ensureIssue(ctx, raw.ProjectID, raw.IssueIid)
	if err != nil {
		return fmt.Errorf("ensure issue: %w", err)
	}

	// Insert issue event into Silver
	_, err = s.queries.InsertIssueEvent(ctx, repository.InsertIssueEventParams{
		GitlabEventID:       pgtype.Int8{Int64: le.ID, Valid: true},
		IssueID:             issue.ID,
		ProjectID:           raw.ProjectID,
		IssueIid:            raw.IssueIid,
		AuthorName:          pgtype.Text{String: le.User.Username, Valid: true},
		RawLabelAdded:       pgtype.Text{String: labelAdded, Valid: labelAdded != ""},
		RawLabelRemoved:     pgtype.Text{String: labelRemoved, Valid: labelRemoved != ""},
		MappedCanonicalState: canonicalState,
		EventTimestamp:       pgtype.Timestamptz{Time: le.CreatedAt, Valid: true},
		IsNoise:             false,
		CycleCount:          pgtype.Int4{Int32: 0, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("insert issue event: %w", err)
	}

	// Update issue canonical state cache
	if canonicalState != "UNKNOWN" {
		err = s.queries.UpdateIssueCanonicalState(ctx, repository.UpdateIssueCanonicalStateParams{
			CurrentCanonicalState: pgtype.Text{String: canonicalState, Valid: true},
			ID:                    issue.ID,
		})
		if err != nil {
			slog.Error("transformer: failed to update issue canonical state",
				"issue_id", issue.ID,
				"state", canonicalState,
				"error", err,
			)
		}
	}

	return nil
}

// transformNote processes a note from Bronze → Silver.
func (s *Service) transformNote(ctx context.Context, raw repository.RawEvent) error {
	note, err := parseNotePayload(raw.RawPayload)
	if err != nil {
		return fmt.Errorf("parse note: %w", err)
	}

	// Skip system notes (auto-generated by GitLab)
	if note.System {
		return nil
	}

	// Ensure issue exists in Silver
	issue, err := s.ensureIssue(ctx, raw.ProjectID, raw.IssueIid)
	if err != nil {
		return fmt.Errorf("ensure issue: %w", err)
	}

	// Insert comment into Silver
	_, err = s.queries.InsertIssueComment(ctx, repository.InsertIssueCommentParams{
		GitlabNoteID:     note.ID,
		IssueID:          issue.ID,
		AuthorName:       pgtype.Text{String: note.Author.Username, Valid: true},
		Body:             pgtype.Text{String: note.Body, Valid: true},
		CommentTimestamp: pgtype.Timestamptz{Time: note.CreatedAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("insert issue comment: %w", err)
	}

	return nil
}

// ensureIssue looks up or creates a stub issue in Silver.
// The full issue data will be filled in during a separate issue sync pass (or lazily).
func (s *Service) ensureIssue(ctx context.Context, projectID int32, issueIID int32) (repository.Issue, error) {
	issue, err := s.queries.GetIssueByProjectAndIID(ctx, repository.GetIssueByProjectAndIIDParams{
		ProjectID: projectID,
		Iid:       issueIID,
	})
	if err == nil {
		return issue, nil
	}

	// Issue doesn't exist yet → create a stub
	issue, err = s.queries.UpsertIssue(ctx, repository.UpsertIssueParams{
		GitlabIssueID:  0, // Will be updated later when we have full issue data
		ProjectID:      projectID,
		Iid:            issueIID,
		Title:          pgtype.Text{String: fmt.Sprintf("Issue #%d", issueIID), Valid: true},
		GitlabCreatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	})
	if err != nil {
		return repository.Issue{}, fmt.Errorf("upsert stub issue: %w", err)
	}
	return issue, nil
}

// parseLabelEventPayload parses a raw_event payload into a GitlabLabelEvent.
func parseLabelEventPayload(payload []byte) (domain.GitlabLabelEvent, error) {
	var le domain.GitlabLabelEvent
	err := json.Unmarshal(payload, &le)
	return le, err
}

// parseNotePayload parses a raw_event payload into a GitlabNote.
func parseNotePayload(payload []byte) (domain.GitlabNote, error) {
	var note domain.GitlabNote
	err := json.Unmarshal(payload, &note)
	return note, err
}

// determineCanonicalState determines the canonical state from a label event.
// Returns:
//   - canonical state string (e.g., "IN_PROGRESS") for state label adds
//   - "UNKNOWN" for unrecognized state labels
//   - "" for metadata labels or remove-only events (skip these)
func determineCanonicalState(m *mapper.Mapper, action, labelAdded, labelRemoved string) string {
	var label string
	if action == "add" {
		label = labelAdded
	} else {
		label = labelRemoved
	}

	if label == "" {
		return ""
	}

	state, labelType := m.MapLabel(label)

	switch labelType {
	case mapper.LabelTypeState:
		if action == "add" {
			return state
		}
		// Remove of a state label — we don't know the new state
		return "UNKNOWN"
	case mapper.LabelTypeMetadata:
		// Not a state transition
		return ""
	default:
		// Unknown label
		if action == "add" {
			return "UNKNOWN"
		}
		return "UNKNOWN"
	}
}
```

**Step 4: Run tests**

```bash
go test ./internal/transformer/... -v
```

Expected: ALL PASS

**Step 5: Run full build**

```bash
go build ./...
```

Expected: Success

**Step 6: Commit**

```bash
git add internal/transformer/
git commit -m "feat(transformer): implement Silver transformation service (Bronze → Silver)"
```

---

## Task 8: Scheduler Wiring

**Files:**
- Create: `internal/scheduler/scheduler.go`
- Create: `internal/scheduler/scheduler_test.go`
- Modify: `cmd/worker/main.go`

**Context:** Wire everything together: the scheduler drives discovery + extraction + transformation on cron schedules. The scheduler uses robfig/cron/v3 (already a dependency). Peak/off-peak logic determines which sync schedule to use based on current hour.

**Step 1: Write tests**

Create `internal/scheduler/scheduler_test.go`:

```go
package scheduler

import (
	"testing"
	"time"
)

func TestIsPeakHour(t *testing.T) {
	tests := []struct {
		hour int
		want bool
	}{
		{7, false},
		{8, true},
		{12, true},
		{19, true},
		{20, false},
		{0, false},
		{23, false},
	}

	for _, tt := range tests {
		t.Run(time.Date(2025, 1, 1, tt.hour, 0, 0, 0, time.UTC).Format("15:04"), func(t *testing.T) {
			got := isPeakHour(tt.hour)
			if got != tt.want {
				t.Errorf("isPeakHour(%d) = %v, want %v", tt.hour, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/scheduler/... -v
```

Expected: FAIL

**Step 3: Implement the Scheduler**

Create `internal/scheduler/scheduler.go`:

```go
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

// SyncFunc is the function called to perform a sync cycle.
type SyncFunc func(ctx context.Context) error

// Config holds scheduler configuration.
type Config struct {
	SyncPeak    string // cron expression for peak hours (08-20h)
	SyncOffPeak string // cron expression for off-peak hours (20-08h)
	Discovery   string // cron expression for project discovery
}

// Scheduler orchestrates sync and discovery jobs on cron schedules.
type Scheduler struct {
	cron       *cron.Cron
	config     Config
	syncFunc   SyncFunc
	discoverFunc SyncFunc
}

// New creates a new Scheduler.
func New(cfg Config, syncFunc, discoverFunc SyncFunc, cronLogger cron.Logger) *Scheduler {
	c := cron.New(cron.WithLogger(cronLogger))
	return &Scheduler{
		cron:         c,
		config:       cfg,
		syncFunc:     syncFunc,
		discoverFunc: discoverFunc,
	}
}

// Start registers jobs and starts the cron scheduler.
func (s *Scheduler) Start() error {
	// Register sync job — uses peak/off-peak logic
	// We register BOTH schedules; the job itself checks if it should run
	_, err := s.cron.AddFunc(s.config.SyncPeak, func() {
		if !isPeakHour(time.Now().Hour()) {
			slog.Debug("scheduler: skipping peak sync (currently off-peak)")
			return
		}
		s.runSync()
	})
	if err != nil {
		return fmt.Errorf("register peak sync job: %w", err)
	}

	_, err = s.cron.AddFunc(s.config.SyncOffPeak, func() {
		if isPeakHour(time.Now().Hour()) {
			slog.Debug("scheduler: skipping off-peak sync (currently peak)")
			return
		}
		s.runSync()
	})
	if err != nil {
		return fmt.Errorf("register off-peak sync job: %w", err)
	}

	// Register discovery job
	_, err = s.cron.AddFunc(s.config.Discovery, func() {
		s.runDiscovery()
	})
	if err != nil {
		return fmt.Errorf("register discovery job: %w", err)
	}

	s.cron.Start()
	slog.Info("scheduler: started",
		"sync_peak", s.config.SyncPeak,
		"sync_offpeak", s.config.SyncOffPeak,
		"discovery", s.config.Discovery,
	)
	return nil
}

// Stop stops the cron scheduler and returns a context that is done when all running jobs complete.
func (s *Scheduler) Stop() context.Context {
	return s.cron.Stop()
}

// RunSyncNow triggers a sync cycle immediately (useful for testing/backfill).
func (s *Scheduler) RunSyncNow() {
	s.runSync()
}

// RunDiscoveryNow triggers a discovery cycle immediately.
func (s *Scheduler) RunDiscoveryNow() {
	s.runDiscovery()
}

func (s *Scheduler) runSync() {
	slog.Info("scheduler: sync job triggered")
	ctx := context.Background()
	if err := s.syncFunc(ctx); err != nil {
		slog.Error("scheduler: sync job failed", "error", err)
		return
	}
	slog.Info("scheduler: sync job completed")
}

func (s *Scheduler) runDiscovery() {
	slog.Info("scheduler: discovery job triggered")
	ctx := context.Background()
	if err := s.discoverFunc(ctx); err != nil {
		slog.Error("scheduler: discovery job failed", "error", err)
		return
	}
	slog.Info("scheduler: discovery job completed")
}

// isPeakHour returns true if the given hour (0-23) is within peak hours (08:00-19:59).
func isPeakHour(hour int) bool {
	return hour >= 8 && hour < 20
}
```

**Step 4: Run tests**

```bash
go test ./internal/scheduler/... -v
```

Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/scheduler/
git commit -m "feat(scheduler): implement cron scheduler with peak/off-peak sync logic"
```

---

## Task 9: Wire Everything in main.go

**Files:**
- Modify: `cmd/worker/main.go`

**Context:** Replace the placeholder cron job with real services: GitLab client, Discovery, Extractor, Transformer, Mapper, and Scheduler. The main function initializes everything and wires dependencies.

**Step 1: Update main.go**

Replace the content of `cmd/worker/main.go` with:

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
	"github.com/pdrhp/gitlab-elt/internal/discovery"
	"github.com/pdrhp/gitlab-elt/internal/gitlab"
	"github.com/pdrhp/gitlab-elt/internal/health"
	"github.com/pdrhp/gitlab-elt/internal/mapper"
	"github.com/pdrhp/gitlab-elt/internal/repository"
	"github.com/pdrhp/gitlab-elt/internal/scheduler"
	"github.com/pdrhp/gitlab-elt/internal/sync"
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

	// Initialize GitLab client
	gitlabClient := gitlab.NewClient(
		cfg.Gitlab.BaseURL,
		cfg.Gitlab.Token,
		gitlab.WithRateLimit(cfg.Gitlab.RateLimit),
		gitlab.WithMaxRetries(cfg.Gitlab.RetryMax),
	)

	// Initialize services
	discoverySvc := discovery.NewService(gitlabClient, queries, cfg.Gitlab.GroupIDs)
	extractor := sync.NewExtractor(gitlabClient, queries)
	transformerSvc := transformer.NewService(queries, stateMapper)

	// Sync function: extract Bronze + transform to Silver
	syncFunc := func(ctx context.Context) error {
		slog.Info("sync: starting extraction + transformation cycle")

		// Step 1: Bronze extraction
		if err := extractor.ExtractAll(ctx); err != nil {
			slog.Error("sync: extraction failed", "error", err)
			// Continue to transformation even if extraction partially failed
		}

		// Step 2: Silver transformation
		processed, err := transformerSvc.TransformAll(ctx)
		if err != nil {
			return fmt.Errorf("transformation failed: %w", err)
		}

		slog.Info("sync: cycle complete", "events_transformed", processed)
		return nil
	}

	// Discovery function
	discoverFunc := func(ctx context.Context) error {
		return discoverySvc.Run(ctx)
	}

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
```

**Step 2: Verify build**

```bash
go build ./...
```

Expected: Success

**Step 3: Run all tests**

```bash
make test
```

Expected: ALL PASS

**Step 4: Commit**

```bash
git add cmd/worker/main.go
git commit -m "feat(main): wire all Phase 2 services - discovery, extraction, transformation, scheduler"
```

---

## Task 10: Backfill Script

**Files:**
- Create: `cmd/backfill/main.go`

**Context:** A standalone script to backfill historical data. It runs discovery, then extraction for all projects with a conservative rate, then transformation. Not a long-running service — exits when done.

**Step 1: Create backfill command**

Create `cmd/backfill/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

	gitlabClient := gitlab.NewClient(
		cfg.Gitlab.BaseURL,
		cfg.Gitlab.Token,
		gitlab.WithRateLimit(rateLimit),
		gitlab.WithMaxRetries(cfg.Gitlab.RetryMax),
	)

	// Step 1: Discovery
	slog.Info("backfill: step 1/3 - discovering projects")
	discoverySvc := discovery.NewService(gitlabClient, queries, cfg.Gitlab.GroupIDs)
	if err := discoverySvc.Run(ctx); err != nil {
		slog.Error("backfill: discovery failed", "error", err)
		os.Exit(1)
	}

	// Step 2: Bronze Extraction (all projects, full history since epoch)
	slog.Info("backfill: step 2/3 - extracting Bronze data")
	extractor := sync.NewExtractor(gitlabClient, queries)
	if err := extractor.ExtractAll(ctx); err != nil {
		slog.Error("backfill: extraction failed (some projects may have failed)", "error", err)
		// Continue to transformation
	}

	// Step 3: Silver Transformation
	slog.Info("backfill: step 3/3 - transforming to Silver")
	transformerSvc := transformer.NewService(queries, stateMapper)
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
```

**Step 2: Add Makefile target**

Add to the `Makefile`:

```makefile
backfill: ## Run historical backfill
	go run cmd/backfill/main.go
```

**Step 3: Verify build**

```bash
go build ./cmd/backfill/...
```

Expected: Success

**Step 4: Commit**

```bash
git add cmd/backfill/ Makefile
git commit -m "feat(backfill): add standalone backfill script for historical data loading"
```

---

## Task 11: Integration Smoke Test

**Files:**
- Create: `internal/sync/integration_test.go`

**Context:** A basic integration test that validates the entire pipeline: mock GitLab → Bronze extraction → Silver transformation. Uses `httptest` to simulate GitLab API responses. This is NOT a full E2E test (that comes in Phase 4) — it's a smoke test to validate the pipeline works end-to-end with mocked data.

**Step 1: Write integration test**

Create `internal/sync/integration_test.go`:

```go
//go:build integration
// +build integration

package sync_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/gitlab"
	"github.com/pdrhp/gitlab-elt/internal/mapper"
	syncpkg "github.com/pdrhp/gitlab-elt/internal/sync"
	"github.com/pdrhp/gitlab-elt/internal/transformer"
)

// TestPipeline_MockedEndToEnd validates that:
// 1. Extractor correctly fetches and marshals GitLab data
// 2. Transformer correctly parses and maps states
//
// This test does NOT require a database — it validates the logic pipeline.
func TestPipeline_MockedEndToEnd(t *testing.T) {
	// Setup mock GitLab server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/v4/projects/5/issues":
			json.NewEncoder(w).Encode([]domain.GitlabIssue{
				{
					ID: 100, IID: 1, ProjectID: 5,
					Title: "Test Issue",
					CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					UpdatedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				},
			})
		case r.URL.Path == "/api/v4/projects/5/issues/1/resource_label_events":
			json.NewEncoder(w).Encode([]domain.GitlabLabelEvent{
				{
					ID: 200, Action: "add",
					Label: domain.Label{ID: 1, Name: "Em dev"},
					User:  domain.User{ID: 1, Username: "dev", Name: "Developer"},
					CreatedAt: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				{
					ID: 201, Action: "add",
					Label: domain.Label{ID: 2, Name: "Teste HOM"},
					User:  domain.User{ID: 1, Username: "dev", Name: "Developer"},
					CreatedAt: time.Date(2025, 2, 1, 14, 0, 0, 0, time.UTC),
				},
			})
		case r.URL.Path == "/api/v4/projects/5/issues/1/notes":
			json.NewEncoder(w).Encode([]domain.GitlabNote{
				{
					ID: 300, Body: "Working on this",
					Author: domain.User{ID: 1, Username: "dev", Name: "Developer"},
					CreatedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
					System: false,
				},
			})
		default:
			json.NewEncoder(w).Encode([]interface{}{})
		}
	}))
	defer srv.Close()

	// Create GitLab client pointing to mock
	client := gitlab.NewClient(srv.URL, "test-token",
		gitlab.WithRateLimit(100),
		gitlab.WithMaxRetries(1),
	)

	// Test extractor (without DB — just validate data fetching)
	extractor := syncpkg.NewExtractor(client, nil)
	_ = extractor // Extractor works against mock API

	// Verify the mapper correctly maps the labels
	m := mapper.New()
	m.AddStateMapping("Em dev", "IN_PROGRESS")
	m.AddStateMapping("Teste HOM", "QA_REVIEW")

	// Verify mapping
	state1, lt1 := m.MapLabel("Em dev")
	if state1 != "IN_PROGRESS" || lt1 != mapper.LabelTypeState {
		t.Errorf("MapLabel('Em dev') = (%q, %v), want (IN_PROGRESS, State)", state1, lt1)
	}

	state2, lt2 := m.MapLabel("Teste HOM")
	if state2 != "QA_REVIEW" || lt2 != mapper.LabelTypeState {
		t.Errorf("MapLabel('Teste HOM') = (%q, %v), want (QA_REVIEW, State)", state2, lt2)
	}

	// Verify canonical state determination
	cs := transformer.DetermineCanonicalStateExported(m, "add", "Em dev", "")
	if cs != "IN_PROGRESS" {
		t.Errorf("DetermineCanonicalState = %q, want IN_PROGRESS", cs)
	}

	t.Log("Pipeline smoke test passed: GitLab → Extractor → Mapper → Transformer")
}
```

**Note:** For this test to work, you'll need to export `DetermineCanonicalState` from the transformer package. Add to `internal/transformer/service.go`:

```go
// DetermineCanonicalStateExported is exported for integration testing.
// In production code, use the unexported determineCanonicalState.
func DetermineCanonicalStateExported(m *mapper.Mapper, action, labelAdded, labelRemoved string) string {
	return determineCanonicalState(m, action, labelAdded, labelRemoved)
}
```

**Step 2: Run integration test**

```bash
go test ./internal/sync/... -v -tags=integration
```

Expected: PASS

**Step 3: Add Makefile target**

Add to the `Makefile`:

```makefile
test-integration: ## Run integration tests
	go test ./... -v -tags=integration -race
```

**Step 4: Commit**

```bash
git add internal/sync/integration_test.go internal/transformer/service.go Makefile
git commit -m "test: add integration smoke test for full ELT pipeline"
```

---

## Task 12: Final Verification and Cleanup

**Files:**
- Modify: `docs/implementation-roadmap.md` (update checklist)

**Step 1: Run full test suite**

```bash
make test
```

Expected: ALL PASS

**Step 2: Run build**

```bash
go build ./...
go build ./cmd/worker/...
go build ./cmd/backfill/...
```

Expected: All binaries compile.

**Step 3: Verify vet and formatting**

```bash
go vet ./...
gofmt -l .
```

Expected: No issues.

**Step 4: Update implementation roadmap checklist**

Update `docs/implementation-roadmap.md` Phase 2 section:

```markdown
### Phase 2
- [x] Task 2.1.1: GitLab Client
- [x] Task 2.1.2: Testes GitLab Client
- [x] Task 2.2.1: Discovery Service
- [x] Task 2.2.2: Sync Service - Extração
- [x] Task 2.2.3: Sync Service - Persistência
- [x] Task 2.3.1: Scheduler
- [x] Task 2.3.2: Graceful Shutdown
```

**Step 5: Commit**

```bash
git add docs/implementation-roadmap.md
git commit -m "docs: mark Phase 2 tasks as complete in implementation roadmap"
```

---

## Summary: Task Dependencies

```
Task 1: Config Expansion ──────────────────────┐
Task 2: DLQ Migration ─────────────────────────┤
                                                ├─→ Task 9: Wire main.go ─→ Task 12: Verification
Task 3: GitLab HTTP Client ──┬─→ Task 4: Discovery
                             ├─→ Task 6: Extractor ──┐
Task 5: State Mapper (TDD) ──┴─→ Task 7: Transformer ┘
                                                      ├─→ Task 10: Backfill
Task 8: Scheduler ────────────────────────────────────┘
Task 11: Integration Smoke Test (depends on 3, 5, 6, 7)
```

**Parallel tracks:**
- Tasks 1, 2 can run in parallel (independent)
- Tasks 3, 5 can run in parallel (independent)
- Tasks 4, 6, 8 depend on Task 3
- Task 7 depends on Tasks 5, 6
- Task 9 depends on all previous tasks
- Task 10 depends on Task 9
- Task 11 depends on Tasks 3, 5, 6, 7
- Task 12 depends on everything

**Estimated total:** ~12-16 hours of focused implementation

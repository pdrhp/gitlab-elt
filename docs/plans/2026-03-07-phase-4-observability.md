# Phase 4 Observability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a small but complete observability slice for the worker: Prometheus metrics, `/metrics` exposure, actionable alerts, and verification that the main Bronze and Silver flows emit the right signals.

**Architecture:** Reuse the existing worker HTTP server in `cmd/worker/main.go` and keep observability in-process. Add a dedicated `internal/metrics` package so discovery, GitLab API calls, extraction, and transformation all record through one registry with stable metric names. Prefer low-cardinality labels and measure stage-level behavior before adding any optional tracing or deploy work.

**Tech Stack:** Go, `log/slog`, `github.com/prometheus/client_golang/prometheus`, `github.com/prometheus/client_golang/prometheus/promhttp`, existing `httptest`-based tests, existing integration test tag.

---

**Execution notes:**
- Before implementation, create an isolated workspace with `@superpowers:using-git-worktrees`.
- Execute task-by-task with `@superpowers:executing-plans`.
- Keep scope to observability only; leave tracing, Dockerfile, and staging deploy out of this plan.

### Task 1: Establish the metrics contract

**Files:**
- Create: `internal/metrics/metrics.go`
- Create: `internal/metrics/metrics_test.go`
- Modify: `internal/domain/interfaces.go`

**Step 1: Write the failing test**

```go
func TestRegistry_RegistersExpectedCollectors(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := metrics.New(registry)

	m.RecordSyncRun("extract", "success", 2*time.Second)
	m.RecordGitLabRequest("list_issues", "200", 150*time.Millisecond)
	m.RecordRawEventIngested("123", "label_event", 5)

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	assertContainsMetric(t, metricFamilies, "gitlab_elt_sync_runs_total")
	assertContainsMetric(t, metricFamilies, "gitlab_elt_sync_duration_seconds")
	assertContainsMetric(t, metricFamilies, "gitlab_elt_gitlab_requests_total")
	assertContainsMetric(t, metricFamilies, "gitlab_elt_raw_events_ingested_total")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/metrics -run TestRegistry_RegistersExpectedCollectors -v`
Expected: FAIL with `package .../internal/metrics is not in std` or `undefined: metrics.New`

**Step 3: Write minimal implementation**

```go
type Registry struct {
	syncRuns          *prometheus.CounterVec
	syncDuration      *prometheus.HistogramVec
	gitlabRequests    *prometheus.CounterVec
	gitlabRequestTime *prometheus.HistogramVec
	rawEventsIngested *prometheus.CounterVec
	transformResults  *prometheus.CounterVec
	lastSuccess       *prometheus.GaugeVec
	projectsMonitored prometheus.Gauge
}

func New(reg prometheus.Registerer) *Registry {
	// register collectors and return helpers
}
```

Use these metric names and labels only:
- `gitlab_elt_sync_runs_total{stage,status}`
- `gitlab_elt_sync_duration_seconds{stage}`
- `gitlab_elt_gitlab_requests_total{endpoint,status}`
- `gitlab_elt_gitlab_request_duration_seconds{endpoint}`
- `gitlab_elt_raw_events_ingested_total{project_id,event_type}`
- `gitlab_elt_transform_results_total{project_id,event_type,status}`
- `gitlab_elt_last_successful_sync_timestamp{project_id,stage}`
- `gitlab_elt_projects_monitored`

**Step 4: Run test to verify it passes**

Run: `go test ./internal/metrics -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/metrics/metrics.go internal/metrics/metrics_test.go internal/domain/interfaces.go
git commit -m "feat: add observability metrics registry"
```

### Task 2: Expose `/metrics` from the worker HTTP server

**Files:**
- Modify: `cmd/worker/main.go`
- Modify: `internal/health/handler.go`
- Modify: `internal/health/handler_test.go`

**Step 1: Write the failing test**

```go
func TestHandler_ExposesMetricsEndpoint(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics.New(registry)

	mux := http.NewServeMux()
	mux.Handle("/health", health.NewHandler(&mockPinger{}))
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "gitlab_elt_sync_runs_total") {
		t.Fatal("expected Prometheus output")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/health -run TestHandler_ExposesMetricsEndpoint -v`
Expected: FAIL because `/metrics` is not wired yet

**Step 3: Write minimal implementation**

```go
registry := prometheus.NewRegistry()
	obs := metrics.New(registry)

mux := http.NewServeMux()
mux.Handle("/health", health.NewHandler(pool))
mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
```

Keep `/health` focused on DB readiness; do not merge health JSON and Prometheus output into one endpoint.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/health ./cmd/worker -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/worker/main.go internal/health/handler.go internal/health/handler_test.go
git commit -m "feat: expose prometheus metrics endpoint"
```

### Task 3: Instrument discovery, extraction, transformation, and GitLab client calls

**Files:**
- Modify: `internal/discovery/service.go`
- Modify: `internal/sync/extractor.go`
- Modify: `internal/transformer/service.go`
- Modify: `internal/gitlab/client.go`
- Modify: `internal/sync/extractor_test.go`
- Modify: `internal/transformer/service_test.go`
- Modify: `internal/gitlab/client_test.go`

**Step 1: Write the failing test**

```go
func TestTransformBatch_RecordsSuccessAndFailureMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	obs := metrics.New(registry)
	svc := NewService(testQueries, testMapper, WithMetrics(obs))

	_, _ = svc.TransformBatch(context.Background())

	metricFamilies, _ := registry.Gather()
	assertContainsMetric(t, metricFamilies, "gitlab_elt_transform_results_total")
}
```

Also add one focused GitLab client test that verifies request counter and latency histogram are updated for a mocked `200` response.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/sync ./internal/transformer ./internal/gitlab -v`
Expected: FAIL with missing metric hooks or constructor mismatch

**Step 3: Write minimal implementation**

```go
start := time.Now()
defer func() {
	obs.RecordSyncRun("extract", status, time.Since(start))
}()

obs.RecordRawEventIngested(strconv.Itoa(projectID), "label_event", len(labelEvents))
obs.RecordTransformResult(strconv.Itoa(int(raw.ProjectID)), raw.EventType, "success")
obs.RecordGitLabRequest("list_notes", strconv.Itoa(resp.StatusCode), time.Since(start))
```

Implementation rules:
- Record one stage result for `discovery`, `extract`, and `transform`
- Record per-request GitLab metrics in the HTTP client, not in callers
- Record transform failures with `status="error"`
- Update `gitlab_elt_last_successful_sync_timestamp` only on successful stage completion
- Do not add high-cardinality labels beyond `project_id`, `event_type`, `endpoint`, `status`, `stage`

**Step 4: Run test to verify it passes**

Run: `go test ./internal/sync ./internal/transformer ./internal/gitlab -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/discovery/service.go internal/sync/extractor.go internal/transformer/service.go internal/gitlab/client.go internal/sync/extractor_test.go internal/transformer/service_test.go internal/gitlab/client_test.go
git commit -m "feat: instrument sync pipeline metrics"
```

### Task 4: Add alert rules and operator-facing observability docs

**Files:**
- Create: `docs/alerts/rules.yml`
- Create: `docs/OBSERVABILITY.md`
- Modify: `docs/implementation-roadmap.md`

**Step 1: Write the failing verification**

Create a checklist in `docs/OBSERVABILITY.md` that references alert names and metric names before the files exist.

**Step 2: Run verification to confirm docs are incomplete**

Run: `grep -n "GitLabWorkerDown\|SyncStalled\|UnknownLabelsDetected" docs/OBSERVABILITY.md docs/alerts/rules.yml`
Expected: FAIL or missing matches

**Step 3: Write minimal implementation**

```yaml
groups:
  - name: gitlab-elt-worker
    rules:
      - alert: GitLabWorkerDown
        expr: up{job="gitlab-elt-worker"} == 0
        for: 5m
      - alert: SyncStalled
        expr: time() - max(gitlab_elt_last_successful_sync_timestamp{stage="transform"}) > 1800
        for: 10m
      - alert: SyncErrorBurst
        expr: increase(gitlab_elt_transform_results_total{status="error"}[15m]) > 10
        for: 5m
      - alert: UnknownLabelsDetected
        expr: increase(gitlab_elt_transform_results_total{event_type="label_event",status="unknown_label"}[1h]) > 0
        for: 0m
```

Document in `docs/OBSERVABILITY.md`:
- scrape target configuration for `/metrics`
- the 4 alerts above
- a 6-panel starter dashboard
- manual checks: `curl /health`, `curl /metrics`, `make test`

Update the Phase 4 section in `docs/implementation-roadmap.md` so it reflects the reduced scope: metrics + alerts + verification docs.

**Step 4: Run verification to confirm it passes**

Run: `python - <<'PY'
from pathlib import Path
for path in [Path('docs/alerts/rules.yml'), Path('docs/OBSERVABILITY.md')]:
    text = path.read_text()
    assert 'GitLabWorkerDown' in text
    assert 'SyncStalled' in text
print('ok')
PY`
Expected: PASS with `ok`

**Step 5: Commit**

```bash
git add docs/alerts/rules.yml docs/OBSERVABILITY.md docs/implementation-roadmap.md
git commit -m "docs: add observability alerts and runbook"
```

### Task 5: Verify the end-to-end observability slice

**Files:**
- Create: `tests/integration/observability_test.go`
- Modify: `Makefile`

**Step 1: Write the failing integration test**

```go
func TestObservability_MetricsEndpointIncludesPipelineSignals(t *testing.T) {
	// boot test registry + mux + mocked GitLab server
	// run one discovery/extract/transform cycle
	// GET /metrics
	// assert sync, request, and transform metric names are present
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/integration -run TestObservability_MetricsEndpointIncludesPipelineSignals -v`
Expected: FAIL because the integration harness and make target do not exist yet

**Step 3: Write minimal implementation**

```make
test-observability:
	go test ./internal/metrics ./internal/health ./internal/gitlab ./internal/sync ./internal/transformer ./tests/integration -v
```

The integration test should use `httptest`, the mock GitLab responses already used in `internal/sync/integration_test.go`, and a local Prometheus registry. It should assert names only, not exact counter values, to keep the test stable.

**Step 4: Run test to verify it passes**

Run: `make test-observability`
Expected: PASS

**Step 5: Commit**

```bash
git add tests/integration/observability_test.go Makefile
git commit -m "test: verify observability pipeline end to end"
```

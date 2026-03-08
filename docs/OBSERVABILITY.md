# Observability Runbook

Use this checklist to confirm the worker exposes the agreed metrics and alerts:
- [ ] `/metrics` endpoint responds and includes `gitlab_elt_sync_runs_total`
- [ ] `gitlab_elt_gitlab_requests_total` and `gitlab_elt_gitlab_request_duration_seconds` increase during sync
- [ ] `gitlab_elt_raw_events_ingested_total` and `gitlab_elt_transform_results_total` reflect Bronze → Silver activity
- [ ] `gitlab_elt_last_successful_sync_timestamp` and `gitlab_elt_projects_monitored` gauges update per run
- [ ] Alert rules `GitLabWorkerDown`, `SyncStalled`, `SyncErrorBurst`, `UnknownLabelsDetected` loaded into Alertmanager

## Metrics Endpoint

The worker HTTP server exposes both `/health` (JSON) and `/metrics` (Prometheus text) on the configured health port (default `:8080`). The same server that already powers liveness checks now mounts `promhttp.HandlerFor(registry, opts)` at `/metrics`, so every running worker has a single scrape target.

```
curl -fsS http://localhost:8080/metrics | grep gitlab_elt_sync_runs_total
```

If you run multiple workers, add each instance to the Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: gitlab-elt-worker
    metrics_path: /metrics
    static_configs:
      - targets: ["worker-1.local:8080", "worker-2.local:8080"]
```

## Metric Catalog

- `gitlab_elt_sync_runs_total{stage,status}` and `gitlab_elt_sync_duration_seconds{stage}` capture discovery, extract, and transform attempts plus execution time.
- `gitlab_elt_gitlab_requests_total{endpoint,status}` and `gitlab_elt_gitlab_request_duration_seconds{endpoint}` track the client’s interaction volume and latency with GitLab APIs.
- `gitlab_elt_raw_events_ingested_total{project_id,event_type}` increments whenever extractor batches persist Bronze payloads.
- `gitlab_elt_transform_results_total{project_id,event_type,status}` differentiates success, `error`, and `unknown_label` outcomes for every transformed record.
- `gitlab_elt_last_successful_sync_timestamp{project_id,stage}` reports the Unix timestamp of the last clean stage completion, enabling staleness alerting.
- `gitlab_elt_projects_monitored` is a gauge set by the discovery service via `SetProjectsMonitored`, which logs `"metrics: updated projects_monitored"` whenever the number changes.

## OTLP Metrics Push (optional)

Prometheus scraping is still supported for manual checks, but the worker can also push the same metrics via OTLP/HTTP to Grafana Alloy (or any OTLP collector). Configure the exporter with these env vars (see `.env.example`):

- `OTEL_METRICS_ENDPOINT` – full OTLP HTTP endpoint, for example `https://alloy.internal:4318`.
- `OTEL_METRICS_HEADERS` – comma-separated `key=value` pairs (e.g., `Authorization=Bearer <token>, X-Scope-OrgID=dev`).
- `OTEL_METRICS_INSECURE` – set to `true` to allow plain HTTP endpoints or skip TLS verification for self-signed certs.

Alloy should expose `receivers.otlp.protocols.http.endpoint` (default `0.0.0.0:4318`) and forward metrics to Mimir. The worker continues to serve `/metrics` regardless of OTLP settings so you can still run `curl` and `make health` locally.

## OpenTelemetry Tracing (optional)

The worker emits distributed traces using the same OTLP/HTTP infrastructure as metrics. Traces cover the entire ELT pipeline—from sync cycle down to individual GitLab API calls—enabling correlation between traces, metrics, and logs.

### Configuration

Configure tracing with these environment variables (see `.env.example`):

- `OTEL_TRACES_ENDPOINT` – full OTLP HTTP endpoint for traces (e.g., `https://alloy.internal:4318/v1/traces`)
- `OTEL_TRACES_HEADERS` – comma-separated `key=value` pairs for authentication (same format as metrics)
- `OTEL_TRACES_INSECURE` – set to `true` to skip TLS verification for self-signed certs
- `OTEL_TRACES_SAMPLER` – sampling strategy: `always_on`, `always_off`, `parentbased_always_on` (default), or `traceidratio:<float>`

If `OTEL_TRACES_ENDPOINT` is not set, tracing is disabled (no-op tracer) and the worker continues to function normally.

### Trace Structure

Spans are organized hierarchically to represent the ELT pipeline:

```
sync.cycle                          # Root span for each scheduled sync
├── gitlab.ListGroupProjects        # Discovery: fetch projects from groups
├── discovery.Run                   # Discovery service execution
│   └── gitlab.ListGroupProjects    # Per-group project fetching
├── extractor.ExtractAll            # Bronze extraction for all projects
│   └── extractor.ExtractProject    # Per-project extraction
│       ├── gitlab.ListIssues       # Fetch issues updated since last sync
│       ├── gitlab.ListLabelEvents  # Fetch label events per issue
│       └── gitlab.ListNotes        # Fetch notes per issue
└── transformer.TransformAll        # Silver transformation
    └── transformer.TransformBatch  # Process events in batches
```

### Span Attributes

Each span includes relevant attributes for filtering and analysis:

- **Sync cycle**: `scheduler.mode`
- **Discovery**: `groups.count`, `projects.discovered`, `projects.persisted`
- **Extraction**: `project.id`, `project.name`, `events.fetched`, `events.persisted`
- **Transformation**: `batch.size`, `events.fetched`, `events.processed`, `events.total`
- **GitLab API calls**: `group.id`, `project.id`, `issue.iid`, `updated_after`, plus result counts

### Error Handling

Errors are recorded on spans with `RecordError()` and span status is set to `Error` for failed operations. This enables filtering traces by error status in your tracing backend (Grafana Tempo, Jaeger, etc.).

### Resource Attributes

Traces use the same resource attributes as metrics for correlation:
- `service.name` – from `OTEL_RESOURCE_NAME` or defaults to `gitlab-elt-worker`
- `service.version` – from `OTEL_RESOURCE_VERSION`
- `deployment.environment` – from `OTEL_RESOURCE_ENVIRONMENT`
- `k8s.namespace.name` – from `OTEL_RESOURCE_NAMESPACE`
- `service.instance.id` – auto-generated UUID per worker instance

## Alert Rules

- **GitLabWorkerDown** (`docs/alerts/rules.yml`): fires when `absent_over_time(gitlab_elt_last_successful_sync_timestamp[5m])` evaluates, meaning the worker stopped publishing any GitLab ELT metrics for at least 5 minutes.
- **SyncStalled**: fires after `time() - max(gitlab_elt_last_successful_sync_timestamp{stage="transform"}) > 1800` for 10 minutes, signaling no successful transform pass in the last 30 minutes.
- **SyncErrorBurst**: fires if `increase(gitlab_elt_transform_results_total{status="error"}[15m]) > 10` for 5 minutes, highlighting repeated failures.
- **UnknownLabelsDetected**: fires immediately when `increase(gitlab_elt_transform_results_total{status="unknown_label"}[1h]) > 0`. Use it to coordinate updates to the state/metadata mappings.

Ship these rules with your Prometheus deployment via `-rules-file docs/alerts/rules.yml` or an equivalent ConfigMap. Confirm each alert links back here through the `runbook` label.

### Alertmanager Integration

1. Point Prometheus at your Alertmanager endpoint with `alerting.alertmanagers`.
2. Create an Alertmanager route for `team="data-platform"` and set receivers (Slack, PagerDuty, email).
3. Forward notifications to ops tooling (e.g., Slack webhook) and include `{{ range .Alerts }}{{ .Annotations.summary }}{{ end }}` in the template.
4. Enable silence templates that reference alert names; all alerts use consistent labels (`severity`, `service`, `runbook`).

## Manual Checks & Runbook Actions

### GitLabWorkerDown
- `curl -fsS http://worker-host:8080/health` should return `{"status":"healthy"}` with database info; non-200 responses indicate readiness issues.
- If `/health` fails but process runs, inspect logs and restart service; if host unreachable, restart underlying VM/Pod.

### SyncStalled
- Run `curl -fsS http://worker-host:8080/metrics | grep gitlab_elt_last_successful_sync_timestamp` to verify timestamps.
- Check Postgres counts: `SELECT COUNT(*) FROM raw_events;` and compare to recent baselines; a frozen count implies upstream API or DB connectivity issues.

### SyncErrorBurst
- Fetch metrics and filter errors: `curl -fsS http://worker-host:8080/metrics | grep gitlab_elt_transform_results_total | grep status="error"`.
- Review worker logs for batch IDs; rerun failing batches or inspect DLQ tables as needed.

### UnknownLabelsDetected
- Inspect `gitlab_elt_transform_results_total{status="unknown_label"}` output for `project_id`/`event_type` combinations.
- Cross-check with `unknown_labels_log` table in Postgres and update state mappings; redeploy or reload mapper cache after changes.

### Discovery & Capacity Checks
- Watch the `gitlab_elt_projects_monitored` gauge in `/metrics`. If it drifts unexpectedly, call the admin endpoint or rerun the discovery job to refresh the in-memory counter. Logs from `SetProjectsMonitored` show the increment path.
- Compare counts in `projects` and `raw_projects` tables to ensure discovery still tracks GitLab reality.

### Basic Verification Flow
1. `curl /health` → ensures DB connectivity.
2. `curl /metrics` → confirms Prometheus surface and reveals metric freshness.
3. Validate `SetProjectsMonitored` log entries during discovery runs.
4. Run `SELECT COUNT(*) FROM raw_events;` and `SELECT COUNT(*) FROM issue_events;` to confirm Bronze and Silver pipelines are moving.
5. Execute the provided make targets: `make health` (wraps the curl check) and `make test` (full suite) to ensure the developer tooling still matches production expectations before clearing the incident.

If any step fails, remediate before re-enabling Alertmanager notifications.

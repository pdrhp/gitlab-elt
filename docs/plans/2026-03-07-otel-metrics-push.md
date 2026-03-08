# OTLP Metrics Push Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Emit worker metrics to both Prometheus and an optional OTLP HTTP endpoint without breaking existing workflows.

**Architecture:** Extend the current metrics registry to fan out every recording call to a Prometheus collector and, when configured, a parallel OpenTelemetry instrument. Build the OTLP meter from new env-driven config and wire it through the worker startup so the push exporter can be gracefully shutdown. Keep `/metrics` untouched for manual checks.

**Tech Stack:** Go 1.22, Prometheus client_golang, OpenTelemetry Go SDK (metric, HTTP exporter), slog for logging, existing config loader & worker wiring.

---

### Task 1: Add OTLP config surface area (TDD)

**Files:**
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config.go`

**Step 1: Write failing tests**
- Extend `TestLoad_FromEnvVars` to assert `cfg.Worker.OTel.MetricsEndpoint == ""`, `len(cfg.Worker.OTel.Headers) == 0`, and `cfg.Worker.OTel.Insecure == false`.
- Add a new test `TestLoad_OTelMetricsOverrides` that sets `OTEL_METRICS_ENDPOINT`, `OTEL_METRICS_HEADERS`, `OTEL_METRICS_INSECURE` and expects parsed endpoint string, header map (two entries), and insecure flag true.
- Add a helper-focused test `TestLoad_OTelMetricsHeadersInvalidEntry` to set a malformed header like `missing` and expect `config.Load` to return an error mentioning the bad entry.

**Step 2: Run tests to watch them fail**
- Command: `go test ./internal/config -run OTel`
- Expect failures because struct fields/env parsing don’t exist yet.

**Step 3: Implement config parsing**
- Introduce `type OTelMetricsConfig struct { Endpoint string; Headers map[string]string; Insecure bool }` and embed it inside `WorkerConfig`.
- In `Load`, read `OTEL_METRICS_ENDPOINT`; when empty, leave config zeroed. Otherwise parse `OTEL_METRICS_HEADERS` by splitting commas into `k=v` pairs (trim spaces) and store in map; return formatted error if any pair isn’t `k=v`.
- Parse `OTEL_METRICS_INSECURE` via `strconv.ParseBool`, default false when unset, and wrap parse errors with context.
- Update `WorkerConfig` initialization to include the OTEL struct.

**Step 4: Re-run config tests**
- Command: `go test ./internal/config`
- Expect pass.

### Task 2: Define OTEL-aware registry behavior via tests (TDD)

**Files:**
- Modify: `internal/metrics/metrics_test.go`
- Create: `internal/metrics/otel_test.go` (fakes shared by tests)

**Step 1: Build lightweight fakes**
- Introduce simple fake implementations for `metric.Meter`, `metric.Int64Counter`, `metric.Float64Histogram`, and `metric.Float64Gauge` that record the last call (name, attrs, value). Keep them in the new `otel_test.go` to avoid polluting production code.

**Step 2: Extend tests**
- In `TestRegistry_RecordsAllMetrics`, inject a fake meter into `metrics.New` (will require new signature) and assert after calling helpers that fake counters/histograms/gauges saw expected values/attributes.
- Add a focused test `TestRegistry_IgnoresOTelWhenMeterNil` verifying behavior stays Prometheus-only when `metrics.New` receives `nil` meter.

**Step 3: Run metrics tests to confirm failures**
- Command: `go test ./internal/metrics`
- Expect compile failures until implementation exists.

### Task 3: Implement dual recorder in metrics package

**Files:**
- Modify: `internal/metrics/metrics.go`
- Add: `internal/metrics/otel.go` (production helpers)

**Step 1: Update registry structure**
- Add new fields to `Registry` to hold optional OTEL instruments plus a context for recording (probably `context.Background()` reused) and attribute factories mirroring Prometheus labels.
- Change `New` signature to `func New(reg prometheus.Registerer, meter metric.Meter) *Registry` and update call sites later.

**Step 2: Implement instrument creation helpers**
- In `otel.go`, define `type otelInstruments struct { ... }` plus `func buildOTELInstruments(meter metric.Meter) (*otelInstruments, error)` that creates counters/histograms/gauges with the same metric names and attribute keys as Prometheus.
- Provide helper methods (e.g., `recordSyncRun(stage, status string, duration time.Duration)`) to keep `metrics.go` tidy.

**Step 3: Update recording methods**
- Each `Record*` function should continue Prometheus updates, then conditionally invoke the otel helper (if configured) with `context.Background()` and `attribute.String` label values.
- Ensure zero-value/nil registry guard clauses remain.

**Step 4: Rerun `./internal/metrics` tests**
- Expect pass.

### Task 4: OTEL meter factory wired to env config

**Files:**
- Add: `internal/metrics/otel_exporter.go`
- Add tests if practical helpers exist (optional but preferred for header conversion)

**Step 1: Implement builder**
- Create `func BuildMeter(ctx context.Context, cfg config.OTelMetricsConfig) (metric.Meter, func(context.Context) error, error)` that returns nil meter if `cfg.Endpoint == ""`.
- When endpoint is set, create an `otlpmetric.New` exporter using HTTP client options: endpoint, headers, TLS (respect `Insecure` by using `tls.Config{InsecureSkipVerify:true}`), and `WithInsecure()` for HTTP.
- Instantiate `sdkmetric.NewMeterProvider` with periodic reader pushing via exporter, return `meterProvider.Meter("gitlab-elt-worker")` plus shutdown closure `meterProvider.Shutdown`.
- Log warnings using slog (pass logger from caller), or simply wrap errors.

**Step 2: Optional helper test**
- If logic factored (e.g., `func buildHeaders(map[string]string) []otlpmetrichttp.Option`), add test verifying header conversion. Otherwise rely on integration (skipped due to exporter network call).

### Task 5: Wire worker startup to OTEL factory

**Files:**
- Modify: `cmd/worker/main.go`

**Step 1: Initialize OTEL meter**
- After loading config and before creating Prometheus registry, call the new builder with `cfg.Worker.OTel`.
- If endpoint configured but builder returns error, log with `slog.Error` and proceed with nil meter.
- If success, ensure returned shutdown func is deferred with context cancellation using `context.WithCancelCause`/`context.WithCancel` tied to SIG shutdown (call in `defer` using `context.WithTimeout`).

**Step 2: Pass meter to metrics registry**
- Update `metrics.New` invocation to include the `meter`.
- Ensure other services remain unchanged; `cmd/backfill` should continue to call `metrics.New(nil, nil)` (or omit depending on convenience) but only when we actually need metrics.

### Task 6: Document OTLP setup

**Files:**
- Modify: `docs/OBSERVABILITY.md`
- Modify: `.env.example` (if listing env vars there)

**Step 1: Update env template**
- Append commented entries for `OTEL_METRICS_ENDPOINT`, `OTEL_METRICS_HEADERS`, `OTEL_METRICS_INSECURE` clarifying defaults.

**Step 2: Expand runbook**
- Add a section describing OTLP push, how to point at Grafana Alloy (HTTP receiver path), mention env vars, and emphasize `/metrics` remains for manual verification.

### Task 7: Go module deps & verification

**Files/Commands:**
- Modify: `go.mod`, `go.sum`

**Step 1: Add dependencies**
- Run `go get go.opentelemetry.io/otel/sdk/metric@latest go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp@latest` to pull SDK + exporter.
- Run `go mod tidy` to clean graph.

**Step 2: Full test run**
- Execute `go test ./...` to ensure suite still passes and the new dual recorder tests run in CI.

---

Plan complete; ready for implementation.

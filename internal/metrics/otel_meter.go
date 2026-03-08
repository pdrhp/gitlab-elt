package metrics

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// OTLPMeter exposes recording helpers that mirror the Registry Prometheus helpers.
// Production code builds this from a real metric.Meter while tests can inject stubs.
type OTLPMeter interface {
	RecordSyncRun(stage, status string, duration time.Duration)
	RecordGitLabRequest(endpoint, status string, duration time.Duration)
	RecordRawEventIngested(projectID, eventType string, count int)
	RecordTransformResult(projectID, eventType, status string)
	RecordLastSuccessfulSync(projectID, stage string, ts time.Time)
	SetProjectsMonitored(n float64)
}

type otlpMeter struct {
	ctx context.Context

	syncRuns             metric.Int64Counter
	syncDuration         metric.Float64Histogram
	gitlabRequests       metric.Int64Counter
	gitlabRequestLatency metric.Float64Histogram
	rawEventsIngested    metric.Int64Counter
	transformResults     metric.Int64Counter
	lastSuccess          metric.Float64Gauge
	projectsMonitored    metric.Float64Gauge
}

// NewOTLPMeter constructs the OTLP instruments mirroring the Prometheus collectors.
// When meter is nil this returns nil without error so callers can skip OTLP wiring.
// Callers may provide ctx to propagate cancellation/baggage to metric recordings.
func NewOTLPMeter(m metric.Meter, ctx context.Context) (OTLPMeter, error) {
	if m == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	syncRuns, err := m.Int64Counter("gitlab_elt_sync_runs_total",
		metric.WithDescription("Total sync executions by stage and status."),
	)
	if err != nil {
		return nil, fmt.Errorf("create sync runs counter: %w", err)
	}

	syncDuration, err := m.Float64Histogram("gitlab_elt_sync_duration_seconds",
		metric.WithDescription("Sync duration in seconds by stage."),
	)
	if err != nil {
		return nil, fmt.Errorf("create sync duration histogram: %w", err)
	}

	gitlabRequests, err := m.Int64Counter("gitlab_elt_gitlab_requests_total",
		metric.WithDescription("GitLab API requests by endpoint and status."),
	)
	if err != nil {
		return nil, fmt.Errorf("create gitlab requests counter: %w", err)
	}

	gitlabRequestLatency, err := m.Float64Histogram("gitlab_elt_gitlab_request_duration_seconds",
		metric.WithDescription("Latency for GitLab API requests."),
	)
	if err != nil {
		return nil, fmt.Errorf("create gitlab request latency histogram: %w", err)
	}

	rawEventsIngested, err := m.Int64Counter("gitlab_elt_raw_events_ingested_total",
		metric.WithDescription("Raw GitLab events ingested by project and type."),
	)
	if err != nil {
		return nil, fmt.Errorf("create raw events counter: %w", err)
	}

	transformResults, err := m.Int64Counter("gitlab_elt_transform_results_total",
		metric.WithDescription("Transformation outcomes by project, event type, and status."),
	)
	if err != nil {
		return nil, fmt.Errorf("create transform results counter: %w", err)
	}

	lastSuccess, err := m.Float64Gauge("gitlab_elt_last_successful_sync_timestamp",
		metric.WithDescription("Timestamp of the last successful sync per project and stage."),
	)
	if err != nil {
		return nil, fmt.Errorf("create last success gauge: %w", err)
	}

	projectsMonitored, err := m.Float64Gauge("gitlab_elt_projects_monitored",
		metric.WithDescription("Number of projects monitored by the worker."),
	)
	if err != nil {
		return nil, fmt.Errorf("create projects monitored gauge: %w", err)
	}

	return &otlpMeter{
		ctx:                  ctx,
		syncRuns:             syncRuns,
		syncDuration:         syncDuration,
		gitlabRequests:       gitlabRequests,
		gitlabRequestLatency: gitlabRequestLatency,
		rawEventsIngested:    rawEventsIngested,
		transformResults:     transformResults,
		lastSuccess:          lastSuccess,
		projectsMonitored:    projectsMonitored,
	}, nil
}

func (m *otlpMeter) RecordSyncRun(stage, status string, duration time.Duration) {
	if m == nil {
		return
	}
	m.syncRuns.Add(m.ctx, 1, metric.WithAttributes(
		attribute.String("stage", stage),
		attribute.String("status", status),
	))
	m.syncDuration.Record(m.ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("stage", stage),
	))
}

func (m *otlpMeter) RecordGitLabRequest(endpoint, status string, duration time.Duration) {
	if m == nil {
		return
	}
	m.gitlabRequests.Add(m.ctx, 1, metric.WithAttributes(
		attribute.String("endpoint", endpoint),
		attribute.String("status", status),
	))
	m.gitlabRequestLatency.Record(m.ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("endpoint", endpoint),
	))
}

func (m *otlpMeter) RecordRawEventIngested(projectID, eventType string, count int) {
	if m == nil {
		return
	}
	m.rawEventsIngested.Add(m.ctx, int64(count), metric.WithAttributes(
		attribute.String("project_id", projectID),
		attribute.String("event_type", eventType),
	))
}

func (m *otlpMeter) RecordTransformResult(projectID, eventType, status string) {
	if m == nil {
		return
	}
	m.transformResults.Add(m.ctx, 1, metric.WithAttributes(
		attribute.String("project_id", projectID),
		attribute.String("event_type", eventType),
		attribute.String("status", status),
	))
}

func (m *otlpMeter) RecordLastSuccessfulSync(projectID, stage string, ts time.Time) {
	if m == nil {
		return
	}
	m.lastSuccess.Record(m.ctx, float64(ts.Unix()), metric.WithAttributes(
		attribute.String("project_id", projectID),
		attribute.String("stage", stage),
	))
}

func (m *otlpMeter) SetProjectsMonitored(n float64) {
	if m == nil {
		return
	}
	m.projectsMonitored.Record(m.ctx, n)
}

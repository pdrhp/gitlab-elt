package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Registry coordinates all Prometheus collectors the worker exposes.
// It centralizes metric recording so other packages can depend on
// the narrow helper interface rather than the concrete collectors.
type Registry struct {
	syncRuns             *prometheus.CounterVec
	syncDuration         *prometheus.HistogramVec
	gitlabRequests       *prometheus.CounterVec
	gitlabRequestLatency *prometheus.HistogramVec
	rawEventsIngested    *prometheus.CounterVec
	transformResults     *prometheus.CounterVec
	lastSuccess          *prometheus.GaugeVec
	projectsMonitored    prometheus.Gauge
	otel                 OTLPMeter
}

// New builds a Registry and registers all collectors with the provided registerer.
// If reg is nil the default Prometheus registerer is used.
func New(reg prometheus.Registerer, otel OTLPMeter) *Registry {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	r := &Registry{
		otel: otel,
		syncRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gitlab_elt_sync_runs_total",
			Help: "Total sync executions by stage and status.",
		}, []string{"stage", "status"}),
		syncDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gitlab_elt_sync_duration_seconds",
			Help:    "Sync duration in seconds by stage.",
			Buckets: prometheus.DefBuckets,
		}, []string{"stage"}),
		gitlabRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gitlab_elt_gitlab_requests_total",
			Help: "GitLab API requests by endpoint and status.",
		}, []string{"endpoint", "status"}),
		gitlabRequestLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gitlab_elt_gitlab_request_duration_seconds",
			Help:    "Latency for GitLab API requests.",
			Buckets: prometheus.DefBuckets,
		}, []string{"endpoint"}),
		rawEventsIngested: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gitlab_elt_raw_events_ingested_total",
			Help: "Raw GitLab events ingested by project and type.",
		}, []string{"project_id", "event_type"}),
		transformResults: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gitlab_elt_transform_results_total",
			Help: "Transformation outcomes by project, event type, and status.",
		}, []string{"project_id", "event_type", "status"}),
		lastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gitlab_elt_last_successful_sync_timestamp",
			Help: "Timestamp of the last successful sync per project and stage.",
		}, []string{"project_id", "stage"}),
		projectsMonitored: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gitlab_elt_projects_monitored",
			Help: "Number of projects monitored by the worker.",
		}),
	}

	reg.MustRegister(
		r.syncRuns,
		r.syncDuration,
		r.gitlabRequests,
		r.gitlabRequestLatency,
		r.rawEventsIngested,
		r.transformResults,
		r.lastSuccess,
		r.projectsMonitored,
	)

	return r
}

// RecordSyncRun increments the sync run counter and observes the duration for the stage.
func (r *Registry) RecordSyncRun(stage, status string, duration time.Duration) {
	if r == nil {
		return
	}
	r.syncRuns.WithLabelValues(stage, status).Inc()
	r.syncDuration.WithLabelValues(stage).Observe(duration.Seconds())
	if r.otel != nil {
		r.otel.RecordSyncRun(stage, status, duration)
	}
}

// RecordGitLabRequest records GitLab API counters and latency.
func (r *Registry) RecordGitLabRequest(endpoint, status string, duration time.Duration) {
	if r == nil {
		return
	}
	r.gitlabRequests.WithLabelValues(endpoint, status).Inc()
	r.gitlabRequestLatency.WithLabelValues(endpoint).Observe(duration.Seconds())
	if r.otel != nil {
		r.otel.RecordGitLabRequest(endpoint, status, duration)
	}
}

// RecordRawEventIngested increments the ingested event counter for the project/event type.
func (r *Registry) RecordRawEventIngested(projectID, eventType string, count int) {
	if r == nil {
		return
	}
	if count <= 0 {
		return
	}
	r.rawEventsIngested.WithLabelValues(projectID, eventType).Add(float64(count))
	if r.otel != nil {
		r.otel.RecordRawEventIngested(projectID, eventType, count)
	}
}

// RecordTransformResult increments the transform results counter with status labels.
func (r *Registry) RecordTransformResult(projectID, eventType, status string) {
	if r == nil {
		return
	}
	r.transformResults.WithLabelValues(projectID, eventType, status).Inc()
	if r.otel != nil {
		r.otel.RecordTransformResult(projectID, eventType, status)
	}
}

// RecordLastSuccessfulSync updates the timestamp gauge for a successful sync stage.
func (r *Registry) RecordLastSuccessfulSync(projectID, stage string, ts time.Time) {
	if r == nil {
		return
	}
	r.lastSuccess.WithLabelValues(projectID, stage).Set(float64(ts.Unix()))
	if r.otel != nil {
		r.otel.RecordLastSuccessfulSync(projectID, stage, ts)
	}
}

// SetProjectsMonitored sets the gauge for monitored projects.
func (r *Registry) SetProjectsMonitored(n float64) {
	if r == nil {
		return
	}
	r.projectsMonitored.Set(n)
	if r.otel != nil {
		r.otel.SetProjectsMonitored(n)
	}
}

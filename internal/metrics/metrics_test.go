package metrics_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/pdrhp/gitlab-elt/internal/metrics"
)

func TestRegistry_RegistersExpectedCollectors(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := metrics.New(registry, nil)

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
	assertContainsMetric(t, metricFamilies, "gitlab_elt_gitlab_request_duration_seconds")
	assertContainsMetric(t, metricFamilies, "gitlab_elt_raw_events_ingested_total")
}

func TestRegistry_RecordsAllMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := metrics.New(registry, nil)

	now := time.Now().UTC()

	m.RecordSyncRun("transform", "error", 3*time.Second)
	m.RecordGitLabRequest("get_project", "500", 90*time.Millisecond)
	m.RecordRawEventIngested("456", "issue_event", 2)
	m.RecordTransformResult("456", "issue_event", "success")
	m.RecordTransformResult("456", "issue_event", "error")
	m.RecordLastSuccessfulSync("456", "transform", now)
	m.SetProjectsMonitored(7)

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	assertMetricHasSample(t, metricFamilies, "gitlab_elt_transform_results_total", map[string]string{
		"project_id": "456",
		"event_type": "issue_event",
		"status":     "success",
	})
	assertMetricHasSample(t, metricFamilies, "gitlab_elt_last_successful_sync_timestamp", map[string]string{
		"project_id": "456",
		"stage":      "transform",
	})
	assertGaugeValue(t, metricFamilies, "gitlab_elt_projects_monitored", 7)
}

func TestRegistry_RecordsOTLPMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	stub := &otlpMeterStub{}
	m := metrics.New(registry, stub)

	now := time.Unix(1700000000, 0).UTC()

	m.RecordSyncRun("transform", "error", 3*time.Second)
	m.RecordGitLabRequest("get_project", "500", 90*time.Millisecond)
	m.RecordRawEventIngested("456", "issue_event", 2)
	m.RecordTransformResult("456", "issue_event", "success")
	m.RecordLastSuccessfulSync("456", "transform", now)
	m.SetProjectsMonitored(7)

	if len(stub.syncRuns) != 1 {
		t.Fatalf("expected 1 OTLP sync run call, got %d", len(stub.syncRuns))
	}
	if got := stub.syncRuns[0]; got.stage != "transform" || got.status != "error" || got.duration != 3*time.Second {
		t.Fatalf("unexpected sync run payload: %+v", got)
	}

	if len(stub.gitlabRequests) != 1 {
		t.Fatalf("expected 1 OTLP gitlab request call, got %d", len(stub.gitlabRequests))
	}
	if got := stub.gitlabRequests[0]; got.endpoint != "get_project" || got.status != "500" || got.duration != 90*time.Millisecond {
		t.Fatalf("unexpected gitlab request payload: %+v", got)
	}

	if len(stub.rawEvents) != 1 {
		t.Fatalf("expected 1 OTLP raw event call, got %d", len(stub.rawEvents))
	}
	if got := stub.rawEvents[0]; got.projectID != "456" || got.eventType != "issue_event" || got.count != 2 {
		t.Fatalf("unexpected raw event payload: %+v", got)
	}

	if len(stub.transformResults) != 1 {
		t.Fatalf("expected 1 OTLP transform result call, got %d", len(stub.transformResults))
	}
	if got := stub.transformResults[0]; got.projectID != "456" || got.eventType != "issue_event" || got.status != "success" {
		t.Fatalf("unexpected transform result payload: %+v", got)
	}

	if len(stub.lastSyncs) != 1 {
		t.Fatalf("expected 1 OTLP last sync call, got %d", len(stub.lastSyncs))
	}
	if got := stub.lastSyncs[0]; got.projectID != "456" || got.stage != "transform" || !got.timestamp.Equal(now) {
		t.Fatalf("unexpected last sync payload: %+v", got)
	}

	if len(stub.projectsMonitored) != 1 {
		t.Fatalf("expected 1 OTLP projects monitored call, got %d", len(stub.projectsMonitored))
	}
	if stub.projectsMonitored[0] != 7 {
		t.Fatalf("unexpected projects monitored payload: %v", stub.projectsMonitored[0])
	}
}

func TestRegistry_RecordRawEventSkipsNonPositive(t *testing.T) {
	stub := &otlpMeterStub{}
	m := metrics.New(prometheus.NewRegistry(), stub)
	m.RecordRawEventIngested("p1", "event", 0)
	m.RecordRawEventIngested("p1", "event", -1)

	if len(stub.rawEvents) != 0 {
		t.Fatalf("expected no OTLP recordings for non-positive counts, got %d", len(stub.rawEvents))
	}
}

func TestRegistry_NilReceiverNoop(t *testing.T) {
	var r *metrics.Registry
	r.RecordSyncRun("stage", "status", time.Second)
	r.RecordGitLabRequest("endpoint", "200", time.Second)
	r.RecordRawEventIngested("p", "event", 1)
	r.RecordTransformResult("p", "event", "success")
	r.RecordLastSuccessfulSync("p", "stage", time.Now())
	r.SetProjectsMonitored(1)
}

func assertContainsMetric(t *testing.T, metricFamilies []*dto.MetricFamily, name string) {
	t.Helper()
	for _, mf := range metricFamilies {
		if mf.GetName() == name {
			return
		}
	}
	t.Fatalf("expected metric %s to be registered", name)
}

func assertMetricHasSample(t *testing.T, metricFamilies []*dto.MetricFamily, name string, labels map[string]string) {
	t.Helper()
	mf := findMetricFamily(metricFamilies, name)
	if mf == nil {
		t.Fatalf("metric family %s not found", name)
	}
	for _, metric := range mf.GetMetric() {
		if hasLabels(metric, labels) {
			return
		}
	}
	t.Fatalf("metric %s missing sample with labels %v", name, labels)
}

func assertGaugeValue(t *testing.T, metricFamilies []*dto.MetricFamily, name string, expected float64) {
	t.Helper()
	mf := findMetricFamily(metricFamilies, name)
	if mf == nil {
		t.Fatalf("metric family %s not found", name)
	}
	for _, metric := range mf.GetMetric() {
		if metric.GetGauge() == nil {
			continue
		}
		if metric.GetGauge().GetValue() == expected {
			return
		}
	}
	t.Fatalf("metric %s missing gauge value %.2f", name, expected)
}

func hasLabels(metric *dto.Metric, labels map[string]string) bool {
	for key, value := range labels {
		if !labelEquals(metric, key, value) {
			return false
		}
	}
	return true
}

func labelEquals(metric *dto.Metric, name, value string) bool {
	for _, lp := range metric.GetLabel() {
		if lp.GetName() == name && lp.GetValue() == value {
			return true
		}
	}
	return false
}

func findMetricFamily(metricFamilies []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, mf := range metricFamilies {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

type otlpMeterStub struct {
	syncRuns          []syncRunRecord
	gitlabRequests    []gitlabRequestRecord
	rawEvents         []rawEventRecord
	transformResults  []transformResultRecord
	lastSyncs         []lastSyncRecord
	projectsMonitored []float64
}

type syncRunRecord struct {
	stage    string
	status   string
	duration time.Duration
}

type gitlabRequestRecord struct {
	endpoint string
	status   string
	duration time.Duration
}

type rawEventRecord struct {
	projectID string
	eventType string
	count     int
}

type transformResultRecord struct {
	projectID string
	eventType string
	status    string
}

type lastSyncRecord struct {
	projectID string
	stage     string
	timestamp time.Time
}

func (s *otlpMeterStub) RecordSyncRun(stage, status string, duration time.Duration) {
	s.syncRuns = append(s.syncRuns, syncRunRecord{stage: stage, status: status, duration: duration})
}

func (s *otlpMeterStub) RecordGitLabRequest(endpoint, status string, duration time.Duration) {
	s.gitlabRequests = append(s.gitlabRequests, gitlabRequestRecord{endpoint: endpoint, status: status, duration: duration})
}

func (s *otlpMeterStub) RecordRawEventIngested(projectID, eventType string, count int) {
	s.rawEvents = append(s.rawEvents, rawEventRecord{projectID: projectID, eventType: eventType, count: count})
}

func (s *otlpMeterStub) RecordTransformResult(projectID, eventType, status string) {
	s.transformResults = append(s.transformResults, transformResultRecord{projectID: projectID, eventType: eventType, status: status})
}

func (s *otlpMeterStub) RecordLastSuccessfulSync(projectID, stage string, ts time.Time) {
	s.lastSyncs = append(s.lastSyncs, lastSyncRecord{projectID: projectID, stage: stage, timestamp: ts})
}

func (s *otlpMeterStub) SetProjectsMonitored(n float64) {
	s.projectsMonitored = append(s.projectsMonitored, n)
}

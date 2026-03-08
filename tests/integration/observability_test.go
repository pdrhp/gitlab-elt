//go:build integration

package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"

	"github.com/pdrhp/gitlab-elt/internal/metrics"
)

func TestObservabilityMetricsExposure(t *testing.T) {
	reg := prometheus.NewRegistry()
	recorder := metrics.New(reg, nil)

	recorder.RecordSyncRun("ingest", "success", 2*time.Second)
	recorder.RecordGitLabRequest("/projects", "200", 50*time.Millisecond)
	recorder.RecordRawEventIngested("123", "push", 4)
	recorder.RecordTransformResult("123", "push", "success")
	recorder.RecordTransformResult("123", "push", "error")
	recorder.RecordTransformResult("123", "push", "unknown_label")
	recorder.RecordLastSuccessfulSync("123", "ingest", time.Now())
	recorder.SetProjectsMonitored(5)

	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather registry: %v", err)
	}
	assertMetricSample(t, metricFamilies, "gitlab_elt_sync_runs_total", map[string]string{"stage": "ingest", "status": "success"})
	assertMetricSample(t, metricFamilies, "gitlab_elt_gitlab_requests_total", map[string]string{"endpoint": "/projects", "status": "200"})
	assertMetricSample(t, metricFamilies, "gitlab_elt_transform_results_total", map[string]string{"project_id": "123", "event_type": "push", "status": "unknown_label"})
	assertGaugeSample(t, metricFamilies, "gitlab_elt_projects_monitored", 5)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET metrics endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read metrics body: %v", err)
	}
	metricsOutput := string(body)

	for _, metricName := range []string{
		"gitlab_elt_sync_runs_total",
		"gitlab_elt_gitlab_requests_total",
		"gitlab_elt_projects_monitored",
	} {
		if !strings.Contains(metricsOutput, metricName) {
			t.Fatalf("expected metrics output to include %s", metricName)
		}
	}
}

func assertMetricSample(t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string) {
	t.Helper()
	mf := findMetricFamily(families, name)
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

func assertGaugeSample(t *testing.T, families []*dto.MetricFamily, name string, expected float64) {
	t.Helper()
	mf := findMetricFamily(families, name)
	if mf == nil {
		t.Fatalf("metric family %s not found", name)
	}
	for _, metric := range mf.GetMetric() {
		if metric.GetGauge() != nil && metric.GetGauge().GetValue() == expected {
			return
		}
	}
	t.Fatalf("metric %s missing gauge value %.2f", name, expected)
}

func findMetricFamily(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, mf := range families {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
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

package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pdrhp/gitlab-elt/internal/config"
	"github.com/pdrhp/gitlab-elt/internal/metrics"
)

func TestBuildOTLPMeter_DisabledWhenEndpointEmpty(t *testing.T) {
	metro, shutdown, err := metrics.BuildOTLPMeter(context.Background(), config.OTelMetricsConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metro != nil || shutdown != nil {
		t.Fatalf("expected nil meter and shutdown when endpoint empty")
	}
}

func TestBuildOTLPMeter_HTTPInsecure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	metro, shutdown, err := metrics.BuildOTLPMeter(context.Background(), config.OTelMetricsConfig{
		Endpoint: srv.URL,
		Insecure: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metro == nil || shutdown == nil {
		t.Fatalf("expected meter and shutdown to be non-nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestBuildOTLPMeter_HTTPSInsecureSkipVerify(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	metro, shutdown, err := metrics.BuildOTLPMeter(context.Background(), config.OTelMetricsConfig{
		Endpoint: srv.URL,
		Insecure: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metro == nil || shutdown == nil {
		t.Fatalf("expected meter and shutdown to be non-nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

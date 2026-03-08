package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/pdrhp/gitlab-elt/internal/health"
	"github.com/pdrhp/gitlab-elt/internal/metrics"
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

func TestHandlers_ExposesMetricsEndpoint(t *testing.T) {
	registry := prometheus.NewRegistry()
	obs := metrics.New(registry, nil)
	obs.RecordSyncRun("extract", "success", 10*time.Millisecond)

	mux := http.NewServeMux()
	health.RegisterHandlers(mux, &mockPinger{}, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "gitlab_elt_sync_runs_total") {
		t.Fatalf("expected Prometheus metrics output, got %q", w.Body.String())
	}
}

func TestRegisterHandlers_AllowsNilPinger(t *testing.T) {
	mux := http.NewServeMux()
	health.RegisterHandlers(mux, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "health pinger not configured") {
		t.Fatalf("expected message about missing pinger, got %s", w.Body.String())
	}
}

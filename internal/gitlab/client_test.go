package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/testutil"
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

func TestClient_RecordsMetricsForRequests(t *testing.T) {
	metrics := testutil.NewMetricsStub()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]domain.GitlabProject{})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tok",
		WithRateLimit(100),
		WithMaxRetries(1),
		WithMetrics(metrics),
	)
	if _, err := client.ListGroupProjects(context.Background(), 1); err != nil {
		t.Fatalf("ListGroupProjects() error = %v", err)
	}

	reqs := metrics.GitLabRequests()
	if len(reqs) != 1 {
		t.Fatalf("recorded %d gitlab requests, want 1", len(reqs))
	}
	if reqs[0].Endpoint != "list_group_projects" || reqs[0].Status != "200" {
		t.Fatalf("unexpected metrics call: %+v", reqs[0])
	}
}

func TestClient_RecordsMetricsOnTransportError(t *testing.T) {
	metrics := testutil.NewMetricsStub()
	errClient := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("boom")
	})}

	client := NewClient("http://example.com", "tok",
		WithHTTPClient(errClient),
		WithRateLimit(100),
		WithMaxRetries(2),
		WithMetrics(metrics),
	)
	if _, err := client.ListGroupProjects(context.Background(), 1); err == nil {
		t.Fatal("expected error when transport fails")
	}

	reqs := metrics.GitLabRequests()
	expected := client.maxRetries + 1
	if len(reqs) != expected {
		t.Fatalf("recorded %d gitlab requests, want %d", len(reqs), expected)
	}
	for _, call := range reqs {
		if call.Status != "error" {
			t.Fatalf("expected status=error, got %+v", call)
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

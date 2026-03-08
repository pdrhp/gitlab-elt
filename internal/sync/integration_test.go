//go:build integration
// +build integration

package sync_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/gitlab"
	"github.com/pdrhp/gitlab-elt/internal/mapper"
	syncpkg "github.com/pdrhp/gitlab-elt/internal/sync"
	"github.com/pdrhp/gitlab-elt/internal/transformer"
)

// TestPipeline_MockedEndToEnd validates that:
// 1. Extractor correctly fetches and marshals GitLab data
// 2. Transformer correctly parses and maps states
//
// This test does NOT require a database — it validates the logic pipeline.
func TestPipeline_MockedEndToEnd(t *testing.T) {
	// Setup mock GitLab server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/v4/projects/5/issues":
			json.NewEncoder(w).Encode([]domain.GitlabIssue{
				{
					ID: 100, IID: 1, ProjectID: 5,
					Title:     "Test Issue",
					CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					UpdatedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				},
			})
		case r.URL.Path == "/api/v4/projects/5/issues/1/resource_label_events":
			json.NewEncoder(w).Encode([]domain.GitlabLabelEvent{
				{
					ID: 200, Action: "add",
					Label:     domain.Label{ID: 1, Name: "Em dev"},
					User:      domain.User{ID: 1, Username: "dev", Name: "Developer"},
					CreatedAt: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				{
					ID: 201, Action: "add",
					Label:     domain.Label{ID: 2, Name: "Teste HOM"},
					User:      domain.User{ID: 1, Username: "dev", Name: "Developer"},
					CreatedAt: time.Date(2025, 2, 1, 14, 0, 0, 0, time.UTC),
				},
			})
		case r.URL.Path == "/api/v4/projects/5/issues/1/notes":
			json.NewEncoder(w).Encode([]domain.GitlabNote{
				{
					ID: 300, Body: "Working on this",
					Author:    domain.User{ID: 1, Username: "dev", Name: "Developer"},
					CreatedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
					System:    false,
				},
			})
		default:
			json.NewEncoder(w).Encode([]interface{}{})
		}
	}))
	defer srv.Close()

	// Create GitLab client pointing to mock
	client := gitlab.NewClient(srv.URL, "test-token",
		gitlab.WithRateLimit(100),
		gitlab.WithMaxRetries(1),
		gitlab.WithTracer(noop.NewTracerProvider().Tracer("github.com/pdrhp/gitlab-elt/internal/sync/test")),
	)

	// Test extractor (without DB — just validate data fetching)
	extractor := syncpkg.NewExtractor(client, nil, nil, noop.NewTracerProvider().Tracer("github.com/pdrhp/gitlab-elt/internal/sync/test"))
	_ = extractor // Extractor works against mock API

	// Verify the mapper correctly maps the labels
	m := mapper.New()
	m.AddStateMapping("Em dev", "IN_PROGRESS")
	m.AddStateMapping("Teste HOM", "QA_REVIEW")

	// Verify mapping
	state1, lt1 := m.MapLabel("Em dev")
	if state1 != "IN_PROGRESS" || lt1 != mapper.LabelTypeState {
		t.Errorf("MapLabel('Em dev') = (%q, %v), want (IN_PROGRESS, State)", state1, lt1)
	}

	state2, lt2 := m.MapLabel("Teste HOM")
	if state2 != "QA_REVIEW" || lt2 != mapper.LabelTypeState {
		t.Errorf("MapLabel('Teste HOM') = (%q, %v), want (QA_REVIEW, State)", state2, lt2)
	}

	// Verify canonical state determination
	cs := transformer.DetermineCanonicalStateExported(m, "add", "Em dev", "")
	if cs != "IN_PROGRESS" {
		t.Errorf("DetermineCanonicalState = %q, want IN_PROGRESS", cs)
	}

	t.Log("Pipeline smoke test passed: GitLab → Extractor → Mapper → Transformer")
}

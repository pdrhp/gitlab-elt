package sync

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/repository"
	"github.com/pdrhp/gitlab-elt/internal/testutil"
)

type stubExtractorQueries struct {
	syncProjectResult struct {
		RawProjectsUpdated int64
		ProjectsUpdated    int64
	}
	syncProjectCalled     bool
	projectLastSynced     time.Time
	lastProjectUpdateID   int32
	insertedRawIssueCount int
	rawProjects           []repository.RawProject
	listRawProjectsErr    error
}

func (s *stubExtractorQueries) BulkInsertRawEvent(_ context.Context, _ repository.BulkInsertRawEventParams) error {
	return nil
}

func (s *stubExtractorQueries) SyncProjectLastSynced(_ context.Context, arg repository.SyncProjectLastSyncedParams) (repository.SyncProjectLastSyncedRow, error) {
	s.syncProjectCalled = true
	s.projectLastSynced = arg.LastSyncedAt.Time
	s.lastProjectUpdateID = arg.ID
	return repository.SyncProjectLastSyncedRow(s.syncProjectResult), nil
}

func (s *stubExtractorQueries) ListRawProjects(_ context.Context) ([]repository.RawProject, error) {
	if s.listRawProjectsErr != nil {
		return nil, s.listRawProjectsErr
	}
	return s.rawProjects, nil
}

func (s *stubExtractorQueries) UpsertRawIssue(_ context.Context, _ repository.UpsertRawIssueParams) (repository.RawIssue, error) {
	s.insertedRawIssueCount++
	return repository.RawIssue{}, nil
}

type mockGitlab struct {
	issues      []domain.GitlabIssue
	labelEvents []domain.GitlabLabelEvent
	notes       []domain.GitlabNote
}

func (m *mockGitlab) ListGroupProjects(_ context.Context, _ int) ([]domain.GitlabProject, error) {
	return nil, nil
}
func (m *mockGitlab) ListIssues(_ context.Context, _ int, _ time.Time) ([]domain.GitlabIssue, error) {
	return m.issues, nil
}
func (m *mockGitlab) ListLabelEvents(_ context.Context, _ int, _ int) ([]domain.GitlabLabelEvent, error) {
	return m.labelEvents, nil
}
func (m *mockGitlab) ListNotes(_ context.Context, _ int, _ int) ([]domain.GitlabNote, error) {
	return m.notes, nil
}

func TestExtractor_BuildRawEvents(t *testing.T) {
	gitlab := &mockGitlab{
		issues: []domain.GitlabIssue{
			{ID: 100, IID: 1, ProjectID: 5, Title: "Test Issue"},
		},
		labelEvents: []domain.GitlabLabelEvent{
			{
				ID:     200,
				Action: "add",
				Label:  domain.Label{ID: 1, Name: "Em dev"},
				User:   domain.User{ID: 1, Username: "dev"},
			},
		},
		notes: []domain.GitlabNote{
			{ID: 300, Body: "A comment", Author: domain.User{ID: 1, Username: "dev"}},
		},
	}

	ext := NewExtractor(gitlab, nil, nil, nil)

	events, err := ext.fetchIssueRawEvents(context.Background(), 5, domain.GitlabIssue{
		ID: 100, IID: 1, ProjectID: 5, Title: "Test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 2 raw events: 1 label event + 1 note
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	// First should be label_event
	if events[0].EventType != "label_event" {
		t.Errorf("got event_type=%q, want 'label_event'", events[0].EventType)
	}
	// Second should be note
	if events[1].EventType != "note" {
		t.Errorf("got event_type=%q, want 'note'", events[1].EventType)
	}
}

func TestExtractor_EmptyIssues(t *testing.T) {
	gitlab := &mockGitlab{
		issues: []domain.GitlabIssue{},
	}

	ext := NewExtractor(gitlab, nil, nil, nil)
	events, err := ext.fetchProjectRawEvents(context.Background(), 5, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events, want 0", len(events))
	}
}

func TestExtractor_ExtractProject_UpdatesSilverProjectLastSynced(t *testing.T) {
	gitlab := &mockGitlab{
		issues: []domain.GitlabIssue{
			{ID: 100, IID: 1, ProjectID: 5, Title: "Test Issue", State: "opened"},
		},
	}
	queries := &stubExtractorQueries{}
	queries.syncProjectResult.RawProjectsUpdated = 1
	queries.syncProjectResult.ProjectsUpdated = 1

	ext := NewExtractor(gitlab, queries, nil, nil)

	project := repository.RawProject{
		ID:   5,
		Name: "Project Five",
		LastSyncedAt: pgtype.Timestamptz{
			Time:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Valid: true,
		},
	}

	if err := ext.ExtractProject(context.Background(), project); err != nil {
		t.Fatalf("ExtractProject() error = %v", err)
	}

	if !queries.syncProjectCalled {
		t.Fatalf("expected project sync timestamps to be updated")
	}
	if queries.lastProjectUpdateID != project.ID {
		t.Fatalf("updated project id = %d, want %d", queries.lastProjectUpdateID, project.ID)
	}
	if !queries.projectLastSynced.IsZero() && queries.projectLastSynced.Location() != time.UTC {
		t.Fatalf("expected synced timestamp in UTC, got %s", queries.projectLastSynced.Location())
	}
	if queries.insertedRawIssueCount != 1 {
		t.Fatalf("inserted raw issue count = %d, want 1", queries.insertedRawIssueCount)
	}
}

func TestExtractor_ExtractProject_ErrorsWhenSilverProjectSyncUpdateMissesRow(t *testing.T) {
	gitlab := &mockGitlab{}
	queries := &stubExtractorQueries{}
	queries.syncProjectResult.RawProjectsUpdated = 1
	queries.syncProjectResult.ProjectsUpdated = 0

	ext := NewExtractor(gitlab, queries, nil, nil)

	err := ext.ExtractProject(context.Background(), repository.RawProject{ID: 9, Name: "Missing Silver"})
	if err == nil {
		t.Fatalf("expected error when silver project row is not updated")
	}
}

func TestExtractor_ExtractAll_RecordsMetrics(t *testing.T) {
	gitlab := &mockGitlab{
		issues: []domain.GitlabIssue{
			{ID: 1, IID: 1, ProjectID: 5, Title: "One"},
		},
		labelEvents: []domain.GitlabLabelEvent{{ID: 10, Action: "add", Label: domain.Label{Name: "Em dev"}}},
		notes:       []domain.GitlabNote{{ID: 20, Body: "note"}},
	}
	queries := &stubExtractorQueries{}
	queries.syncProjectResult.RawProjectsUpdated = 1
	queries.syncProjectResult.ProjectsUpdated = 1
	queries.rawProjects = []repository.RawProject{{ID: 5, Name: "Proj"}}
	metrics := testutil.NewMetricsStub()

	ext := NewExtractor(gitlab, queries, metrics, nil)
	if err := ext.ExtractAll(context.Background()); err != nil {
		t.Fatalf("ExtractAll() error = %v", err)
	}

	syncRuns := metrics.SyncRuns()
	if len(syncRuns) != 1 {
		t.Fatalf("recorded %d sync runs, want 1", len(syncRuns))
	}
	if syncRuns[0].Stage != "extract" || syncRuns[0].Status != "success" {
		t.Fatalf("unexpected sync run: %+v", syncRuns[0])
	}

	last := metrics.LastSuccessfulSyncs()
	if len(last) != 1 {
		t.Fatalf("recorded %d last syncs, want 1", len(last))
	}
	if last[0].Stage != "extract" || last[0].ProjectID != "5" {
		t.Fatalf("unexpected last sync: %+v", last[0])
	}
	if !last[0].Timestamp.Equal(queries.projectLastSynced) {
		t.Fatalf("last sync timestamp = %v, want %v", last[0].Timestamp, queries.projectLastSynced)
	}

	rawEvents := metrics.RawEvents()
	if len(rawEvents) != 2 {
		t.Fatalf("recorded %d raw events, want 2", len(rawEvents))
	}
}

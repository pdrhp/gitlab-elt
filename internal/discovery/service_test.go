package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/repository"
	"github.com/pdrhp/gitlab-elt/internal/testutil"
)

// mockGitlabClient implements domain.GitlabClient for testing.
type mockGitlabClient struct {
	projects []domain.GitlabProject
	err      error
}

type stubDiscoveryQueries struct {
	existingProject        repository.Project
	getProjectErr          error
	upsertProjectArg       repository.UpsertProjectParams
	upsertProjectCalled    bool
	upsertRawProjectArg    repository.UpsertRawProjectParams
	upsertRawProjectCalled bool
}

func (m *mockGitlabClient) ListGroupProjects(_ context.Context, _ int) ([]domain.GitlabProject, error) {
	return m.projects, m.err
}
func (m *mockGitlabClient) ListIssues(_ context.Context, _ int, _ time.Time) ([]domain.GitlabIssue, error) {
	return nil, nil
}
func (m *mockGitlabClient) ListLabelEvents(_ context.Context, _ int, _ int) ([]domain.GitlabLabelEvent, error) {
	return nil, nil
}
func (m *mockGitlabClient) ListNotes(_ context.Context, _ int, _ int) ([]domain.GitlabNote, error) {
	return nil, nil
}

func (s *stubDiscoveryQueries) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (s *stubDiscoveryQueries) GetProject(_ context.Context, _ int32) (repository.Project, error) {
	if s.getProjectErr != nil {
		return repository.Project{}, s.getProjectErr
	}
	return s.existingProject, nil
}

func (s *stubDiscoveryQueries) UpsertProject(_ context.Context, arg repository.UpsertProjectParams) (repository.Project, error) {
	s.upsertProjectCalled = true
	s.upsertProjectArg = arg
	return repository.Project{}, nil
}

func (s *stubDiscoveryQueries) UpsertRawProject(_ context.Context, arg repository.UpsertRawProjectParams) (repository.RawProject, error) {
	s.upsertRawProjectCalled = true
	s.upsertRawProjectArg = arg
	return repository.RawProject{}, nil
}

func TestService_DiscoverProjects(t *testing.T) {
	gitlabClient := &mockGitlabClient{
		projects: []domain.GitlabProject{
			{ID: 1, Name: "Project Alpha", PathWithNamespace: "group/alpha"},
			{ID: 2, Name: "Project Beta", PathWithNamespace: "group/beta"},
		},
	}

	svc := NewService(gitlabClient, nil, []int{42}, nil, nil)
	projects, err := svc.fetchGroupProjects(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("got %d projects, want 2", len(projects))
	}
}

func TestService_DiscoverMultipleGroups(t *testing.T) {
	gitlabClient := &mockGitlabClient{
		projects: []domain.GitlabProject{
			{ID: 1, Name: "P1", PathWithNamespace: "g/p1"},
		},
	}

	svc := NewService(gitlabClient, nil, []int{10, 20}, nil, nil)
	projects, err := svc.fetchAllGroupProjects(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 project per group × 2 groups = 2 (but same ID deduped)
	// The mock returns the same project for both groups
	if len(projects) != 1 {
		t.Errorf("got %d unique projects, want 1 (deduped)", len(projects))
	}
}

func TestService_DiscoverNoGroups(t *testing.T) {
	svc := NewService(nil, nil, []int{}, nil, nil)
	projects, err := svc.fetchAllGroupProjects(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("got %d projects, want 0", len(projects))
	}
}

func TestService_PersistProject_PreservesExistingSilverLastSyncedAt(t *testing.T) {
	existingSync := time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC)
	queries := &stubDiscoveryQueries{
		existingProject: repository.Project{
			ID:           1,
			LastSyncedAt: pgtype.Timestamptz{Time: existingSync, Valid: true},
		},
	}

	svc := NewService(&mockGitlabClient{}, queries, []int{42}, nil, nil)
	project := domain.GitlabProject{ID: 1, Name: "Project Alpha", PathWithNamespace: "group/alpha"}

	if err := svc.persistProject(context.Background(), project); err != nil {
		t.Fatalf("persistProject() error = %v", err)
	}

	if !queries.upsertRawProjectCalled {
		t.Fatalf("expected raw project to be upserted")
	}
	if !queries.upsertProjectCalled {
		t.Fatalf("expected silver project to be upserted")
	}
	if !queries.upsertProjectArg.LastSyncedAt.Time.Equal(existingSync) {
		t.Fatalf("silver last_synced_at = %s, want preserved %s", queries.upsertProjectArg.LastSyncedAt.Time, existingSync)
	}
	if queries.upsertRawProjectArg.LastSyncedAt.Time.Year() != 1970 {
		t.Fatalf("raw project last_synced_at year = %d, want 1970 seed cursor", queries.upsertRawProjectArg.LastSyncedAt.Time.Year())
	}
}

func TestService_Run_RecordsMetrics(t *testing.T) {
	gitlabClient := &mockGitlabClient{
		projects: []domain.GitlabProject{
			{ID: 10, Name: "Alpha", PathWithNamespace: "g/alpha"},
			{ID: 11, Name: "Beta", PathWithNamespace: "g/beta"},
		},
	}
	queries := &stubDiscoveryQueries{}
	metrics := testutil.NewMetricsStub()

	svc := NewService(gitlabClient, queries, []int{1}, metrics, nil)
	if err := svc.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	syncRuns := metrics.SyncRuns()
	if len(syncRuns) != 1 {
		t.Fatalf("recorded %d sync runs, want 1", len(syncRuns))
	}
	if syncRuns[0].Stage != "discovery" || syncRuns[0].Status != "success" {
		t.Fatalf("sync run = %+v, want stage discovery success", syncRuns[0])
	}

	last := metrics.LastSuccessfulSyncs()
	if len(last) != 2 {
		t.Fatalf("recorded %d last successful syncs, want 2", len(last))
	}
	if last[0].Stage != "discovery" || last[1].Stage != "discovery" {
		t.Fatalf("unexpected stage values: %+v", last)
	}

	gauge := metrics.ProjectsMonitored()
	if len(gauge) != 1 {
		t.Fatalf("expected gauge update, got %d entries", len(gauge))
	}
	if gauge[0].Value != 2 {
		t.Fatalf("projects monitored gauge = %v, want 2", gauge[0].Value)
	}
}

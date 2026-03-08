package transformer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/mapper"
	"github.com/pdrhp/gitlab-elt/internal/repository"
	"github.com/pdrhp/gitlab-elt/internal/testutil"
)

func TestParseLabelEvent(t *testing.T) {
	le := domain.GitlabLabelEvent{
		ID:        100,
		Action:    "add",
		Label:     domain.Label{ID: 1, Name: "Em dev"},
		User:      domain.User{ID: 1, Username: "dev", Name: "Dev User"},
		CreatedAt: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	payload, _ := json.Marshal(le)

	parsed, err := parseLabelEventPayload(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Label.Name != "Em dev" {
		t.Errorf("got label=%q, want 'Em dev'", parsed.Label.Name)
	}
	if parsed.Action != "add" {
		t.Errorf("got action=%q, want 'add'", parsed.Action)
	}
}

func TestParseNotePayload(t *testing.T) {
	note := domain.GitlabNote{
		ID:        200,
		Body:      "Fixed the bug",
		Author:    domain.User{ID: 1, Username: "dev", Name: "Dev"},
		CreatedAt: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		System:    false,
	}
	payload, _ := json.Marshal(note)

	parsed, err := parseNotePayload(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Body != "Fixed the bug" {
		t.Errorf("got body=%q, want 'Fixed the bug'", parsed.Body)
	}
}

func TestDetermineCanonicalState_AddAction(t *testing.T) {
	m := newTestMapper()

	// "add" action with a state label → mapped state
	state := determineCanonicalState(m, "add", "Em dev", "")
	if state != "IN_PROGRESS" {
		t.Errorf("got %q, want IN_PROGRESS", state)
	}
}

func TestDetermineCanonicalState_RemoveAction(t *testing.T) {
	m := newTestMapper()

	// "remove" action with a state label → previous state removed, return UNKNOWN
	// because we don't know the new state from a remove event alone
	state := determineCanonicalState(m, "remove", "", "Em dev")
	if state != "UNKNOWN" {
		t.Errorf("got %q, want UNKNOWN", state)
	}
}

func TestDetermineCanonicalState_MetadataLabel(t *testing.T) {
	m := newTestMapper()

	// Metadata label (Bug) → skip, not a state transition
	state := determineCanonicalState(m, "add", "Bug", "")
	if state != "" {
		t.Errorf("got %q, want empty (metadata labels are not state transitions)", state)
	}
}

func newTestMapper() *mapper.Mapper {
	m := mapper.New()
	m.AddStateMapping("Backlog", "BACKLOG")
	m.AddStateMapping("Em dev", "IN_PROGRESS")
	m.AddStateMapping("Teste HOM", "QA_REVIEW")
	m.AddStateMapping("Bloqueado", "BLOCKED")
	m.AddStateMapping("Concluido", "DONE")
	m.AddStateMapping("Cancelado", "CANCELED")
	m.AddMetadataMapping("Bug", "tipo")
	return m
}

func TestService_TransformAll_RecordsStageMetrics(t *testing.T) {
	queries := &stubTransformerQueries{}
	metrics := testutil.NewMetricsStub()
	svc := NewService(queries, newTestMapper(), metrics, nil)

	if _, err := svc.TransformAll(context.Background()); err != nil {
		t.Fatalf("TransformAll() error = %v", err)
	}

	runs := metrics.SyncRuns()
	if len(runs) != 1 {
		t.Fatalf("recorded %d sync runs, want 1", len(runs))
	}
	if runs[0].Stage != "transform" || runs[0].Status != "success" {
		t.Fatalf("unexpected sync run: %+v", runs[0])
	}
}

func TestService_transformLabelEvent_RecordsMetrics(t *testing.T) {
	metrics := testutil.NewMetricsStub()
	queries := &stubTransformerQueries{}
	svc := &Service{queries: queries, mapper: newTestMapper(), batchSize: 10, metrics: metrics}

	raw := repository.RawEvent{
		ProjectID: 7,
		IssueIid:  1,
		EventType: "label_event",
		RawPayload: mustJSON(domain.GitlabLabelEvent{
			ID:     1,
			Action: "add",
			Label:  domain.Label{Name: "Em dev"},
		}),
	}
	if err := svc.transformLabelEvent(context.Background(), raw); err != nil {
		t.Fatalf("transformLabelEvent() error = %v", err)
	}

	queries.insertIssueEventErr = errors.New("boom")
	if err := svc.transformLabelEvent(context.Background(), raw); err == nil {
		t.Fatalf("expected error from insert failure")
	}
	queries.insertIssueEventErr = nil

	unknownRaw := repository.RawEvent{
		ProjectID: 7,
		IssueIid:  1,
		EventType: "label_event",
		RawPayload: mustJSON(domain.GitlabLabelEvent{
			ID:     2,
			Action: "add",
			Label:  domain.Label{Name: "Mystery"},
		}),
	}
	if err := svc.transformLabelEvent(context.Background(), unknownRaw); err != nil {
		t.Fatalf("transformLabelEvent unknown error = %v", err)
	}

	results := metrics.TransformResults()
	if len(results) < 3 {
		t.Fatalf("expected at least 3 transform results, got %d: %+v", len(results), results)
	}
	if results[0].Status != "success" || results[1].Status != "error" {
		t.Fatalf("unexpected first statuses: %+v", results)
	}
	foundUnknown := false
	for _, r := range results {
		if r.Status == "unknown_label" {
			foundUnknown = true
			break
		}
	}
	if !foundUnknown {
		t.Fatalf("expected unknown_label status in %+v", results)
	}
}

func TestService_transformNote_RecordsMetrics(t *testing.T) {
	metrics := testutil.NewMetricsStub()
	queries := &stubTransformerQueries{}
	svc := &Service{queries: queries, mapper: newTestMapper(), batchSize: 10, metrics: metrics}

	userNote := repository.RawEvent{
		ProjectID: 8,
		IssueIid:  3,
		EventType: "note",
		RawPayload: mustJSON(domain.GitlabNote{
			ID:        20,
			Body:      "hello",
			Author:    domain.User{Username: "dev"},
			CreatedAt: time.Now(),
		}),
	}
	if err := svc.transformNote(context.Background(), userNote); err != nil {
		t.Fatalf("transformNote() error = %v", err)
	}

	queries.insertIssueCommentErr = errors.New("fail")
	if err := svc.transformNote(context.Background(), userNote); err == nil {
		t.Fatalf("expected error from comment insert")
	}
	queries.insertIssueCommentErr = nil

	systemNote := repository.RawEvent{
		ProjectID: 8,
		IssueIid:  3,
		EventType: "note",
		RawPayload: mustJSON(domain.GitlabNote{
			ID:        21,
			Body:      "assigned to @dev",
			Author:    domain.User{Username: "system"},
			CreatedAt: time.Now(),
			System:    true,
		}),
	}
	if err := svc.transformNote(context.Background(), systemNote); err != nil {
		t.Fatalf("transformNote system note error = %v", err)
	}

	results := metrics.TransformResults()
	if len(results) < 3 {
		t.Fatalf("expected at least 3 transform results, got %d", len(results))
	}
	if results[0].Status != "success" || results[1].Status != "error" {
		t.Fatalf("unexpected transform result statuses: %+v", results[:2])
	}
	if results[2].Status != "success" {
		t.Fatalf("expected system note success status, got %+v", results[2])
	}
}

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

type stubTransformerQueries struct {
	rawEvents             []repository.RawEvent
	insertIssueEventErr   error
	insertIssueCommentErr error
}

func (s *stubTransformerQueries) ListUnprocessedRawEvents(context.Context, int32) ([]repository.RawEvent, error) {
	return s.rawEvents, nil
}

func (s *stubTransformerQueries) MarkRawEventsProcessedBatch(context.Context, []int64) error {
	return nil
}

func (s *stubTransformerQueries) UpsertUnknownLabel(context.Context, string) (repository.UnknownLabelsLog, error) {
	return repository.UnknownLabelsLog{}, nil
}

func (s *stubTransformerQueries) GetLastIssueEvent(context.Context, int32) (repository.IssueEvent, error) {
	return repository.IssueEvent{}, nil
}

func (s *stubTransformerQueries) InsertIssueEvent(context.Context, repository.InsertIssueEventParams) (repository.IssueEvent, error) {
	if s.insertIssueEventErr != nil {
		return repository.IssueEvent{}, s.insertIssueEventErr
	}
	return repository.IssueEvent{}, nil
}

func (s *stubTransformerQueries) UpdateIssueCanonicalState(context.Context, repository.UpdateIssueCanonicalStateParams) error {
	return nil
}

func (s *stubTransformerQueries) InsertIssueComment(context.Context, repository.InsertIssueCommentParams) (repository.IssueComment, error) {
	if s.insertIssueCommentErr != nil {
		return repository.IssueComment{}, s.insertIssueCommentErr
	}
	return repository.IssueComment{}, nil
}

func (s *stubTransformerQueries) UpdateIssueAssignees(context.Context, repository.UpdateIssueAssigneesParams) error {
	return nil
}

func (s *stubTransformerQueries) GetIssueByProjectAndIID(context.Context, repository.GetIssueByProjectAndIIDParams) (repository.Issue, error) {
	return repository.Issue{ID: 100}, nil
}

func (s *stubTransformerQueries) GetRawIssueByProjectAndIID(context.Context, repository.GetRawIssueByProjectAndIIDParams) (repository.RawIssue, error) {
	return repository.RawIssue{}, nil
}

func (s *stubTransformerQueries) UpsertIssue(context.Context, repository.UpsertIssueParams) (repository.Issue, error) {
	return repository.Issue{ID: 100}, nil
}

func (s *stubTransformerQueries) GetIssueByID(context.Context, int32) (repository.Issue, error) {
	return repository.Issue{ID: 100}, nil
}

func (s *stubTransformerQueries) UpdateIssueMetadata(context.Context, repository.UpdateIssueMetadataParams) error {
	return nil
}

package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/repository"
)

// RawEventData holds the data needed to insert a raw event into Bronze.
type RawEventData struct {
	GitlabEventID int64
	ProjectID     int32
	IssueIID      int32
	EventType     string // "label_event" or "note"
	RawPayload    []byte
}

type extractorQueries interface {
	BulkInsertRawEvent(ctx context.Context, arg repository.BulkInsertRawEventParams) error
	ListRawProjects(ctx context.Context) ([]repository.RawProject, error)
	SyncProjectLastSynced(ctx context.Context, arg repository.SyncProjectLastSyncedParams) (repository.SyncProjectLastSyncedRow, error)
	UpsertRawIssue(ctx context.Context, arg repository.UpsertRawIssueParams) (repository.RawIssue, error)
}

// Extractor fetches data from GitLab and inserts it into Bronze layer (raw_events).
type Extractor struct {
	gitlab  domain.GitlabClient
	queries extractorQueries
	metrics domain.MetricsRecorder
	tracer  trace.Tracer
}

// NewExtractor creates a new Extractor.
func NewExtractor(gitlab domain.GitlabClient, queries extractorQueries, metrics domain.MetricsRecorder, tracer trace.Tracer) *Extractor {
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("github.com/pdrhp/gitlab-elt/internal/sync")
	}
	return &Extractor{
		gitlab:  gitlab,
		queries: queries,
		metrics: metrics,
		tracer:  tracer,
	}
}

// ExtractProject runs Bronze extraction for a single project:
// 1. Fetch issues updated since last sync
// 2. For each issue, fetch label events + notes
// 3. Insert raw events into raw_events table
// 4. Update sync cursor
func (e *Extractor) ExtractProject(ctx context.Context, project repository.RawProject) error {
	ctx, span := e.tracer.Start(ctx, "extractor.ExtractProject",
		trace.WithAttributes(
			attribute.Int("project.id", int(project.ID)),
			attribute.String("project.name", project.Name),
		),
	)
	defer span.End()

	lastSynced := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	if project.LastSyncedAt.Valid {
		lastSynced = project.LastSyncedAt.Time
	}

	slog.Info("extractor: starting project extraction",
		"project_id", project.ID,
		"project_name", project.Name,
		"last_synced_at", lastSynced,
	)

	syncStartTime := time.Now().UTC()

	rawEvents, err := e.fetchProjectRawEvents(ctx, int(project.ID), lastSynced)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("fetch raw events for project %d: %w", project.ID, err)
	}

	if e.queries == nil {
		slog.Warn("extractor: no repository configured, skipping persistence",
			"project_id", project.ID,
			"events_fetched", len(rawEvents),
		)
		return nil
	}

	// Persist raw events in batch
	persisted := 0
	for _, evt := range rawEvents {
		err := e.queries.BulkInsertRawEvent(ctx, repository.BulkInsertRawEventParams{
			GitlabEventID: pgtype.Int8{Int64: evt.GitlabEventID, Valid: evt.GitlabEventID > 0},
			ProjectID:     evt.ProjectID,
			IssueIid:      evt.IssueIID,
			EventType:     evt.EventType,
			RawPayload:    evt.RawPayload,
		})
		if err != nil {
			slog.Error("extractor: failed to insert raw event",
				"project_id", evt.ProjectID,
				"issue_iid", evt.IssueIID,
				"event_type", evt.EventType,
				"error", err,
			)
			continue
		}
		if e.metrics != nil {
			e.metrics.RecordRawEventIngested(strconv.Itoa(int(evt.ProjectID)), evt.EventType, 1)
		}
		persisted++
	}

	// Update sync cursor atomically in Bronze + Silver
	updated, err := e.queries.SyncProjectLastSynced(ctx, repository.SyncProjectLastSyncedParams{
		LastSyncedAt: pgtype.Timestamptz{Time: syncStartTime, Valid: true},
		ID:           project.ID,
	})
	if err != nil {
		return fmt.Errorf("sync project cursor for project %d: %w", project.ID, err)
	}
	if updated.RawProjectsUpdated != 1 {
		return fmt.Errorf("sync project cursor for project %d: raw project rows updated = %d, want 1", project.ID, updated.RawProjectsUpdated)
	}
	if updated.ProjectsUpdated != 1 {
		return fmt.Errorf("sync project cursor for project %d: silver project rows updated = %d, want 1", project.ID, updated.ProjectsUpdated)
	}
	if e.metrics != nil {
		e.metrics.RecordLastSuccessfulSync(strconv.Itoa(int(project.ID)), "extract", syncStartTime)
	}

	span.SetAttributes(
		attribute.Int("events.fetched", len(rawEvents)),
		attribute.Int("events.persisted", persisted),
	)

	slog.Info("extractor: completed project extraction",
		"project_id", project.ID,
		"events_fetched", len(rawEvents),
		"events_persisted", persisted,
	)
	return nil
}

// ExtractAll runs Bronze extraction for all projects that are due for sync.
func (e *Extractor) ExtractAll(ctx context.Context) error {
	ctx, span := e.tracer.Start(ctx, "extractor.ExtractAll")
	defer span.End()

	if e.queries == nil {
		return fmt.Errorf("extractor: repository not configured")
	}
	start := time.Now()
	status := "success"
	defer func() {
		if e.metrics != nil {
			e.metrics.RecordSyncRun("extract", status, time.Since(start))
		}
	}()

	projects, err := e.queries.ListRawProjects(ctx)
	if err != nil {
		status = "error"
		span.RecordError(err)
		return fmt.Errorf("list raw projects: %w", err)
	}

	slog.Info("extractor: starting extraction for all projects", "count", len(projects))

	var errs []error
	for _, p := range projects {
		if err := e.ExtractProject(ctx, p); err != nil {
			slog.Error("extractor: project extraction failed",
				"project_id", p.ID,
				"error", err,
			)
			errs = append(errs, err)
		}
	}

	span.SetAttributes(attribute.Int("projects.count", len(projects)))

	if len(errs) > 0 {
		status = "error"
		span.RecordError(fmt.Errorf("%d project(s) failed", len(errs)))
		return fmt.Errorf("extractor: %d project(s) failed", len(errs))
	}
	return nil
}

// fetchProjectRawEvents fetches all raw events for a project since lastSynced.
func (e *Extractor) fetchProjectRawEvents(ctx context.Context, projectID int, lastSynced time.Time) ([]RawEventData, error) {
	issues, err := e.gitlab.ListIssues(ctx, projectID, lastSynced)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	slog.Info("extractor: fetched issues",
		"project_id", projectID,
		"count", len(issues),
	)

	var allEvents []RawEventData
	for _, issue := range issues {
		// Persist issue metadata to raw_issues
		if err := e.persistIssueMetadata(ctx, projectID, issue); err != nil {
			slog.Error("extractor: failed to persist issue metadata",
				"project_id", projectID,
				"issue_iid", issue.IID,
				"error", err,
			)
			// Continue even if metadata persistence fails
		}

		events, err := e.fetchIssueRawEvents(ctx, projectID, issue)
		if err != nil {
			slog.Error("extractor: failed to fetch events for issue",
				"project_id", projectID,
				"issue_iid", issue.IID,
				"error", err,
			)
			continue
		}
		allEvents = append(allEvents, events...)
	}

	return allEvents, nil
}

// persistIssueMetadata saves issue metadata to raw_issues table.
func (e *Extractor) persistIssueMetadata(ctx context.Context, projectID int, issue domain.GitlabIssue) error {
	if e.queries == nil {
		return nil
	}

	// Marshal full issue payload
	payload, err := json.Marshal(issue)
	if err != nil {
		return fmt.Errorf("marshal issue payload: %w", err)
	}

	_, err = e.queries.UpsertRawIssue(ctx, repository.UpsertRawIssueParams{
		GitlabIssueID: int64(issue.ID),
		ProjectID:     int32(projectID),
		Iid:           int32(issue.IID),
		Title:         issue.Title,
		Description:   pgtype.Text{Valid: false}, // Description not available in GitlabIssue struct
		State:         issue.State,
		RawPayload:    payload,
	})
	if err != nil {
		return fmt.Errorf("upsert raw issue: %w", err)
	}

	return nil
}

// fetchIssueRawEvents fetches label events + notes for a single issue and returns raw event data.
func (e *Extractor) fetchIssueRawEvents(ctx context.Context, projectID int, issue domain.GitlabIssue) ([]RawEventData, error) {
	var events []RawEventData

	// Fetch label events
	labelEvents, err := e.gitlab.ListLabelEvents(ctx, projectID, issue.IID)
	if err != nil {
		return nil, fmt.Errorf("list label events for issue %d: %w", issue.IID, err)
	}

	for _, le := range labelEvents {
		payload, err := json.Marshal(le)
		if err != nil {
			return nil, fmt.Errorf("marshal label event: %w", err)
		}
		events = append(events, RawEventData{
			GitlabEventID: le.ID,
			ProjectID:     int32(projectID),
			IssueIID:      int32(issue.IID),
			EventType:     "label_event",
			RawPayload:    payload,
		})
	}

	// Fetch notes
	notes, err := e.gitlab.ListNotes(ctx, projectID, issue.IID)
	if err != nil {
		return nil, fmt.Errorf("list notes for issue %d: %w", issue.IID, err)
	}

	for _, n := range notes {
		payload, err := json.Marshal(n)
		if err != nil {
			return nil, fmt.Errorf("marshal note: %w", err)
		}
		events = append(events, RawEventData{
			GitlabEventID: n.ID,
			ProjectID:     int32(projectID),
			IssueIID:      int32(issue.IID),
			EventType:     "note",
			RawPayload:    payload,
		})
	}

	return events, nil
}

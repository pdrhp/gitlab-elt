package transformer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/pdrhp/gitlab-elt/internal/domain"
	"github.com/pdrhp/gitlab-elt/internal/mapper"
	"github.com/pdrhp/gitlab-elt/internal/repository"
)

// Service transforms Bronze raw_events into Silver tables.
type transformerQueries interface {
	ListUnprocessedRawEvents(ctx context.Context, limit int32) ([]repository.RawEvent, error)
	MarkRawEventsProcessedBatch(ctx context.Context, ids []int64) error
	UpsertUnknownLabel(ctx context.Context, label string) (repository.UnknownLabelsLog, error)
	GetLastIssueEvent(ctx context.Context, issueID int32) (repository.IssueEvent, error)
	InsertIssueEvent(ctx context.Context, arg repository.InsertIssueEventParams) (repository.IssueEvent, error)
	UpdateIssueCanonicalState(ctx context.Context, arg repository.UpdateIssueCanonicalStateParams) error
	InsertIssueComment(ctx context.Context, arg repository.InsertIssueCommentParams) (repository.IssueComment, error)
	UpdateIssueAssignees(ctx context.Context, arg repository.UpdateIssueAssigneesParams) error
	GetIssueByProjectAndIID(ctx context.Context, arg repository.GetIssueByProjectAndIIDParams) (repository.Issue, error)
	GetRawIssueByProjectAndIID(ctx context.Context, arg repository.GetRawIssueByProjectAndIIDParams) (repository.RawIssue, error)
	UpsertIssue(ctx context.Context, arg repository.UpsertIssueParams) (repository.Issue, error)
	GetIssueByID(ctx context.Context, id int32) (repository.Issue, error)
	UpdateIssueMetadata(ctx context.Context, arg repository.UpdateIssueMetadataParams) error
}

type Service struct {
	queries   transformerQueries
	mapper    *mapper.Mapper
	batchSize int32
	metrics   domain.MetricsRecorder
	tracer    trace.Tracer
}

// NewService creates a new Transformer Service.
func NewService(queries transformerQueries, m *mapper.Mapper, metrics domain.MetricsRecorder, tracer trace.Tracer) *Service {
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("github.com/pdrhp/gitlab-elt/internal/transformer")
	}
	return &Service{
		queries:   queries,
		mapper:    m,
		batchSize: 500,
		metrics:   metrics,
		tracer:    tracer,
	}
}

// TransformBatch processes a batch of unprocessed raw events → Silver.
// Returns the number of events processed.
func (s *Service) TransformBatch(ctx context.Context) (int, error) {
	ctx, span := s.tracer.Start(ctx, "transformer.TransformBatch",
		trace.WithAttributes(
			attribute.Int("batch.size", int(s.batchSize)),
		),
	)
	defer span.End()

	events, err := s.queries.ListUnprocessedRawEvents(ctx, s.batchSize)
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("list unprocessed events: %w", err)
	}

	if len(events) == 0 {
		return 0, nil
	}

	span.SetAttributes(attribute.Int("events.fetched", len(events)))
	slog.Info("transformer: processing batch", "count", len(events))

	processed := 0
	processedIDs := make([]int64, 0, len(events))

	for _, raw := range events {
		err := s.transformEvent(ctx, raw)
		if err != nil {
			slog.Error("transformer: failed to transform event",
				"raw_event_id", raw.ID,
				"event_type", raw.EventType,
				"project_id", raw.ProjectID,
				"error", err,
			)
			continue
		}
		processedIDs = append(processedIDs, raw.ID)
		processed++
	}

	// Mark events as processed in batch
	if len(processedIDs) > 0 {
		if err := s.queries.MarkRawEventsProcessedBatch(ctx, processedIDs); err != nil {
			return processed, fmt.Errorf("mark events processed: %w", err)
		}
	}

	slog.Info("transformer: batch complete",
		"processed", processed,
		"total", len(events),
	)

	span.SetAttributes(attribute.Int("events.processed", processed))
	return processed, nil
}

// TransformAll processes all unprocessed raw events in batches until none remain.
func (s *Service) TransformAll(ctx context.Context) (int, error) {
	ctx, span := s.tracer.Start(ctx, "transformer.TransformAll")
	defer span.End()

	start := time.Now()
	status := "success"
	defer func() {
		if s.metrics != nil {
			s.metrics.RecordSyncRun("transform", status, time.Since(start))
		}
	}()
	total := 0
	for {
		n, err := s.TransformBatch(ctx)
		if err != nil {
			status = "error"
			span.RecordError(err)
			return total, err
		}
		total += n
		if n == 0 {
			break
		}
	}

	span.SetAttributes(attribute.Int("events.total", total))
	slog.Info("transformer: all batches complete", "total_processed", total)
	return total, nil
}

// transformEvent transforms a single raw event into Silver tables.
func (s *Service) transformEvent(ctx context.Context, raw repository.RawEvent) error {
	switch raw.EventType {
	case "label_event":
		return s.transformLabelEvent(ctx, raw)
	case "note":
		return s.transformNote(ctx, raw)
	default:
		slog.Warn("transformer: unknown event type", "event_type", raw.EventType, "raw_id", raw.ID)
		return nil
	}
}

// transformLabelEvent processes a label event from Bronze → Silver.
func (s *Service) transformLabelEvent(ctx context.Context, raw repository.RawEvent) error {
	le, err := parseLabelEventPayload(raw.RawPayload)
	if err != nil {
		return s.transformError(raw.ProjectID, raw.EventType, fmt.Errorf("parse label event: %w", err))
	}

	// Determine label added/removed
	var labelAdded, labelRemoved string
	if le.Action == "add" {
		labelAdded = le.Label.Name
	} else {
		labelRemoved = le.Label.Name
	}

	// Determine canonical state
	canonicalState := determineCanonicalState(s.mapper, le.Action, labelAdded, labelRemoved)

	// Handle metadata labels - update issue metadata cache
	if canonicalState == "" && le.Action == "add" {
		// Check if this is a metadata label
		metadataKey := s.mapper.MetadataKey(labelAdded)
		if metadataKey != "" {
			// Ensure issue exists
			issue, err := s.ensureIssue(ctx, raw.ProjectID, raw.IssueIid)
			if err != nil {
				return s.transformError(raw.ProjectID, raw.EventType, fmt.Errorf("ensure issue for metadata: %w", err))
			}

			// Update issue metadata (accumulate labels)
			err = s.updateIssueMetadata(ctx, issue.ID, metadataKey, labelAdded)
			if err != nil {
				slog.Error("transformer: failed to update metadata",
					"issue_id", issue.ID,
					"label", labelAdded,
					"error", err,
				)
			}
		}
		return nil
	}

	unknownLabelOnly := false
	// Log unknown labels (only if label is truly unmapped, not for remove events of mapped labels)
	if canonicalState == "UNKNOWN" && le.Action == "add" {
		// Only log as unknown if it's an "add" action - "remove" actions return UNKNOWN
		// because we don't know the new state, but the label itself might be mapped
		if _, labelType := s.mapper.MapLabel(labelAdded); labelType == mapper.LabelTypeUnknown {
			unknownLabelOnly = true
			s.recordTransformResult(raw.ProjectID, raw.EventType, "unknown_label")
			if _, err := s.queries.UpsertUnknownLabel(ctx, labelAdded); err != nil {
				slog.Error("transformer: failed to log unknown label",
					"label", labelAdded,
					"error", err,
				)
			}
		}
	}

	// Only persist "add" events as state transitions (remove events are informational)
	if le.Action == "remove" {
		return nil
	}

	// Ensure issue exists in Silver
	issue, err := s.ensureIssue(ctx, raw.ProjectID, raw.IssueIid)
	if err != nil {
		return s.transformError(raw.ProjectID, raw.EventType, fmt.Errorf("ensure issue: %w", err))
	}

	// Get last event for edge case detection
	lastEvent, _ := s.queries.GetLastIssueEvent(ctx, issue.ID)

	// Edge case detection
	var isNoise bool
	var isFalseMovement bool
	var cycleCount int32

	if lastEvent.ID != 0 {
		isNoise = detectNervousFinger(s.mapper, &lastEvent, canonicalState, le.CreatedAt)
		isFalseMovement = detectFalseMovement(s.mapper, lastEvent.MappedCanonicalState, labelAdded)
		cycleCount = calculateCycleCount(s.mapper, &lastEvent, canonicalState)
	}

	if isFalseMovement {
		slog.Debug("transformer: false movement detected",
			"project_id", raw.ProjectID,
			"issue_iid", raw.IssueIid,
			"from_state", lastEvent.MappedCanonicalState,
			"label_added", labelAdded,
		)
	}

	// Insert issue event into Silver
	_, err = s.queries.InsertIssueEvent(ctx, repository.InsertIssueEventParams{
		GitlabEventID:        pgtype.Int8{Int64: le.ID, Valid: true},
		IssueID:              issue.ID,
		ProjectID:            raw.ProjectID,
		IssueIid:             raw.IssueIid,
		AuthorName:           pgtype.Text{String: le.User.Username, Valid: true},
		RawLabelAdded:        pgtype.Text{String: labelAdded, Valid: labelAdded != ""},
		RawLabelRemoved:      pgtype.Text{String: labelRemoved, Valid: labelRemoved != ""},
		MappedCanonicalState: canonicalState,
		EventTimestamp:       pgtype.Timestamptz{Time: le.CreatedAt, Valid: true},
		IsNoise:              isNoise,
		CycleCount:           pgtype.Int4{Int32: cycleCount, Valid: true},
	})
	if err != nil {
		if isInsertNoRowsConflict(err) {
			slog.Debug("transformer: duplicate issue event ignored",
				"raw_event_id", raw.ID,
				"gitlab_event_id", le.ID,
				"project_id", raw.ProjectID,
			)
			return nil
		}
		return s.transformError(raw.ProjectID, raw.EventType, fmt.Errorf("insert issue event: %w", err))
	}
	if !unknownLabelOnly {
		s.recordTransformResult(raw.ProjectID, raw.EventType, "success")
	}

	// Update issue canonical state cache
	if canonicalState != "UNKNOWN" {
		err = s.queries.UpdateIssueCanonicalState(ctx, repository.UpdateIssueCanonicalStateParams{
			CurrentCanonicalState: pgtype.Text{String: canonicalState, Valid: true},
			ID:                    issue.ID,
		})
		if err != nil {
			slog.Error("transformer: failed to update issue canonical state",
				"issue_id", issue.ID,
				"state", canonicalState,
				"error", err,
			)
		}
	}

	return nil
}

// transformNote processes a note from Bronze → Silver.
func (s *Service) transformNote(ctx context.Context, raw repository.RawEvent) error {
	note, err := parseNotePayload(raw.RawPayload)
	if err != nil {
		return s.transformError(raw.ProjectID, raw.EventType, fmt.Errorf("parse note: %w", err))
	}

	// Check if this is a system note about assignee changes
	if note.System {
		assigneeChanges := parseAssigneeChangeFromSystemNote(note.Body)
		if len(assigneeChanges) > 0 {
			if err := s.processAssigneeChanges(ctx, raw, note, assigneeChanges); err != nil {
				return s.transformError(raw.ProjectID, raw.EventType, err)
			}
			s.recordTransformResult(raw.ProjectID, raw.EventType, "success")
			return nil
		}
		// Skip other system notes
		return nil
	}

	// Ensure issue exists in Silver
	issue, err := s.ensureIssue(ctx, raw.ProjectID, raw.IssueIid)
	if err != nil {
		return s.transformError(raw.ProjectID, raw.EventType, fmt.Errorf("ensure issue: %w", err))
	}

	// Insert comment into Silver
	_, err = s.queries.InsertIssueComment(ctx, repository.InsertIssueCommentParams{
		GitlabNoteID:     note.ID,
		IssueID:          issue.ID,
		AuthorName:       pgtype.Text{String: note.Author.Username, Valid: true},
		Body:             pgtype.Text{String: note.Body, Valid: true},
		CommentTimestamp: pgtype.Timestamptz{Time: note.CreatedAt, Valid: true},
	})
	if err != nil {
		if isInsertNoRowsConflict(err) {
			slog.Debug("transformer: duplicate issue comment ignored",
				"raw_event_id", raw.ID,
				"gitlab_note_id", note.ID,
				"project_id", raw.ProjectID,
			)
			return nil
		}
		return s.transformError(raw.ProjectID, raw.EventType, fmt.Errorf("insert issue comment: %w", err))
	}
	s.recordTransformResult(raw.ProjectID, raw.EventType, "success")

	return nil
}

// processAssigneeChanges processes assignee changes from system notes and updates the issue history.
func (s *Service) processAssigneeChanges(ctx context.Context, raw repository.RawEvent, note domain.GitlabNote, changes []AssigneeChange) error {
	// Ensure issue exists in Silver
	issue, err := s.ensureIssue(ctx, raw.ProjectID, raw.IssueIid)
	if err != nil {
		return fmt.Errorf("ensure issue for assignee changes: %w", err)
	}

	// Parse current assignee history
	history, err := parseAssigneeHistory(issue.Assignees)
	if err != nil {
		slog.Warn("transformer: failed to parse assignee history, starting fresh",
			"issue_id", issue.ID,
			"error", err,
		)
		history = &AssigneeHistory{
			Current: []string{},
			History: []AssigneeHistoryEntry{},
		}
	}

	// Process each change
	modified := false
	for _, change := range changes {
		changeModified := applyAssigneeChange(history, change, note.CreatedAt)
		if changeModified {
			modified = true
		}
	}

	if !modified {
		return nil
	}

	// Serialize updated history
	assigneesJSON, err := serializeAssigneeHistory(history)
	if err != nil {
		return fmt.Errorf("serialize assignee history: %w", err)
	}

	// Update issue with new assignee history
	err = s.queries.UpdateIssueAssignees(ctx, repository.UpdateIssueAssigneesParams{
		Assignees: assigneesJSON,
		ID:        issue.ID,
	})
	if err != nil {
		return fmt.Errorf("update issue assignees: %w", err)
	}

	slog.Debug("transformer: updated assignee history from system note",
		"issue_id", issue.ID,
		"changes_count", len(changes),
		"current_count", len(history.Current),
		"history_count", len(history.History),
	)

	return nil
}

// ensureIssue looks up or creates a stub issue in Silver.
// Uses metadata from raw_issues if available, otherwise creates a placeholder.
func (s *Service) ensureIssue(ctx context.Context, projectID int32, issueIID int32) (repository.Issue, error) {
	issue, err := s.queries.GetIssueByProjectAndIID(ctx, repository.GetIssueByProjectAndIIDParams{
		ProjectID: projectID,
		Iid:       issueIID,
	})
	if err == nil {
		return issue, nil
	}

	// Issue doesn't exist yet → create with metadata from raw_issues or placeholder
	title := fmt.Sprintf("Issue #%d", issueIID)
	var gitlabIssueID int32 = 0
	createdAt := time.Now().UTC()

	// Try to get metadata from raw_issues
	rawIssue, err := s.queries.GetRawIssueByProjectAndIID(ctx, repository.GetRawIssueByProjectAndIIDParams{
		ProjectID: projectID,
		Iid:       issueIID,
	})
	if err == nil {
		if rawIssue.Title != "" {
			title = rawIssue.Title
		}
		if rawIssue.GitlabIssueID > 0 {
			gitlabIssueID = int32(rawIssue.GitlabIssueID)
		}
		// Extract created_at from the raw payload JSON
		if len(rawIssue.RawPayload) > 0 {
			var issueData domain.GitlabIssue
			if err := json.Unmarshal(rawIssue.RawPayload, &issueData); err == nil {
				createdAt = issueData.CreatedAt
			}
		}
	}

	issue, err = s.queries.UpsertIssue(ctx, repository.UpsertIssueParams{
		GitlabIssueID:   gitlabIssueID,
		ProjectID:       projectID,
		Iid:             issueIID,
		Title:           pgtype.Text{String: title, Valid: true},
		GitlabCreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
	})
	if err != nil {
		return repository.Issue{}, fmt.Errorf("upsert stub issue: %w", err)
	}
	return issue, nil
}

// updateIssueMetadata accumulates a metadata label in the issue's metadata_labels JSONB field.
func (s *Service) updateIssueMetadata(ctx context.Context, issueID int32, metadataKey, labelValue string) error {
	// Get current metadata
	issue, err := s.queries.GetIssueByID(ctx, issueID)
	if err != nil {
		return fmt.Errorf("get issue for metadata update: %w", err)
	}

	// Parse current metadata
	metadata := make(map[string][]string)
	if len(issue.MetadataLabels) > 0 {
		if err := json.Unmarshal(issue.MetadataLabels, &metadata); err != nil {
			slog.Warn("transformer: failed to parse existing metadata, starting fresh",
				"issue_id", issueID,
				"error", err,
			)
			metadata = make(map[string][]string)
		}
	}

	// Accumulate new label (avoid duplicates)
	found := false
	for _, existing := range metadata[metadataKey] {
		if existing == labelValue {
			found = true
			break
		}
	}
	if !found {
		metadata[metadataKey] = append(metadata[metadataKey], labelValue)
	}

	// Serialize and update
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	return s.queries.UpdateIssueMetadata(ctx, repository.UpdateIssueMetadataParams{
		MetadataLabels: metadataJSON,
		ID:             issueID,
	})
}

func (s *Service) transformError(projectID int32, eventType string, err error) error {
	if err == nil {
		return nil
	}
	s.recordTransformResult(projectID, eventType, "error")
	return err
}

func (s *Service) recordTransformResult(projectID int32, eventType, status string) {
	if s.metrics == nil {
		return
	}
	s.metrics.RecordTransformResult(strconv.Itoa(int(projectID)), eventType, status)
}

// parseLabelEventPayload parses a raw_event payload into a GitlabLabelEvent.
func parseLabelEventPayload(payload []byte) (domain.GitlabLabelEvent, error) {
	var le domain.GitlabLabelEvent
	err := json.Unmarshal(payload, &le)
	return le, err
}

// parseNotePayload parses a raw_event payload into a GitlabNote.
func parseNotePayload(payload []byte) (domain.GitlabNote, error) {
	var note domain.GitlabNote
	err := json.Unmarshal(payload, &note)
	return note, err
}

// determineCanonicalState determines the canonical state from a label event.
// Returns:
//   - canonical state string (e.g., "IN_PROGRESS") for state label adds
//   - "UNKNOWN" for unrecognized state labels
//   - "" for metadata labels or remove-only events (skip these)
func determineCanonicalState(m *mapper.Mapper, action, labelAdded, labelRemoved string) string {
	var label string
	if action == "add" {
		label = labelAdded
	} else {
		label = labelRemoved
	}

	if label == "" {
		return ""
	}

	state, labelType := m.MapLabel(label)

	switch labelType {
	case mapper.LabelTypeState:
		if action == "add" {
			return state
		}
		// Remove of a state label — we don't know the new state
		return "UNKNOWN"
	case mapper.LabelTypeMetadata:
		// Not a state transition
		return ""
	default:
		// Unknown label
		if action == "add" {
			return "UNKNOWN"
		}
		return "UNKNOWN"
	}
}

// DetermineCanonicalStateExported is exported for integration testing.
// In production code, use the unexported determineCanonicalState.
func DetermineCanonicalStateExported(m *mapper.Mapper, action, labelAdded, labelRemoved string) string {
	return determineCanonicalState(m, action, labelAdded, labelRemoved)
}

// isInsertNoRowsConflict detects sqlc "no rows" from INSERT ... ON CONFLICT DO NOTHING RETURNING.
func isInsertNoRowsConflict(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

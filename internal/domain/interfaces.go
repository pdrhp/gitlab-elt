package domain

import (
	"context"
	"time"
)

// =============================================================================
// GitLab API Client Interface
// =============================================================================
//
// GitlabClient defines the contract for interacting with the GitLab REST API.
// All methods return domain models that map directly to GitLab API responses.
//
// Base URL format: https://gitlab.example.com/api/v4
// Authentication: PRIVATE-TOKEN header
//
// API Documentation: https://docs.gitlab.com/ee/api/

// GitlabClient defines the contract for interacting with the GitLab API.
type GitlabClient interface {
	// ListGroupProjects retrieves all projects within a GitLab group.
	//
	// API Endpoint: GET /groups/:id/projects
	// Documentation: https://docs.gitlab.com/ee/api/groups.html#list-a-groups-projects
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - groupID: The ID of the GitLab group (e.g., 123)
	//
	// Returns:
	//   - []GitlabProject: Array of projects in the group
	//   - error: Any error encountered during the API call
	//
	// Example:
	//
	//	projects, err := client.ListGroupProjects(ctx, 42)
	//	if err != nil {
	//	    return err
	//	}
	//	for _, p := range projects {
	//	    fmt.Printf("Project: %s (ID: %d)\n", p.Name, p.ID)
	//	}
	//
	// Response Model: See GitlabProject in models.go
	ListGroupProjects(ctx context.Context, groupID int) ([]GitlabProject, error)

	// ListIssues retrieves issues from a project, optionally filtered by update time.
	//
	// API Endpoint: GET /projects/:id/issues?updated_after=:timestamp
	// Documentation: https://docs.gitlab.com/ee/api/issues.html#list-project-issues
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - projectID: The ID of the project
	//   - updatedAfter: Only return issues updated after this timestamp (for incremental sync)
	//
	// Returns:
	//   - []GitlabIssue: Array of issues matching the criteria
	//   - error: Any error encountered during the API call
	//
	// Note: This method is used for incremental synchronization. The updatedAfter
	// parameter should be set to the last sync timestamp to fetch only changed issues.
	//
	// Example:
	//
	//	lastSync := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	//	issues, err := client.ListIssues(ctx, 123, lastSync)
	//	if err != nil {
	//	    return err
	//	}
	//	for _, issue := range issues {
	//	    fmt.Printf("Issue #%d: %s (updated: %s)\n", issue.IID, issue.Title, issue.UpdatedAt)
	//	}
	//
	// Response Model: See GitlabIssue in models.go
	ListIssues(ctx context.Context, projectID int, updatedAfter time.Time) ([]GitlabIssue, error)

	// ListLabelEvents retrieves all label events (add/remove label actions) for an issue.
	//
	// API Endpoint: GET /projects/:id/issues/:issue_iid/resource_label_events
	// Documentation: https://docs.gitlab.com/ee/api/resource_label_events.html
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - projectID: The ID of the project
	//   - issueIID: The internal ID of the issue (not the global ID)
	//
	// Returns:
	//   - []GitlabLabelEvent: Array of label events in chronological order
	//   - error: Any error encountered during the API call
	//
	// Note: Label events track workflow state transitions. Each event represents
	// either adding or removing a label from an issue. The action field will be
	// either "add" or "remove".
	//
	// Example:
	//
	//	events, err := client.ListLabelEvents(ctx, 123, 42)
	//	if err != nil {
	//	    return err
	//	}
	//	for _, event := range events {
	//	    fmt.Printf("%s: %s %sed label '%s'\n",
	//	        event.CreatedAt, event.User.Username, event.Action, event.Label.Name)
	//	}
	//
	// Response Model: See GitlabLabelEvent in models.go
	ListLabelEvents(ctx context.Context, projectID int, issueIID int) ([]GitlabLabelEvent, error)

	// ListNotes retrieves all notes (comments) for an issue.
	//
	// API Endpoint: GET /projects/:id/issues/:issue_iid/notes
	// Documentation: https://docs.gitlab.com/ee/api/notes.html#list-project-issue-notes
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - projectID: The ID of the project
	//   - issueIID: The internal ID of the issue (not the global ID)
	//
	// Returns:
	//   - []GitlabNote: Array of notes in chronological order
	//   - error: Any error encountered during the API call
	//
	// Note: Notes include both user comments and system-generated messages
	// (e.g., "user closed this issue"). System notes have the System field set to true.
	//
	// Example:
	//
	//	notes, err := client.ListNotes(ctx, 123, 42)
	//	if err != nil {
	//	    return err
	//	}
	//	for _, note := range notes {
	//	    if note.System {
	//	        fmt.Printf("[System] %s: %s\n", note.CreatedAt, note.Body)
	//	    } else {
	//	        fmt.Printf("[%s] %s: %s\n", note.Author.Username, note.CreatedAt, note.Body)
	//	    }
	//	}
	//
	// Response Model: See GitlabNote in models.go
	ListNotes(ctx context.Context, projectID int, issueIID int) ([]GitlabNote, error)
}

// MetricsRecorder defines the observability contract used throughout the worker.
// Implementations should forward these helper calls to Prometheus collectors with
// the metric names and labels defined in the observability plan to preserve a
// stable metrics surface for operators.
type MetricsRecorder interface {
	RecordSyncRun(stage, status string, duration time.Duration)
	RecordGitLabRequest(endpoint, status string, duration time.Duration)
	RecordRawEventIngested(projectID, eventType string, count int)
	RecordTransformResult(projectID, eventType, status string)
	RecordLastSuccessfulSync(projectID, stage string, ts time.Time)
	SetProjectsMonitored(n float64)
}

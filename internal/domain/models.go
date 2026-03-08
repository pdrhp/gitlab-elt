package domain

import "time"

// =============================================================================
// GitLab API Endpoints Documentation
// =============================================================================
//
// This package contains Go structs that map to GitLab API v4 responses.
// All models are based on official GitLab API documentation.
//
// Base URL: https://docs.gitlab.com/ee/api/
// Authentication: PRIVATE-TOKEN header with personal access token
//
// =============================================================================

// =============================================================================
// ENDPOINT: GET /groups/:id/projects
// =============================================================================
// Lists all projects in a GitLab group.
//
// Documentation: https://docs.gitlab.com/ee/api/groups.html#list-a-groups-projects
//
// Path Parameters:
//   - id (integer/string): The ID or URL-encoded path of the group
//
// Query Parameters:
//   - archived (boolean): Limit by archived status
//   - visibility (string): Limit by visibility (public, internal, private)
//   - order_by (string): Return projects ordered by id, name, path, etc.
//   - sort (string): Return projects sorted in asc or desc order
//   - search (string): Return list of projects matching the search criteria
//   - per_page (integer): Number of results to show per page (default 20, max 100)
//   - page (integer): Page number for pagination
//
// Response: JSON array of GitlabProject objects
//
// Example Response:
//
//	[
//	  {
//	    "id": 8,
//	    "description": "Shared project for Html5 Boilerplate",
//	    "name": "Html5 Boilerplate",
//	    "name_with_namespace": "H5bp / Html5 Boilerplate",
//	    "path": "html5-boilerplate",
//	    "path_with_namespace": "h5bp/html5-boilerplate",
//	    "created_at": "2020-04-27T06:13:22.642Z",
//	    "default_branch": "main",
//	    "web_url": "https://gitlab.com/h5bp/html5-boilerplate",
//	    "avatar_url": null,
//	    "star_count": 0,
//	    "forks_count": 4,
//	    "last_activity_at": "2020-04-27T06:13:22.642Z",
//	    "namespace": {
//	      "id": 28,
//	      "name": "H5bp",
//	      "path": "h5bp",
//	      "kind": "group",
//	      "full_path": "h5bp"
//	    }
//	  }
//	]

// GitlabProject represents a project from the GitLab API.
// Maps to the response from GET /groups/:id/projects and GET /projects/:id
//
// API Documentation: https://docs.gitlab.com/ee/api/projects.html
type GitlabProject struct {
	ID                int    `json:"id"`                  // Project ID
	Name              string `json:"name"`                // Project name
	PathWithNamespace string `json:"path_with_namespace"` // Full path (e.g., "group/project")
}

// =============================================================================
// ENDPOINT: GET /projects/:id/issues
// =============================================================================
// Lists all issues in a project. Supports filtering by updated_after for incremental sync.
//
// Documentation: https://docs.gitlab.com/ee/api/issues.html#list-project-issues
//
// Path Parameters:
//   - id (integer/string): The ID or URL-encoded path of the project
//
// Query Parameters:
//   - updated_after (string, ISO8601): Return issues updated after the given timestamp
//   - updated_before (string, ISO8601): Return issues updated before the given timestamp
//   - state (string): Return all issues or just those that are opened or closed
//   - labels (string): Comma-separated list of label names
//   - per_page (integer): Number of results to show per page (default 20, max 100)
//   - page (integer): Page number for pagination
//   - order_by (string): Return issues ordered by created_at or updated_at
//   - sort (string): Return issues sorted in asc or desc order
//   - iids[] (array): Return only the issues having the given IID
//
// Response: JSON array of GitlabIssue objects
//
// Example Response:
//
//	[
//	  {
//	    "id": 83,
//	    "iid": 1,
//	    "project_id": 12,
//	    "title": "Add file",
//	    "description": "Add first file",
//	    "state": "opened",
//	    "created_at": "2018-01-24T06:02:15.514Z",
//	    "updated_at": "2018-02-06T12:36:23.263Z",
//	    "closed_at": null,
//	    "labels": ["bug", "enhancement"],
//	    "assignees": [
//	      {
//	        "id": 20,
//	        "name": "Ceola Deckow",
//	        "username": "sammy.collier",
//	        "state": "active",
//	        "avatar_url": "https://www.gravatar.com/avatar/..."
//	      }
//	    ],
//	    "author": {
//	      "id": 1,
//	      "name": "Administrator",
//	      "username": "root",
//	      "state": "active",
//	      "avatar_url": "https://www.gravatar.com/avatar/..."
//	    }
//	  }
//	]

// GitlabIssue represents an issue from the GitLab API.
// Maps to the response from GET /projects/:id/issues
//
// API Documentation: https://docs.gitlab.com/ee/api/issues.html
//
// Note: The 'id' field is the global issue ID across GitLab.
// The 'iid' field is the project-scoped issue number (e.g., #215).
// The 'project_id' field indicates which project the issue belongs to.
type GitlabIssue struct {
	ID        int       `json:"id"`         // Global issue ID
	IID       int       `json:"iid"`        // Project-scoped number (e.g., #215)
	ProjectID int       `json:"project_id"` // ID of the project this issue belongs to
	Title     string    `json:"title"`      // Issue title
	State     string    `json:"state"`      // Issue state: "opened" or "closed"
	Labels    []string  `json:"labels"`     // Array of label names
	CreatedAt time.Time `json:"created_at"` // When the issue was created (ISO8601)
	UpdatedAt time.Time `json:"updated_at"` // When the issue was last updated (ISO8601)
}

// =============================================================================
// ENDPOINT: GET /projects/:id/issues/:issue_iid/resource_label_events
// =============================================================================
// Lists all label events for a specific issue.
//
// Documentation: https://docs.gitlab.com/ee/api/resource_label_events.html
//
// Path Parameters:
//   - id (integer/string): The ID or URL-encoded path of the project
//   - issue_iid (integer): The internal ID of the project issue
//
// Response: JSON array of GitlabLabelEvent objects
//
// Example Response:
//
//	[
//	  {
//	    "id": 142,
//	    "user": {
//	      "id": 1,
//	      "name": "Administrator",
//	      "username": "root",
//	      "state": "active",
//	      "avatar_url": "https://www.gravatar.com/avatar/..."
//	    },
//	    "created_at": "2018-08-20T13:38:20.077Z",
//	    "resource_type": "Issue",
//	    "resource_id": 253,
//	    "label": {
//	      "id": 73,
//	      "name": "a1",
//	      "color": "#34495E",
//	      "description": ""
//	    },
//	    "action": "add"
//	  },
//	  {
//	    "id": 143,
//	    "user": {
//	      "id": 1,
//	      "name": "Administrator",
//	      "username": "root",
//	      "state": "active"
//	    },
//	    "created_at": "2018-08-20T13:38:20.077Z",
//	    "resource_type": "Issue",
//	    "resource_id": 253,
//	    "label": {
//	      "id": 74,
//	      "name": "p1",
//	      "color": "#0033CC",
//	      "description": ""
//	    },
//	    "action": "remove"
//	  }
//	]

// GitlabLabelEvent represents a resource_label_event from the GitLab API.
// Maps to the response from GET /projects/:id/issues/:issue_iid/resource_label_events
//
// API Documentation: https://docs.gitlab.com/ee/api/resource_label_events.html
//
// Note: These events track when labels are added or removed from issues.
// The 'action' field will be either "add" or "remove".
// The 'resource_type' field indicates what type of resource the event belongs to (e.g., "Issue").
type GitlabLabelEvent struct {
	ID           int64     `json:"id"`            // Event ID
	User         User      `json:"user"`          // User who performed the action
	Action       string    `json:"action"`        // Action type: "add" or "remove"
	Label        Label     `json:"label"`         // The label that was added/removed
	CreatedAt    time.Time `json:"created_at"`    // When the event occurred (ISO8601)
	ResourceType string    `json:"resource_type"` // Type of resource (e.g., "Issue")
}

// =============================================================================
// ENDPOINT: GET /projects/:id/issues/:issue_iid/notes
// =============================================================================
// Lists all notes (comments) for a specific issue.
//
// Documentation: https://docs.gitlab.com/ee/api/notes.html#list-project-issue-notes
//
// Path Parameters:
//   - id (integer/string): The ID or URL-encoded path of the project
//   - issue_iid (integer): The internal ID of the project issue
//
// Query Parameters:
//   - sort (string): Sort in asc or desc order (default: desc)
//   - order_by (string): Order by created_at or updated_at (default: created_at)
//   - per_page (integer): Number of results to show per page (default 20, max 100)
//   - page (integer): Page number for pagination
//
// Response: JSON array of GitlabNote objects
//
// Example Response:
//
//	[
//	  {
//	    "id": 302,
//	    "body": "closed",
//	    "author": {
//	      "id": 1,
//	      "username": "pipin",
//	      "email": "admin@example.com",
//	      "name": "Pip",
//	      "state": "active"
//	    },
//	    "created_at": "2013-10-02T09:22:45Z",
//	    "updated_at": "2013-10-02T10:22:45Z",
//	    "system": true,
//	    "noteable_id": 377,
//	    "noteable_type": "Issue",
//	    "project_id": 5,
//	    "noteable_iid": 377,
//	    "resolvable": false,
//	    "confidential": false,
//	    "internal": false
//	  },
//	  {
//	    "id": 305,
//	    "body": "Text of the comment\r\n",
//	    "author": {
//	      "id": 1,
//	      "username": "pipin",
//	      "email": "admin@example.com",
//	      "name": "Pip",
//	      "state": "active"
//	    },
//	    "created_at": "2013-10-02T09:56:03Z",
//	    "updated_at": "2013-10-02T09:56:03Z",
//	    "system": false,
//	    "noteable_id": 121,
//	    "noteable_type": "Issue",
//	    "project_id": 5,
//	    "noteable_iid": 121
//	  }
//	]

// GitlabNote represents a note (comment) from the GitLab API.
// Maps to the response from GET /projects/:id/issues/:issue_iid/notes
//
// API Documentation: https://docs.gitlab.com/ee/api/notes.html
//
// Note: The 'system' field indicates if the note was auto-generated by GitLab
// (e.g., "user closed this issue") versus user-written comments.
type GitlabNote struct {
	ID        int64     `json:"id"`         // Note ID
	Body      string    `json:"body"`       // Content of the note/comment
	Author    User      `json:"author"`     // User who wrote the note
	CreatedAt time.Time `json:"created_at"` // When the note was created (ISO8601)
	System    bool      `json:"system"`     // True if auto-generated by GitLab
}

// =============================================================================
// Supporting Types (User, Label)
// =============================================================================
//
// These types appear as nested objects in multiple API responses.

// User represents a GitLab user.
// Appears in: GitlabLabelEvent, GitlabNote, GitlabIssue (as author/assignees)
//
// API Documentation: https://docs.gitlab.com/ee/api/users.html
type User struct {
	ID       int    `json:"id"`       // User ID
	Username string `json:"username"` // Username (login)
	Name     string `json:"name"`     // Display name
}

// Label represents a GitLab label.
// Appears in: GitlabLabelEvent
//
// API Documentation: https://docs.gitlab.com/ee/api/labels.html
type Label struct {
	ID   int    `json:"id"`   // Label ID
	Name string `json:"name"` // Label name (e.g., "bug", "enhancement")
}

// =============================================================================
// ENDPOINT: GET /projects/:id/issues/:issue_iid/events
// =============================================================================
// Lists all events for a specific issue, including assignee changes, title changes,
// and other issue updates.
//
// Documentation: https://docs.gitlab.com/ee/api/events.html#list-project-issue-events
//
// Path Parameters:
//   - id (integer/string): The ID or URL-encoded path of the project
//   - issue_iid (integer): The internal ID of the project issue
//
// Response: JSON array of GitlabIssueEvent objects
//
// Example Response:
//
//	[
//	  {
//	    "id": 1001,
//	    "project_id": 1,
//	    "action_name": "assigned",
//	    "target_id": 253,
//	    "target_iid": 1,
//	    "target_type": "Issue",
//	    "author_id": 1,
//	    "author": {
//	      "id": 1,
//	      "name": "Administrator",
//	      "username": "root"
//	    },
//	    "assignee": {
//	      "id": 2,
//	      "name": "John Doe",
//	      "username": "john.doe"
//	    },
//	    "created_at": "2018-08-20T13:38:20.077Z"
//	  }
//	]

// GitlabIssueEvent represents an issue event from the GitLab API.
// Maps to the response from GET /projects/:id/issues/:issue_iid/events
//
// API Documentation: https://docs.gitlab.com/ee/api/events.html
type GitlabIssueEvent struct {
	ID         int64     `json:"id"`          // Event ID
	ProjectID  int       `json:"project_id"`  // Project ID
	ActionName string    `json:"action_name"` // Action type: "assigned", "unassigned", etc.
	TargetID   int       `json:"target_id"`   // Target resource ID
	TargetIID  int       `json:"target_iid"`  // Target resource IID
	TargetType string    `json:"target_type"` // Type of target (e.g., "Issue")
	AuthorID   int       `json:"author_id"`   // Author user ID
	Author     User      `json:"author"`      // Author who performed the action
	Assignee   *User     `json:"assignee"`    // Assignee (for assign/unassign events)
	CreatedAt  time.Time `json:"created_at"`  // When the event occurred
}

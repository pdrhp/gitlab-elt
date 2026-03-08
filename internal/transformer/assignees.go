package transformer

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// AssigneeHistory represents the structure stored in issues.assignees JSONB column.
type AssigneeHistory struct {
	Current []string               `json:"current"` // Current assignees (usernames)
	History []AssigneeHistoryEntry `json:"history"` // Historical assignments
}

// AssigneeHistoryEntry represents a single assignment period.
type AssigneeHistoryEntry struct {
	Username        string     `json:"username"`         // Username of assignee
	AssignedAt      time.Time  `json:"assigned_at"`      // When assigned
	UnassignedAt    *time.Time `json:"unassigned_at"`    // When unassigned (nil = still assigned)
	DurationSeconds *int64     `json:"duration_seconds"` // Duration in seconds (nil if still assigned)
}

// AssigneeChange represents a single assignee change parsed from a system note.
type AssigneeChange struct {
	Username string // Username affected
	Action   string // "assigned" or "unassigned"
}

// Regex patterns for parsing assignee system notes
var (
	// assigned to @username
	assignedToPattern = regexp.MustCompile(`assigned to @(\w+)`)
	// unassigned @username
	unassignedPattern = regexp.MustCompile(`unassigned @(\w+)`)
)

// parseAssigneeHistory parses the JSONB assignees column into a struct.
// Returns empty history if JSON is nil/empty.
func parseAssigneeHistory(data []byte) (*AssigneeHistory, error) {
	if len(data) == 0 || string(data) == "null" {
		return &AssigneeHistory{
			Current: []string{},
			History: []AssigneeHistoryEntry{},
		}, nil
	}

	var history AssigneeHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, err
	}

	// Ensure arrays are initialized
	if history.Current == nil {
		history.Current = []string{}
	}
	if history.History == nil {
		history.History = []AssigneeHistoryEntry{}
	}

	return &history, nil
}

// serializeAssigneeHistory serializes the assignee history to JSON.
func serializeAssigneeHistory(history *AssigneeHistory) ([]byte, error) {
	return json.Marshal(history)
}

// parseAssigneeChangeFromSystemNote parses assignee changes from system note body.
// Returns a list of changes (can have both assigned and unassigned in one note).
// Returns empty list if not an assignee-related system note.
func parseAssigneeChangeFromSystemNote(body string) []AssigneeChange {
	body = strings.ToLower(body)
	var changes []AssigneeChange

	// Find all "assigned to @username"
	assignedMatches := assignedToPattern.FindAllStringSubmatch(body, -1)
	for _, match := range assignedMatches {
		if len(match) > 1 {
			changes = append(changes, AssigneeChange{
				Username: match[1],
				Action:   "assigned",
			})
		}
	}

	// Find all "unassigned @username"
	unassignedMatches := unassignedPattern.FindAllStringSubmatch(body, -1)
	for _, match := range unassignedMatches {
		if len(match) > 1 {
			changes = append(changes, AssigneeChange{
				Username: match[1],
				Action:   "unassigned",
			})
		}
	}

	return changes
}

// applyAssigneeChange applies a single assignee change to the history.
// Returns true if the history was modified.
func applyAssigneeChange(history *AssigneeHistory, change AssigneeChange, timestamp time.Time) bool {
	switch change.Action {
	case "assigned":
		return handleAssigned(history, change.Username, timestamp)
	case "unassigned":
		return handleUnassigned(history, change.Username, timestamp)
	default:
		return false
	}
}

// handleAssigned processes an assignment event.
func handleAssigned(history *AssigneeHistory, username string, assignedAt time.Time) bool {
	modified := false

	// Check if already assigned
	alreadyAssigned := false
	for _, current := range history.Current {
		if current == username {
			alreadyAssigned = true
			break
		}
	}

	// Add to current if not already there
	if !alreadyAssigned {
		history.Current = append(history.Current, username)
		modified = true
	}

	// Check if there's an open (unassigned_at = nil) entry for this user
	// If so, don't create a duplicate - they were already assigned
	for i := range history.History {
		if history.History[i].Username == username && history.History[i].UnassignedAt == nil {
			// Already has an open assignment, don't create duplicate
			return modified
		}
	}

	// Add new history entry
	entry := AssigneeHistoryEntry{
		Username:   username,
		AssignedAt: assignedAt,
	}
	history.History = append(history.History, entry)

	return true
}

// handleUnassigned processes an unassignment event.
func handleUnassigned(history *AssigneeHistory, username string, unassignedAt time.Time) bool {
	modified := false

	// Remove from current assignees
	newCurrent := []string{}
	for _, current := range history.Current {
		if current != username {
			newCurrent = append(newCurrent, current)
		} else {
			modified = true
		}
	}
	history.Current = newCurrent

	// Find and close the open history entry for this user
	for i := range history.History {
		if history.History[i].Username == username && history.History[i].UnassignedAt == nil {
			history.History[i].UnassignedAt = &unassignedAt
			duration := int64(unassignedAt.Sub(history.History[i].AssignedAt).Seconds())
			history.History[i].DurationSeconds = &duration
			modified = true
			break
		}
	}

	return modified
}

// calculateAssigneeDuration calculates how long a user has been assigned.
// Returns duration in seconds. Returns 0 if user not found or still assigned.
func calculateAssigneeDuration(history *AssigneeHistory, username string, now time.Time) int64 {
	for _, entry := range history.History {
		if entry.Username == username {
			if entry.DurationSeconds != nil {
				return *entry.DurationSeconds
			}
			// Still assigned, calculate from assigned_at to now
			return int64(now.Sub(entry.AssignedAt).Seconds())
		}
	}
	return 0
}

package transformer

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pdrhp/gitlab-elt/internal/mapper"
	"github.com/pdrhp/gitlab-elt/internal/repository"
)

const nervousFingerThreshold = 15 * time.Minute

// detectNervousFinger checks if this is a rapid transition (< 15 min) that should be marked as noise.
// A transition is considered "nervous finger" if:
// - There is a previous event
// - The time delta is < 15 minutes
// - The state actually changed (different canonical states)
// - The previous state was NOT BACKLOG (quick pickup of new tasks is normal)
func detectNervousFinger(m *mapper.Mapper, lastEvent *repository.IssueEvent, currentState string, currentTime time.Time) bool {
	if lastEvent == nil {
		return false
	}

	// Only check if there's an actual state change
	if lastEvent.MappedCanonicalState == currentState {
		return false
	}

	// Don't flag as noise if coming from BACKLOG - quick pickup of new tasks is normal
	if lastEvent.MappedCanonicalState == "BACKLOG" {
		return false
	}

	delta := currentTime.Sub(lastEvent.EventTimestamp.Time)
	return delta < nervousFingerThreshold
}

// detectFalseMovement checks if this transition is between labels that map to the same canonical state.
// Example: "Teste HOM" → "Teste Prod" both map to QA_REVIEW, so this is a false movement.
func detectFalseMovement(m *mapper.Mapper, lastState string, newLabel string) bool {
	if lastState == "" {
		return false
	}

	newState, labelType := m.MapLabel(newLabel)

	// Only check state labels
	if labelType != mapper.LabelTypeState {
		return false
	}

	// False movement: different labels, same canonical state
	return lastState == newState
}

// calculateCycleCount increments the cycle count when returning from QA_REVIEW to IN_PROGRESS.
// A "cycle" represents a round of rework (was in QA, went back to development).
func calculateCycleCount(m *mapper.Mapper, lastEvent *repository.IssueEvent, newState string) int32 {
	if lastEvent == nil {
		return 0
	}

	baseCount := lastEvent.CycleCount.Int32

	// Cycle detected: returning from QA_REVIEW to IN_PROGRESS
	if lastEvent.MappedCanonicalState == "QA_REVIEW" && newState == "IN_PROGRESS" {
		return baseCount + 1
	}

	return baseCount
}

// Helper to create pgtype.Int4 for tests
func int32ToPgInt4(val int32) pgtype.Int4 {
	return pgtype.Int4{Int32: val, Valid: true}
}

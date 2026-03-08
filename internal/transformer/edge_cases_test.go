package transformer

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pdrhp/gitlab-elt/internal/repository"
)

func TestDetectNervousFinger_WithinThreshold(t *testing.T) {
	m := newTestMapper()

	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "IN_PROGRESS", // Not BACKLOG
		EventTimestamp:       pgtype.Timestamptz{Time: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), Valid: true},
	}

	currentTime := time.Date(2025, 6, 15, 10, 10, 0, 0, time.UTC) // 10 min later
	currentState := "QA_REVIEW"

	result := detectNervousFinger(m, lastEvent, currentState, currentTime)

	if !result {
		t.Error("expected nervous finger detected (10 min < 15 min threshold)")
	}
}

func TestDetectNervousFinger_OutsideThreshold(t *testing.T) {
	m := newTestMapper()

	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "IN_PROGRESS", // Not BACKLOG
		EventTimestamp:       pgtype.Timestamptz{Time: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), Valid: true},
	}

	currentTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC) // 30 min later
	currentState := "QA_REVIEW"

	result := detectNervousFinger(m, lastEvent, currentState, currentTime)

	if result {
		t.Error("expected no nervous finger (30 min > 15 min threshold)")
	}
}

func TestDetectNervousFinger_FromBacklog(t *testing.T) {
	m := newTestMapper()

	// BACKLOG -> IN_PROGRESS quickly should NOT be noise (normal task pickup)
	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "BACKLOG",
		EventTimestamp:       pgtype.Timestamptz{Time: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), Valid: true},
	}

	currentTime := time.Date(2025, 6, 15, 10, 5, 0, 0, time.UTC) // Only 5 min later
	currentState := "IN_PROGRESS"

	result := detectNervousFinger(m, lastEvent, currentState, currentTime)

	if result {
		t.Error("expected no nervous finger when coming from BACKLOG (quick pickup is normal)")
	}
}

func TestDetectNervousFinger_NoStateChange(t *testing.T) {
	m := newTestMapper()

	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "IN_PROGRESS",
		EventTimestamp:       pgtype.Timestamptz{Time: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), Valid: true},
	}

	currentTime := time.Date(2025, 6, 15, 10, 5, 0, 0, time.UTC) // 5 min later, same state
	currentState := "IN_PROGRESS"

	result := detectNervousFinger(m, lastEvent, currentState, currentTime)

	if result {
		t.Error("expected no nervous finger when state hasn't changed")
	}
}

func TestDetectNervousFinger_NoLastEvent(t *testing.T) {
	m := newTestMapper()

	var lastEvent *repository.IssueEvent = nil
	currentTime := time.Date(2025, 6, 15, 10, 10, 0, 0, time.UTC)
	currentState := "IN_PROGRESS"

	result := detectNervousFinger(m, lastEvent, currentState, currentTime)

	if result {
		t.Error("expected no nervous finger when there's no last event")
	}
}

func TestDetectFalseMovement_SameCanonicalState(t *testing.T) {
	m := newTestMapper()
	// "Teste HOM" and "Teste Prod" both map to QA_REVIEW
	m.AddStateMapping("Teste HOM", "QA_REVIEW")
	m.AddStateMapping("Teste Prod", "QA_REVIEW")

	lastState := "QA_REVIEW" // Previous state (from "Teste HOM")
	newLabel := "Teste Prod" // New label being added

	result := detectFalseMovement(m, lastState, newLabel)

	if !result {
		t.Error("expected false movement detected (same canonical state QA_REVIEW)")
	}
}

func TestDetectFalseMovement_DifferentCanonicalState(t *testing.T) {
	m := newTestMapper()

	lastState := "IN_PROGRESS"
	newLabel := "Teste HOM" // Maps to QA_REVIEW

	result := detectFalseMovement(m, lastState, newLabel)

	if result {
		t.Error("expected no false movement (different canonical states)")
	}
}

func TestDetectFalseMovement_EmptyLastState(t *testing.T) {
	m := newTestMapper()

	lastState := ""
	newLabel := "Teste HOM"

	result := detectFalseMovement(m, lastState, newLabel)

	if result {
		t.Error("expected no false movement when last state is empty")
	}
}

func TestDetectFalseMovement_MetadataLabel(t *testing.T) {
	m := newTestMapper()

	lastState := "QA_REVIEW"
	newLabel := "Bug" // This is a metadata label, not a state

	result := detectFalseMovement(m, lastState, newLabel)

	if result {
		t.Error("expected no false movement for metadata labels")
	}
}

func TestCalculateCycleCount_FirstQAReturn(t *testing.T) {
	m := newTestMapper()
	m.AddStateMapping("Teste HOM", "QA_REVIEW")

	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "QA_REVIEW",
		CycleCount:           pgtype.Int4{Int32: 0, Valid: true},
	}

	newState := "IN_PROGRESS" // Returning from QA to dev

	count := calculateCycleCount(m, lastEvent, newState)

	if count != 1 {
		t.Errorf("expected cycle count 1, got %d", count)
	}
}

func TestCalculateCycleCount_NoChange(t *testing.T) {
	m := newTestMapper()

	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "IN_PROGRESS",
		CycleCount:           pgtype.Int4{Int32: 2, Valid: true},
	}

	newState := "QA_REVIEW" // Normal forward transition

	count := calculateCycleCount(m, lastEvent, newState)

	if count != 2 {
		t.Errorf("expected cycle count unchanged (2), got %d", count)
	}
}

func TestCalculateCycleCount_NoLastEvent(t *testing.T) {
	m := newTestMapper()

	var lastEvent *repository.IssueEvent = nil
	newState := "IN_PROGRESS"

	count := calculateCycleCount(m, lastEvent, newState)

	if count != 0 {
		t.Errorf("expected cycle count 0 (no last event), got %d", count)
	}
}

func TestCalculateCycleCount_MultipleCycles(t *testing.T) {
	m := newTestMapper()

	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "QA_REVIEW",
		CycleCount:           pgtype.Int4{Int32: 3, Valid: true},
	}

	newState := "IN_PROGRESS" // Another return from QA

	count := calculateCycleCount(m, lastEvent, newState)

	if count != 4 {
		t.Errorf("expected cycle count 4 (incremented from 3), got %d", count)
	}
}

func TestCalculateCycleCount_NotQAReturn(t *testing.T) {
	m := newTestMapper()

	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "BACKLOG",
		CycleCount:           pgtype.Int4{Int32: 1, Valid: true},
	}

	newState := "IN_PROGRESS" // Not a return from QA

	count := calculateCycleCount(m, lastEvent, newState)

	if count != 1 {
		t.Errorf("expected cycle count unchanged (1), got %d", count)
	}
}

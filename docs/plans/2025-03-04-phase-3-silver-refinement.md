# Phase 3: Silver Refinement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement edge case detection (dedo nervoso, falso movimento, ping-pong), metadata label extraction, data quality validation, and prepare handoff documentation for the GitLab ELT Worker.

**Architecture:** Extend the existing transformer service to analyze event sequences before persisting. Edge case detection runs as a pre-processor that queries recent events for the same issue, calculates metrics (time deltas, state transitions), and marks events with is_noise/cycle_count flags. Metadata labels are accumulated during batch processing and persisted to issues.metadata_labels JSONB field.

**Tech Stack:** Go 1.22, PostgreSQL 16, sqlc, pgx, golang.org/x/text (for normalization - already used), testify (for testing)

---

## Prerequisites

Before starting Phase 3, verify Phase 1 and 2 are complete:

```bash
cd /home/pedrohenrique/projects/go/pdrhp-app-gitlab-elt
go test ./... -v 2>&1 | head -50
make build
make test
```

Expected: All tests pass, binaries compile successfully.

---

## Task 1: Edge Cases Detection Engine

**Files:**
- Create: `internal/transformer/edge_cases.go`
- Create: `internal/transformer/edge_cases_test.go`
- Modify: `internal/transformer/service.go:111-201` (transformLabelEvent method)
- Modify: `db/query/issue_events.sql:26-31` (add GetLastIssueEvent query)
- Modify: `internal/repository/` (regenerate with sqlc after query changes)

**Context:** Edge cases are common patterns in issue workflows that distort metrics:
1. **Dedo nervoso (Nervous finger):** Rapid state transitions (< 15 min) that should be ignored
2. **Falso movimento (False movement):** Transitions between labels that map to the same canonical state (e.g., "Teste HOM" → "Teste Prod" both map to QA_REVIEW)
3. **Efeito ping-pong (Ping-pong effect):** Cycles between IN_PROGRESS and QA_REVIEW indicating rework

### Step 1: Write failing tests for edge case detection

**File:** `internal/transformer/edge_cases_test.go`

```go
package transformer

import (
	"testing"
	"time"

	"github.com/pdrhp/gitlab-elt/internal/mapper"
	"github.com/pdrhp/gitlab-elt/internal/repository"
)

func TestDetectNervousFinger_WithinThreshold(t *testing.T) {
	m := newTestMapper()
	
	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "BACKLOG",
		EventTimestamp:       time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	
	currentTime := time.Date(2025, 6, 15, 10, 10, 0, 0, time.UTC) // 10 min later
	currentState := "IN_PROGRESS"
	
	result := detectNervousFinger(m, lastEvent, currentState, currentTime)
	
	if !result {
		t.Error("expected nervous finger detected (10 min < 15 min threshold)")
	}
}

func TestDetectNervousFinger_OutsideThreshold(t *testing.T) {
	m := newTestMapper()
	
	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "BACKLOG",
		EventTimestamp:       time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	
	currentTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC) // 30 min later
	currentState := "IN_PROGRESS"
	
	result := detectNervousFinger(m, lastEvent, currentState, currentTime)
	
	if result {
		t.Error("expected no nervous finger (30 min > 15 min threshold)")
	}
}

func TestDetectFalseMovement_SameCanonicalState(t *testing.T) {
	m := newTestMapper()
	// "Teste HOM" and "Teste Prod" both map to QA_REVIEW
	m.AddStateMapping("Teste HOM", "QA_REVIEW")
	m.AddStateMapping("Teste Prod", "QA_REVIEW")
	
	lastState := "QA_REVIEW" // Previous state (from "Teste HOM")
	newLabel := "Teste Prod"  // New label being added
	
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

func TestCalculateCycleCount_FirstQAReturn(t *testing.T) {
	m := newTestMapper()
	m.AddStateMapping("Teste HOM", "QA_REVIEW")
	
	lastEvent := &repository.IssueEvent{
		MappedCanonicalState: "QA_REVIEW",
		CycleCount:           0,
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
		CycleCount:           2,
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
```

### Step 2: Run tests to verify they fail

```bash
cd /home/pedrohenrique/projects/go/pdrhp-app-gitlab-elt
go test ./internal/transformer/... -run "TestDetect|TestCalculate" -v
```

**Expected:** FAIL - undefined: detectNervousFinger, detectFalseMovement, calculateCycleCount

### Step 3: Add GetLastIssueEvent query to issue_events.sql

**File:** `db/query/issue_events.sql` (add at end, after line 31)

```sql
-- name: GetLastIssueEvent :one
-- Returns the most recent non-noise event for an issue (for edge case detection).
SELECT * FROM issue_events
WHERE issue_id = $1 AND is_noise = FALSE
ORDER BY event_timestamp DESC
LIMIT 1;
```

### Step 4: Regenerate sqlc code

```bash
make sqlc-generate
```

**Expected:** SUCCESS - repository package updated with GetLastIssueEvent method

### Step 5: Implement edge case detection functions

**File:** `internal/transformer/edge_cases.go`

```go
package transformer

import (
	"time"

	"github.com/pdrhp/gitlab-elt/internal/mapper"
	"github.com/pdrhp/gitlab-elt/internal/repository"
)

const nervousFingerThreshold = 15 * time.Minute

// detectNervousFinger checks if this is a rapid transition (< 15 min) that should be marked as noise.
// A transition is considered "nervous finger" if:
// - There is a previous event
// - The time delta is < 15 minutes
// - The state actually changed (different canonical states)
func detectNervousFinger(m *mapper.Mapper, lastEvent *repository.IssueEvent, currentState string, currentTime time.Time) bool {
	if lastEvent == nil {
		return false
	}
	
	// Only check if there's an actual state change
	if lastEvent.MappedCanonicalState == currentState {
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
```

### Step 6: Run edge case tests to verify they pass

```bash
cd /home/pedrohenrique/projects/go/pdrhp-app-gitlab-elt
go test ./internal/transformer/... -run "TestDetect|TestCalculate" -v
```

**Expected:** PASS - all 7 tests pass

### Step 7: Modify transformLabelEvent to use edge case detection

**File:** `internal/transformer/service.go` - Modify transformLabelEvent method (lines 111-201)

**OLD CODE (lines 148-172):**
```go
	// Only persist "add" events as state transitions (remove events are informational)
	if le.Action == "remove" {
		return nil
	}

	// Ensure issue exists in Silver
	issue, err := s.ensureIssue(ctx, raw.ProjectID, raw.IssueIid)
	if err != nil {
		return fmt.Errorf("ensure issue: %w", err)
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
		IsNoise:              false,
		CycleCount:           pgtype.Int4{Int32: 0, Valid: true},
	})
```

**NEW CODE:**
```go
	// Only persist "add" events as state transitions (remove events are informational)
	if le.Action == "remove" {
		return nil
	}

	// Ensure issue exists in Silver
	issue, err := s.ensureIssue(ctx, raw.ProjectID, raw.IssueIid)
	if err != nil {
		return fmt.Errorf("ensure issue: %w", err)
	}

	// Get last event for edge case detection
	lastEvent, _ := s.queries.GetLastIssueEvent(ctx, issue.ID)
	
	// Edge case detection
	isNoise := detectNervousFinger(s.mapper, lastEvent, canonicalState, le.CreatedAt)
	
	// Detect false movement (log but still persist with flag)
	isFalseMovement := false
	if lastEvent != nil {
		isFalseMovement = detectFalseMovement(s.mapper, lastEvent.MappedCanonicalState, labelAdded)
	}
	
	// Calculate cycle count
	cycleCount := calculateCycleCount(s.mapper, lastEvent, canonicalState)
	
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
```

### Step 8: Run all transformer tests

```bash
cd /home/pedrohenrique/projects/go/pdrhp-app-gitlab-elt
go test ./internal/transformer/... -v
```

**Expected:** PASS - all tests pass

### Step 9: Commit

```bash
git add internal/transformer/edge_cases.go internal/transformer/edge_cases_test.go
git add internal/transformer/service.go db/query/issue_events.sql
git add internal/repository/
git commit -m "feat: implement edge case detection (dedo nervoso, falso movimento, ping-pong)

- Add detectNervousFinger for rapid transitions (< 15 min)
- Add detectFalseMovement for same-state label switches
- Add calculateCycleCount for QA_REVIEW -> IN_PROGRESS cycles
- Integrate edge case detection into transformLabelEvent
- Mark events with is_noise flag for nervous finger
- Track cycle_count for ping-pong effect detection"
```

---

## Task 2: Metadata Label Extraction

**Files:**
- Modify: `internal/transformer/service.go:111-201` (transformLabelEvent to track metadata)
- Modify: `internal/transformer/service.go:35-78` (TransformBatch to persist metadata)
- Modify: `db/query/issues.sql:26-29` (add UpdateIssueMetadata query)
- Modify: `internal/repository/` (regenerate with sqlc)

**Context:** Metadata labels (tipo, prioridade, area, complexidade) should be extracted from label events and accumulated in issues.metadata_labels JSONB field. Unlike state labels which create events, metadata labels just update the issue's metadata cache.

### Step 1: Write failing test for metadata extraction

**File:** `internal/transformer/service_test.go` (add at end, before newTestMapper)

```go
func TestMetadataLabelExtraction(t *testing.T) {
	m := newTestMapper()
	m.AddMetadataMapping("PRIORIDADE: ALTA", "prioridade")
	m.AddMetadataMapping("Backend", "area")
	
	// Simulate processing a metadata label event
	label := "PRIORIDADE: ALTA"
	metadataKey, labelType := m.MapLabel(label)
	
	if labelType != mapper.LabelTypeMetadata {
		t.Errorf("expected metadata label type, got %v", labelType)
	}
	
	if metadataKey != "prioridade" {
		t.Errorf("expected metadata key 'prioridade', got %q", metadataKey)
	}
	
	// Test accumulation logic
	metadata := make(map[string][]string)
	accumulateMetadata(metadata, "prioridade", "PRIORIDADE: ALTA")
	accumulateMetadata(metadata, "prioridade", "PRIORIDADE: MEDIA")
	accumulateMetadata(metadata, "area", "Backend")
	
	if len(metadata["prioridade"]) != 2 {
		t.Errorf("expected 2 prioridade values, got %d", len(metadata["prioridade"]))
	}
	
	if len(metadata["area"]) != 1 {
		t.Errorf("expected 1 area value, got %d", len(metadata["area"]))
	}
}
```

### Step 2: Run test to verify it fails

```bash
cd /home/pedrohenrique/projects/go/pdrhp-app-gitlab-elt
go test ./internal/transformer/... -run TestMetadataLabelExtraction -v
```

**Expected:** FAIL - undefined: accumulateMetadata

### Step 3: Add UpdateIssueMetadata query

**File:** `db/query/issues.sql` (add at end, after line 29)

```sql
-- name: UpdateIssueMetadata :exec
-- Atualiza os metadados (labels categoricas) de uma issue.
UPDATE issues SET 
    metadata_labels = $1,
    updated_at = NOW()
WHERE id = $2;
```

### Step 4: Regenerate sqlc code

```bash
make sqlc-generate
```

**Expected:** SUCCESS - repository package updated with UpdateIssueMetadata method

### Step 5: Add metadata extraction helper and modify transformLabelEvent

**File:** `internal/transformer/service.go` - Add helper function after line 355

```go
// accumulateMetadata adds a label value to the metadata map for a given key.
func accumulateMetadata(metadata map[string][]string, key, value string) {
	if key == "" || value == "" {
		return
	}
	metadata[key] = append(metadata[key], value)
}
```

### Step 6: Modify transformLabelEvent to handle metadata labels

**File:** `internal/transformer/service.go` - Modify the early return logic (around line 128-132)

**OLD CODE:**
```go
	// Skip non-state label events (metadata labels like Bug, Priority, etc.)
	if canonicalState == "" {
		return nil
	}
```

**NEW CODE:**
```go
	// Handle metadata labels - update issue metadata cache
	if canonicalState == "" && le.Action == "add" {
		// Check if this is a metadata label
		_, labelType := s.mapper.MapLabel(labelAdded)
		if labelType == mapper.LabelTypeMetadata {
			// Ensure issue exists
			issue, err := s.ensureIssue(ctx, raw.ProjectID, raw.IssueIid)
			if err != nil {
				return fmt.Errorf("ensure issue for metadata: %w", err)
			}
			
			// Get metadata key for this label
			metadataKey, _ := s.mapper.MapLabel(labelAdded)
			
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
```

### Step 7: Add updateIssueMetadata method to Service

**File:** `internal/transformer/service.go` - Add method after ensureIssue (after line 292)

```go
// updateIssueMetadata accumulates a metadata label into the issue's metadata_labels JSONB field.
func (s *Service) updateIssueMetadata(ctx context.Context, issueID int32, metadataKey, labelValue string) error {
	// Get current metadata
	issue, err := s.queries.GetIssueByGitlabID(ctx, int64(issueID))
	if err != nil {
		// Fallback: try to get by ID directly if gitlab_id lookup fails
		// This is a workaround - ideally we'd have GetIssueByID query
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
```

**Note:** The GetIssueByGitlabID query uses gitlab_issue_id, but we have issue.ID (internal ID). We need to add a GetIssueByID query.

### Step 8: Add GetIssueByID query

**File:** `db/query/issues.sql` (add after line 21)

```sql
-- name: GetIssueByID :one
SELECT * FROM issues WHERE id = $1;
```

### Step 9: Regenerate sqlc and update code

```bash
make sqlc-generate
```

Update the updateIssueMetadata method to use GetIssueByID instead of GetIssueByGitlabID:

```go
// updateIssueMetadata accumulates a metadata label into the issue's metadata_labels JSONB field.
func (s *Service) updateIssueMetadata(ctx context.Context, issueID int32, metadataKey, labelValue string) error {
	// Get current metadata
	issue, err := s.queries.GetIssueByID(ctx, issueID)
	if err != nil {
		return fmt.Errorf("get issue for metadata update: %w", err)
	}
	// ... rest of method unchanged
}
```

### Step 10: Run tests

```bash
cd /home/pedrohenrique/projects/go/pdrhp-app-gitlab-elt
go test ./internal/transformer/... -v
```

**Expected:** PASS - all tests pass

### Step 11: Commit

```bash
git add internal/transformer/service.go internal/transformer/service_test.go
git add db/query/issues.sql internal/repository/
git commit -m "feat: implement metadata label extraction

- Extract metadata labels (tipo, prioridade, area, etc.) during transformation
- Accumulate metadata labels in issues.metadata_labels JSONB field
- Add UpdateIssueMetadata query for persisting metadata
- Add GetIssueByID query for metadata updates
- Prevent duplicate metadata values per issue"
```

---

## Task 3: Data Quality Validation

**Files:**
- Create: `db/views/data_quality_views.sql`
- Create: `scripts/validate_silver_data.sql`
- Modify: `Makefile` (add validate target)

**Context:** Data quality validation ensures the Silver layer has referential integrity, proper coverage, and consistent timestamps. We'll create SQL views and validation scripts.

### Step 1: Create data quality views

**File:** `db/views/data_quality_views.sql`

```sql
-- View: Eventos por projeto (para validar cobertura)
CREATE OR REPLACE VIEW vw_events_by_project AS
SELECT 
    p.id as project_id,
    p.name as project_name,
    p.path as project_path,
    COUNT(DISTINCT ie.id) as event_count,
    COUNT(DISTINCT i.id) as issue_count,
    MAX(ie.event_timestamp) as last_event_timestamp,
    MIN(ie.event_timestamp) as first_event_timestamp
FROM projects p
LEFT JOIN issues i ON i.project_id = p.id
LEFT JOIN issue_events ie ON ie.project_id = p.id
GROUP BY p.id, p.name, p.path;

-- View: Issues sem eventos (problemas de integridade)
CREATE OR REPLACE VIEW vw_issues_without_events AS
SELECT 
    i.id as issue_id,
    i.project_id,
    p.name as project_name,
    i.iid,
    i.title,
    i.gitlab_created_at
FROM issues i
JOIN projects p ON p.id = i.project_id
LEFT JOIN issue_events ie ON ie.issue_id = i.id
WHERE ie.id IS NULL;

-- View: Eventos com estado UNKNOWN (mapeamento incompleto)
CREATE OR REPLACE VIEW vw_unknown_state_events AS
SELECT 
    ie.id as event_id,
    ie.project_id,
    p.name as project_name,
    ie.issue_iid,
    ie.raw_label_added,
    ie.raw_label_removed,
    ie.mapped_canonical_state,
    ie.event_timestamp
FROM issue_events ie
JOIN projects p ON p.id = ie.project_id
WHERE ie.mapped_canonical_state = 'UNKNOWN'
ORDER BY ie.event_timestamp DESC;

-- View: Resumo de qualidade dos dados Silver
CREATE OR REPLACE VIEW vw_data_quality_summary AS
SELECT
    (SELECT COUNT(*) FROM projects) as total_projects,
    (SELECT COUNT(*) FROM issues) as total_issues,
    (SELECT COUNT(*) FROM issue_events) as total_events,
    (SELECT COUNT(*) FROM issue_comments) as total_comments,
    (SELECT COUNT(*) FROM vw_issues_without_events) as issues_without_events,
    (SELECT COUNT(*) FROM vw_unknown_state_events) as events_with_unknown_state,
    (SELECT COUNT(*) FROM unknown_labels_log) as unknown_labels_count,
    (SELECT COUNT(*) FROM issue_events WHERE is_noise = TRUE) as noise_events_count,
    (SELECT AVG(cycle_count) FROM issue_events WHERE cycle_count > 0) as avg_cycle_count;

-- View: Distribuição de estados canônicos
CREATE OR REPLACE VIEW vw_canonical_state_distribution AS
SELECT 
    mapped_canonical_state,
    COUNT(*) as event_count,
    ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (), 2) as percentage
FROM issue_events
WHERE is_noise = FALSE
GROUP BY mapped_canonical_state
ORDER BY event_count DESC;

-- View: Issues com alto número de ciclos (retrabalho)
CREATE OR REPLACE VIEW vw_high_cycle_issues AS
SELECT 
    i.id as issue_id,
    i.project_id,
    p.name as project_name,
    i.iid,
    i.title,
    i.current_canonical_state,
    MAX(ie.cycle_count) as max_cycles,
    COUNT(ie.id) as total_events
FROM issues i
JOIN projects p ON p.id = i.project_id
JOIN issue_events ie ON ie.issue_id = i.id
WHERE ie.is_noise = FALSE
GROUP BY i.id, i.project_id, p.name, i.iid, i.title, i.current_canonical_state
HAVING MAX(ie.cycle_count) >= 3
ORDER BY max_cycles DESC, total_events DESC;
```

### Step 2: Create validation script

**File:** `scripts/validate_silver_data.sql`

```sql
-- Script de validação de dados Silver
-- Uso: psql -d gitlab_elt -f scripts/validate_silver_data.sql

\echo '========================================'
\echo 'Silver Data Quality Validation Report'
\echo '========================================'
\echo ''

\echo '--- Summary ---'
SELECT * FROM vw_data_quality_summary;
\echo ''

\echo '--- Canonical State Distribution ---'
SELECT * FROM vw_canonical_state_distribution;
\echo ''

\echo '--- Issues Without Events (should be 0) ---'
SELECT COUNT(*) as count FROM vw_issues_without_events;
\echo ''

\echo '--- Top 10 Projects by Event Count ---'
SELECT project_name, event_count, issue_count 
FROM vw_events_by_project 
ORDER BY event_count DESC 
LIMIT 10;
\echo ''

\echo '--- Unknown Labels (need mapping) ---'
SELECT label_name, occurrence_count, first_seen_at 
FROM unknown_labels_log 
ORDER BY occurrence_count DESC 
LIMIT 10;
\echo ''

\echo '--- Events with UNKNOWN State (need mapping) ---'
SELECT COUNT(*) as count FROM vw_unknown_state_events;
\echo ''

\echo '--- High Cycle Issues (>= 3 cycles) ---'
SELECT project_name, iid, title, max_cycles, total_events
FROM vw_high_cycle_issues
LIMIT 10;
\echo ''

\echo '--- Noise Events Summary ---'
SELECT 
    COUNT(*) as total_noise_events,
    ROUND(100.0 * COUNT(*) / (SELECT COUNT(*) FROM issue_events), 2) as percentage
FROM issue_events 
WHERE is_noise = TRUE;
\echo ''

\echo '========================================'
\echo 'Validation Complete'
\echo '========================================'
```

### Step 3: Add validate target to Makefile

**File:** `Makefile` - Add after the `health` target (around line 90)

```makefile
.PHONY: validate
validate: ## Run data quality validation against Silver layer
	@echo "Running Silver data quality validation..."
	@docker compose exec -T postgres psql -U gitlab_elt -d gitlab_elt -f /scripts/validate_silver_data.sql

.PHONY: apply-views
apply-views: ## Apply data quality views to database
	@echo "Applying data quality views..."
	@docker compose exec -T postgres psql -U gitlab_elt -d gitlab_elt -f /db/views/data_quality_views.sql
```

### Step 4: Mount views directory in docker-compose

**File:** `docker-compose.yml` - Add volumes mount for postgres service

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-gitlab_elt}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-gitlab_elt}
      POSTGRES_DB: ${POSTGRES_DB:-gitlab_elt}
    ports:
      - "${POSTGRES_PORT:-5432}:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./db/views:/db/views:ro  # Add this line
      - ./scripts:/scripts:ro     # Add this line
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-gitlab_elt}"]
      interval: 5s
      timeout: 5s
      retries: 5

volumes:
  postgres_data:
```

### Step 5: Apply views to database

```bash
cd /home/pedrohenrique/projects/go/pdrhp-app-gitlab-elt
make docker-up
sleep 5
make apply-views
```

**Expected:** Views created successfully

### Step 6: Commit

```bash
git add db/views/data_quality_views.sql scripts/validate_silver_data.sql
git add Makefile docker-compose.yml
git commit -m "feat: add data quality validation views and scripts

- Create vw_events_by_project for coverage analysis
- Create vw_issues_without_events for integrity checks
- Create vw_unknown_state_events for mapping gaps
- Create vw_data_quality_summary for overall health
- Create vw_canonical_state_distribution for state analysis
- Create vw_high_cycle_issues for rework detection
- Add validate and apply-views Makefile targets
- Mount views/scripts directories in docker-compose"
```

---

## Task 4: Handoff Preparation

**Files:**
- Create: `docs/DATA_CONTRACT.md`
- Create: `docs/QUERY_EXAMPLES.md`
- Create: `db/seeds/test_data.sql`

**Context:** Prepare documentation for downstream consumers of the Silver layer data. Include data contract, example queries, and test data.

### Step 1: Create DATA_CONTRACT.md

**File:** `docs/DATA_CONTRACT.md`

```markdown
# Data Contract: GitLab ELT Worker - Silver Layer

## Overview

This document defines the contract between the GitLab ELT Worker and downstream systems consuming the Silver layer data.

## Tables

### projects

Core project information.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Internal ID |
| name | VARCHAR | Project name |
| path | VARCHAR | Full project path (group/project) |
| last_synced_at | TIMESTAMPTZ | Last successful sync timestamp |
| created_at | TIMESTAMPTZ | Record creation time |
| updated_at | TIMESTAMPTZ | Last update time |

**Constraints:**
- path is UNIQUE

---

### issues

Normalized issue data with current state cache.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Internal ID |
| gitlab_issue_id | INTEGER | GitLab global issue ID |
| project_id | INTEGER FK | Reference to projects.id |
| iid | INTEGER | Project-scoped issue number |
| title | TEXT | Issue title |
| current_canonical_state | VARCHAR | Current state (BACKLOG, IN_PROGRESS, QA_REVIEW, BLOCKED, DONE, CANCELED, UNKNOWN) |
| metadata_labels | JSONB | Categorical labels {tipo: [...], prioridade: [...], ...} |
| assignees | JSONB | Assignee usernames |
| gitlab_created_at | TIMESTAMPTZ | Original creation time in GitLab |
| created_at | TIMESTAMPTZ | Record creation time |
| updated_at | TIMESTAMPTZ | Last update time |

**Constraints:**
- UNIQUE(project_id, iid)

---

### issue_events

State transition events with edge case flags.

| Column | Type | Description |
|--------|------|-------------|
| id | BIGSERIAL PK | Internal ID |
| gitlab_event_id | BIGINT | GitLab event ID |
| issue_id | INTEGER FK | Reference to issues.id |
| project_id | INTEGER FK | Reference to projects.id |
| issue_iid | INTEGER | Issue number (denormalized) |
| author_name | VARCHAR | Username who triggered event |
| raw_label_added | VARCHAR | Label added (original text) |
| raw_label_removed | VARCHAR | Label removed (original text) |
| mapped_canonical_state | VARCHAR | Target state after transition |
| event_timestamp | TIMESTAMPTZ | When event occurred |
| is_noise | BOOLEAN | True if rapid transition (< 15 min) |
| cycle_count | INTEGER | Cumulative rework cycles |
| created_at | TIMESTAMPTZ | Record creation time |

**Constraints:**
- UNIQUE(gitlab_event_id, project_id)

**Important Notes:**
- Filter with `is_noise = FALSE` for accurate metrics
- cycle_count increments when returning from QA_REVIEW to IN_PROGRESS

---

### issue_comments

User comments on issues.

| Column | Type | Description |
|--------|------|-------------|
| id | BIGSERIAL PK | Internal ID |
| gitlab_note_id | BIGINT | GitLab note ID |
| issue_id | INTEGER FK | Reference to issues.id |
| author_name | VARCHAR | Comment author |
| body | TEXT | Comment text |
| comment_timestamp | TIMESTAMPTZ | When comment was made |
| created_at | TIMESTAMPTZ | Record creation time |

---

### state_mapping

Configuration: GitLab labels → canonical states.

| Column | Type | Description |
|--------|------|-------------|
| id | SERIAL PK | Internal ID |
| gitlab_label_name | VARCHAR | Original label text |
| canonical_state | VARCHAR | Mapped state |
| description | TEXT | Optional description |

---

### metadata_mapping

Configuration: GitLab labels → metadata keys.

| Column | Type | Description |
|--------|------|-------------|
| id | SERIAL PK | Internal ID |
| gitlab_label_name | VARCHAR | Original label text |
| metadata_key | VARCHAR | Key for metadata_labels JSON (tipo, prioridade, area, etc.) |
| description | TEXT | Optional description |

---

### unknown_labels_log

Audit log of unmapped labels.

| Column | Type | Description |
|--------|------|-------------|
| id | SERIAL PK | Internal ID |
| label_name | VARCHAR | Unmapped label |
| occurrence_count | INTEGER | Times seen |
| first_seen_at | TIMESTAMPTZ | First occurrence |
| last_seen_at | TIMESTAMPTZ | Most recent occurrence |

---

## Canonical States

| State | Description |
|-------|-------------|
| BACKLOG | Not started |
| IN_PROGRESS | Active development |
| QA_REVIEW | Testing/Review phase |
| BLOCKED | Paused/Impeded |
| DONE | Completed |
| CANCELED | Discarded/Cancelled |
| UNKNOWN | Unmapped state (check unknown_labels_log) |

---

## Important Queries for Downstream

See `docs/QUERY_EXAMPLES.md` for detailed query examples.

---

## Data Quality

Use views in `vw_*` namespace for data quality monitoring:
- `vw_data_quality_summary` - Overall health metrics
- `vw_issues_without_events` - Integrity issues
- `vw_unknown_state_events` - Mapping gaps

---

## Update Frequency

- **Peak hours (08:00-20:00 UTC):** Every 15 minutes
- **Off-peak hours:** Every 4 hours
- **Discovery (new projects):** Every 6 hours

---

## Version

- Contract Version: 1.0
- Last Updated: 2025-03-04
```

### Step 2: Create QUERY_EXAMPLES.md

**File:** `docs/QUERY_EXAMPLES.md`

```markdown
# Query Examples - Silver Layer

## Basic Queries

### List all issues in a project

```sql
SELECT i.iid, i.title, i.current_canonical_state, i.gitlab_created_at
FROM issues i
JOIN projects p ON p.id = i.project_id
WHERE p.path = 'my-group/my-project'
ORDER BY i.iid;
```

### Get event history for an issue

```sql
SELECT 
    ie.event_timestamp,
    ie.mapped_canonical_state,
    ie.raw_label_added,
    ie.raw_label_removed,
    ie.author_name,
    ie.is_noise,
    ie.cycle_count
FROM issue_events ie
WHERE ie.project_id = (SELECT id FROM projects WHERE path = 'my-group/my-project')
  AND ie.issue_iid = 123
  AND ie.is_noise = FALSE
ORDER BY ie.event_timestamp;
```

## Analytics Queries

### Lead time (BACKLOG → DONE)

```sql
WITH lifecycle AS (
    SELECT 
        i.id,
        i.project_id,
        i.iid,
        i.title,
        MIN(CASE WHEN ie.mapped_canonical_state = 'BACKLOG' THEN ie.event_timestamp END) as started_at,
        MIN(CASE WHEN ie.mapped_canonical_state = 'DONE' THEN ie.event_timestamp END) as completed_at
    FROM issues i
    LEFT JOIN issue_events ie ON ie.issue_id = i.id AND ie.is_noise = FALSE
    GROUP BY i.id, i.project_id, i.iid, i.title
)
SELECT 
    p.path as project,
    l.iid,
    l.title,
    l.started_at,
    l.completed_at,
    EXTRACT(EPOCH FROM (l.completed_at - l.started_at))/3600/24 as lead_time_days
FROM lifecycle l
JOIN projects p ON p.id = l.project_id
WHERE l.completed_at IS NOT NULL
ORDER BY lead_time_days DESC;
```

### Cycle time (IN_PROGRESS → DONE)

```sql
WITH cycles AS (
    SELECT 
        i.id,
        i.project_id,
        i.iid,
        MIN(CASE WHEN ie.mapped_canonical_state = 'IN_PROGRESS' THEN ie.event_timestamp END) as in_progress_at,
        MIN(CASE WHEN ie.mapped_canonical_state = 'DONE' THEN ie.event_timestamp END) as done_at
    FROM issues i
    LEFT JOIN issue_events ie ON ie.issue_id = i.id AND ie.is_noise = FALSE
    GROUP BY i.id, i.project_id, i.iid
)
SELECT 
    p.path as project,
    c.iid,
    c.in_progress_at,
    c.done_at,
    EXTRACT(EPOCH FROM (c.done_at - c.in_progress_at))/3600/24 as cycle_time_days
FROM cycles c
JOIN projects p ON p.id = c.project_id
WHERE c.done_at IS NOT NULL
  AND c.in_progress_at IS NOT NULL
ORDER BY cycle_time_days DESC;
```

### Issues by metadata (e.g., all Bugs with High Priority)

```sql
SELECT 
    p.path as project,
    i.iid,
    i.title,
    i.current_canonical_state,
    i.metadata_labels
FROM issues i
JOIN projects p ON p.id = i.project_id
WHERE i.metadata_labels @> '{"tipo": ["Bug"]}'::jsonb
  AND i.metadata_labels @> '{"prioridade": ["PRIORIDADE: ALTA"]}'::jsonb
ORDER BY i.gitlab_created_at DESC;
```

### Time in each state (state duration analysis)

```sql
WITH state_durations AS (
    SELECT 
        ie.issue_id,
        ie.mapped_canonical_state,
        ie.event_timestamp,
        LEAD(ie.event_timestamp) OVER (PARTITION BY ie.issue_id ORDER BY ie.event_timestamp) as next_timestamp
    FROM issue_events ie
    WHERE ie.is_noise = FALSE
)
SELECT 
    mapped_canonical_state,
    AVG(EXTRACT(EPOCH FROM (next_timestamp - event_timestamp))/3600) as avg_hours
FROM state_durations
WHERE next_timestamp IS NOT NULL
GROUP BY mapped_canonical_state
ORDER BY avg_hours DESC;
```

### Rework analysis (high cycle count issues)

```sql
SELECT 
    p.path as project,
    i.iid,
    i.title,
    i.current_canonical_state,
    MAX(ie.cycle_count) as max_cycles,
    COUNT(DISTINCT ie.id) as total_events
FROM issues i
JOIN projects p ON p.id = i.project_id
JOIN issue_events ie ON ie.issue_id = i.id
WHERE ie.is_noise = FALSE
GROUP BY p.path, i.iid, i.title, i.current_canonical_state
HAVING MAX(ie.cycle_count) >= 3
ORDER BY max_cycles DESC;
```

## Time-Range Queries

### Events in a date range

```sql
SELECT 
    p.path as project,
    i.iid,
    ie.mapped_canonical_state,
    ie.event_timestamp,
    ie.author_name
FROM issue_events ie
JOIN projects p ON p.id = ie.project_id
JOIN issues i ON i.id = ie.issue_id
WHERE ie.event_timestamp BETWEEN '2025-01-01' AND '2025-02-01'
  AND ie.is_noise = FALSE
ORDER BY ie.event_timestamp DESC;
```

### Project activity summary (last 30 days)

```sql
SELECT 
    p.path,
    COUNT(DISTINCT ie.issue_id) as active_issues,
    COUNT(*) as total_events,
    COUNT(DISTINCT CASE WHEN ie.mapped_canonical_state = 'DONE' THEN ie.issue_id END) as completed_issues
FROM projects p
LEFT JOIN issue_events ie ON ie.project_id = p.id 
    AND ie.event_timestamp > NOW() - INTERVAL '30 days'
    AND ie.is_noise = FALSE
GROUP BY p.path
ORDER BY total_events DESC;
```

## Data Quality Queries

### Issues without events (potential data gaps)

```sql
SELECT * FROM vw_issues_without_events;
```

### Unknown labels needing mapping

```sql
SELECT label_name, occurrence_count, first_seen_at
FROM unknown_labels_log
ORDER BY occurrence_count DESC
LIMIT 20;
```

### Data quality summary

```sql
SELECT * FROM vw_data_quality_summary;
```
```

### Step 3: Create test data seed

**File:** `db/seeds/test_data.sql`

```sql
-- Test data for development and testing
-- This creates sample projects, issues, and events for validation

-- Insert test projects (if not exists)
INSERT INTO projects (name, path, last_synced_at) VALUES
    ('Test Project Alpha', 'test-group/alpha', NOW() - INTERVAL '1 day'),
    ('Test Project Beta', 'test-group/beta', NOW() - INTERVAL '2 hours')
ON CONFLICT (path) DO NOTHING;

-- Get project IDs
WITH project_ids AS (
    SELECT id, path FROM projects WHERE path IN ('test-group/alpha', 'test-group/beta')
)
-- Insert test issues
INSERT INTO issues (gitlab_issue_id, project_id, iid, title, current_canonical_state, metadata_labels, gitlab_created_at)
SELECT 
    10000 + i,
    CASE WHEN i <= 3 THEN (SELECT id FROM project_ids WHERE path = 'test-group/alpha')
         ELSE (SELECT id FROM project_ids WHERE path = 'test-group/beta')
    END,
    i,
    'Test Issue #' || i,
    CASE (i % 6)
        WHEN 0 THEN 'BACKLOG'
        WHEN 1 THEN 'IN_PROGRESS'
        WHEN 2 THEN 'QA_REVIEW'
        WHEN 3 THEN 'BLOCKED'
        WHEN 4 THEN 'DONE'
        ELSE 'CANCELED'
    END,
    CASE (i % 4)
        WHEN 0 THEN '{"tipo": ["Bug"], "prioridade": ["ALTA"]}'::jsonb
        WHEN 1 THEN '{"tipo": ["Feature"], "area": ["Backend"]}'::jsonb
        WHEN 2 THEN '{"tipo": ["Correção"], "complexidade": ["Média"]}'::jsonb
        ELSE '{"tipo": ["Task"], "prioridade": ["BAIXA"], "area": ["Frontend"]}'::jsonb
    END,
    NOW() - INTERVAL '30 days' * i
FROM generate_series(1, 6) i
ON CONFLICT (project_id, iid) DO NOTHING;

-- Insert test events with realistic patterns
WITH test_issues AS (
    SELECT id, project_id, iid FROM issues WHERE title LIKE 'Test Issue #%'
),
event_data AS (
    SELECT 
        i.id as issue_id,
        i.project_id,
        i.iid,
        generate_series(1, 5) as event_num,
        CASE (generate_series(1, 5) % 5)
            WHEN 0 THEN 'BACKLOG'
            WHEN 1 THEN 'IN_PROGRESS'
            WHEN 2 THEN 'QA_REVIEW'
            WHEN 3 THEN 'IN_PROGRESS'  -- Return to dev (cycle)
            ELSE 'DONE'
        END as state
    FROM test_issues i
)
INSERT INTO issue_events (
    gitlab_event_id, issue_id, project_id, issue_iid,
    author_name, raw_label_added, mapped_canonical_state,
    event_timestamp, is_noise, cycle_count
)
SELECT 
    100000 + row_number() OVER () as gitlab_event_id,
    issue_id,
    project_id,
    iid,
    'test-user'::varchar,
    'Label ' || state,
    state,
    NOW() - INTERVAL '1 day' * event_num + INTERVAL '1 hour' * event_num,
    FALSE,
    CASE WHEN state = 'IN_PROGRESS' AND event_num = 4 THEN 1 ELSE 0 END
FROM event_data
ON CONFLICT (gitlab_event_id, project_id) DO NOTHING;

-- Insert a "nervous finger" event (rapid transition)
WITH last_issue AS (
    SELECT id, project_id, iid 
    FROM issues 
    WHERE title = 'Test Issue #1'
    LIMIT 1
)
INSERT INTO issue_events (
    gitlab_event_id, issue_id, project_id, issue_iid,
    author_name, raw_label_added, mapped_canonical_state,
    event_timestamp, is_noise, cycle_count
)
SELECT 
    999999,
    id,
    project_id,
    iid,
    'test-user',
    'Quick Label',
    'IN_PROGRESS',
    NOW() - INTERVAL '30 days' + INTERVAL '5 minutes',  -- Only 5 min after previous
    TRUE,  -- Mark as noise
    0
FROM last_issue
ON CONFLICT (gitlab_event_id, project_id) DO NOTHING;

\echo 'Test data seeded successfully!'
\echo 'Run validation with: make validate'
```

### Step 4: Add seed-test target to Makefile

**File:** `Makefile` - Add after the `seed` target

```makefile
.PHONY: seed-test
seed-test: ## Seed test data for development
	@echo "Seeding test data..."
	@docker compose exec -T postgres psql -U gitlab_elt -d gitlab_elt -f /db/seeds/test_data.sql
```

### Step 5: Commit

```bash
git add docs/DATA_CONTRACT.md docs/QUERY_EXAMPLES.md
git add db/seeds/test_data.sql Makefile
git commit -m "docs: add data contract, query examples, and test data

- Create DATA_CONTRACT.md documenting Silver layer schema
- Create QUERY_EXAMPLES.md with common analytics queries
- Add test_data.sql seed for development and testing
- Include lead time, cycle time, and rework analysis examples
- Document canonical states and metadata structure"
```

---

## Task 5: Integration and Final Validation

### Step 1: Run all tests

```bash
cd /home/pedrohenrique/projects/go/pdrhp-app-gitlab-elt
go test ./... -v 2>&1 | tail -30
```

**Expected:** All tests pass

### Step 2: Build binaries

```bash
make build
```

**Expected:** worker and backfill binaries created in bin/

### Step 3: Run linter (if available)

```bash
make lint 2>/dev/null || go vet ./...
```

**Expected:** No errors

### Step 4: Final commit

```bash
git log --oneline -5
```

**Expected:** Clean commit history with Phase 3 work

---

## Summary

**Phase 3 Complete!** The following features have been implemented:

1. **Edge Case Detection**
   - Dedo nervoso: rapid transitions marked as is_noise
   - Falso movimento: same-state transitions detected
   - Ping-pong: cycle_count tracked for rework analysis

2. **Metadata Label Extraction**
   - Categorical labels (tipo, prioridade, area) extracted
   - Accumulated in issues.metadata_labels JSONB field

3. **Data Quality Validation**
   - SQL views for monitoring data health
   - Validation script for integrity checks
   - Makefile targets for easy validation

4. **Handoff Documentation**
   - DATA_CONTRACT.md for downstream consumers
   - QUERY_EXAMPLES.md with analytics queries
   - Test data seed for development

**Next Steps:**
- Run `make validate` to check data quality
- Review data contract with downstream team
- Consider Phase 4: Observability (Prometheus metrics)

---

## Files Modified/Created

**New Files:**
- `internal/transformer/edge_cases.go`
- `internal/transformer/edge_cases_test.go`
- `db/views/data_quality_views.sql`
- `scripts/validate_silver_data.sql`
- `docs/DATA_CONTRACT.md`
- `docs/QUERY_EXAMPLES.md`
- `db/seeds/test_data.sql`

**Modified Files:**
- `internal/transformer/service.go`
- `internal/transformer/service_test.go`
- `db/query/issue_events.sql`
- `db/query/issues.sql`
- `Makefile`
- `docker-compose.yml`
- `internal/repository/` (regenerated)

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

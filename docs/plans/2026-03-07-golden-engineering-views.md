# Golden Engineering Views Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add gold-layer SQL views that expose per-issue lifecycle metrics and project summaries for engineering analytics.

**Architecture:** Build the gold layer directly on top of the Silver tables `issues`, `issue_events`, and `projects`. First normalize the event stream into deduplicated state transitions, then derive state intervals, then compute per-issue metrics and project-level aggregates.

**Tech Stack:** PostgreSQL views, existing migration system, psql verification queries.

---

### Task 1: Validate the target contract

**Files:**
- Modify: `docs/plans/2026-03-07-golden-engineering-views.md`
- Test: run ad hoc `psql` queries against current data

**Step 1: Confirm metric feasibility from Silver tables**

Run read-only SQL against `issues`, `issue_events`, `projects`, `state_mapping`, and related tables to verify coverage for backlog, in-progress, QA, blocked, and done timestamps.

**Step 2: Record modeling decisions**

- Filter `issue_events.is_noise = FALSE`
- Collapse consecutive duplicate canonical states
- Use `COALESCE(first_backlog_at, gitlab_created_at)` as lifecycle start
- Define cycle time as time spent in `IN_PROGRESS` + `QA_REVIEW`
- Define blocked time as total time spent in `BLOCKED`

### Task 2: Write the failing verification query

**Files:**
- Test: `db/migrations/000013_create_golden_engineering_views.up.sql`

**Step 1: Run verification before implementation**

Run a `psql` query selecting from the target views and confirm it fails with `relation does not exist`.

**Step 2: Keep the target API stable**

Verify these view names before implementation:
- `vw_issue_state_transitions`
- `vw_issue_state_intervals`
- `vw_issue_lifecycle_metrics`
- `vw_project_engineering_metrics`

### Task 3: Create the migration

**Files:**
- Create: `db/migrations/000013_create_golden_engineering_views.up.sql`
- Create: `db/migrations/000013_create_golden_engineering_views.down.sql`

**Step 1: Create deduplicated transition view**

Expose one row per real canonical state change, ordered by issue timeline.

**Step 2: Create interval view**

Expose `entered_at`, `exited_at`, and interval durations per issue/state.

**Step 3: Create lifecycle metric view**

Expose lead time, cycle time, blocked time, rework count, ghost-work flag, and supporting timestamps per issue.

**Step 4: Create project summary view**

Aggregate the issue lifecycle view into project-level counts and averages for downstream apps.

### Task 4: Verify the migration and semantics

**Files:**
- Test: `db/migrations/000013_create_golden_engineering_views.up.sql`

**Step 1: Apply the migration**

Run the migration against the local Postgres container.

**Step 2: Re-run verification queries**

Select from the new views and confirm they return rows and expected columns.

**Step 3: Sanity-check metrics**

Compare selected outputs against direct SQL aggregates from the Silver layer.

### Task 5: Document findings for downstream consumption

**Files:**
- Modify: `docs/QUERY_EXAMPLES.md`

**Step 1: Add sample gold-layer queries if useful**

Show how a downstream service can consume the new views for dashboards and integrations.

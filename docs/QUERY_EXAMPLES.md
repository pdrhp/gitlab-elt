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

### State semantics and noise

`vw_issue_lifecycle_metrics` exposes two state perspectives, both based on canonical timeline state:

- `operational_current_canonical_state`: current snapshot from `issues.current_canonical_state`
- `analytical_current_canonical_state`: state derived from canonical transitions timeline
- `current_canonical_state`: compatibility alias for `operational_current_canonical_state`

`is_noise` is treated as a separate process-quality signal and does not filter canonical state progression for lifecycle calculations.

```sql
SELECT
    issue_id,
    issue_iid,
    operational_current_canonical_state,
    analytical_current_canonical_state,
    current_canonical_state
FROM vw_issue_lifecycle_metrics
WHERE project_id = (SELECT id FROM projects WHERE path = 'my-group/my-project')
ORDER BY issue_iid DESC
LIMIT 20;
```

### Project noise metrics (per project)

`vw_project_engineering_metrics` includes:

- `noise_events_count`: number of events flagged with `is_noise = TRUE`
- `noise_rate_pct`: percentage of noise events over total events in the project

```sql
SELECT
    project_path,
    backlog_issues,
    in_progress_issues,
    canceled_issues,
    noise_events_count,
    noise_rate_pct
FROM vw_project_engineering_metrics
ORDER BY noise_rate_pct DESC NULLS LAST, project_path;
```

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

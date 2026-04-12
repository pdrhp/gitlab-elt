-- name: GetAssigneeCycleTimeByIssue :many
-- Get cycle time breakdown for each assignee on a specific issue.
SELECT * FROM vw_assignee_cycle_time WHERE issue_id = $1 ORDER BY active_cycle_hours DESC NULLS LAST;

-- name: GetAssigneeCycleTimeByProject :many
-- Get all assignee cycle time metrics for a project.
SELECT 
    assignee_username,
    COUNT(DISTINCT issue_id) AS issues_count,
    ROUND(SUM(active_cycle_hours)::numeric, 2) AS total_active_hours,
    ROUND(AVG(active_cycle_hours)::numeric, 2) AS avg_active_hours,
    ROUND(SUM(in_progress_hours)::numeric, 2) AS total_dev_hours,
    ROUND(SUM(qa_review_hours)::numeric, 2) AS total_qa_hours
FROM vw_assignee_cycle_time
WHERE project_id = $1
GROUP BY assignee_username
ORDER BY total_active_hours DESC NULLS LAST;

-- name: GetIndividualPerformanceMetrics :many
-- Get aggregated performance metrics for all assignees in a project.
SELECT * FROM vw_individual_performance_metrics 
WHERE project_id = $1 
ORDER BY total_active_cycle_hours DESC NULLS LAST;

-- name: GetUserPerformanceOverTime :many
-- Get user performance trends by month.
SELECT 
    ac.assignee_username,
    DATE_TRUNC('month', ie.event_timestamp)::date AS month,
    COUNT(DISTINCT ac.issue_id) AS issues_completed,
    ROUND(SUM(ac.active_cycle_hours)::numeric, 2) AS total_active_hours,
    ROUND(AVG(ac.active_cycle_hours)::numeric, 2) AS avg_active_hours
FROM vw_assignee_cycle_time ac
JOIN issue_events ie ON ie.issue_id = ac.issue_id AND ie.mapped_canonical_state = 'DONE'
WHERE ac.project_id = $1
  AND ac.assignee_username = $2
GROUP BY ac.assignee_username, DATE_TRUNC('month', ie.event_timestamp)
ORDER BY month DESC;

-- name: GetHighPerformers :many
-- Get top performers by active work percentage and volume.
SELECT 
    assignee_username,
    issues_contributed,
    total_active_cycle_hours,
    avg_active_cycle_per_issue,
    active_work_pct,
    p50_active_cycle_hours,
    p95_active_cycle_hours
FROM vw_individual_performance_metrics
WHERE project_id = $1
  AND issues_contributed >= 5
ORDER BY total_active_cycle_hours DESC NULLS LAST
LIMIT 20;

-- name: FindAssigneesWithHighBlockedTime :many
-- Identify assignees spending too much time in BLOCKED state.
SELECT 
    assignee_username,
    issues_assigned,
    total_blocked_hours,
    ROUND((100.0 * total_blocked_hours / NULLIF(total_hours_as_assignee, 0))::numeric, 2) AS blocked_pct
FROM vw_individual_performance_metrics
WHERE project_id = $1
  AND total_blocked_hours > 50
ORDER BY blocked_pct DESC NULLS LAST;

-- name: GetAssigneeWorkDistribution :many
-- Get distribution of active vs wait time for each assignee.
SELECT 
    assignee_username,
    ROUND(SUM(active_cycle_hours)::numeric, 2) AS active_hours,
    ROUND(SUM(total_hours_as_assignee - COALESCE(active_cycle_hours, 0))::numeric, 2) AS wait_hours,
    ROUND((100.0 * SUM(active_cycle_hours) / NULLIF(SUM(total_hours_as_assignee), 0))::numeric, 2) AS active_pct
FROM vw_assignee_cycle_time
WHERE project_id = $1
GROUP BY assignee_username
ORDER BY active_pct DESC NULLS LAST;

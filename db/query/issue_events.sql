-- name: InsertIssueEvent :one
-- Insere um evento normalizado na camada Silver.
INSERT INTO issue_events (
    gitlab_event_id, issue_id, project_id, issue_iid,
    author_name, raw_label_added, raw_label_removed,
    mapped_canonical_state, event_timestamp, is_noise, cycle_count
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (gitlab_event_id, project_id) DO NOTHING
RETURNING *;

-- name: ListIssueEventsByIssue :many
-- Lista eventos de uma issue especifica, ordenados por timestamp.
SELECT * FROM issue_events
WHERE issue_id = $1
ORDER BY event_timestamp ASC;

-- name: ListIssueEventsByProject :many
-- Lista eventos de um projeto, ordenados por timestamp.
SELECT * FROM issue_events
WHERE project_id = $1
ORDER BY event_timestamp ASC;

-- name: CountIssueEventsByProject :one
SELECT COUNT(*) FROM issue_events WHERE project_id = $1;

-- name: GetLatestIssueEvent :one
-- Retorna o evento mais recente de uma issue (para calcular estado atual).
SELECT * FROM issue_events
WHERE issue_id = $1 AND is_noise = FALSE
ORDER BY event_timestamp DESC
LIMIT 1;

-- name: GetLastIssueEvent :one
-- Returns the most recent non-noise event for an issue (for edge case detection).
SELECT * FROM issue_events
WHERE issue_id = $1 AND is_noise = FALSE
ORDER BY event_timestamp DESC
LIMIT 1;

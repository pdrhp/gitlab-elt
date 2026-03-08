-- name: UpsertIssue :one
-- Insere ou atualiza uma issue na camada Silver.
INSERT INTO issues (
    gitlab_issue_id, project_id, iid, title,
    current_canonical_state, metadata_labels, assignees,
    gitlab_created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
ON CONFLICT (project_id, iid) DO UPDATE SET
    gitlab_issue_id = EXCLUDED.gitlab_issue_id,
    title = EXCLUDED.title,
    current_canonical_state = EXCLUDED.current_canonical_state,
    metadata_labels = EXCLUDED.metadata_labels,
    assignees = EXCLUDED.assignees,
    updated_at = NOW()
RETURNING *;

-- name: GetIssueByGitlabID :one
SELECT * FROM issues WHERE gitlab_issue_id = $1;

-- name: GetIssueByProjectAndIID :one
SELECT * FROM issues WHERE project_id = $1 AND iid = $2;

-- name: ListIssuesByProject :many
SELECT * FROM issues WHERE project_id = $1 ORDER BY iid;

-- name: UpdateIssueCanonicalState :exec
-- Atualiza o cache de estado canonico da issue.
UPDATE issues SET current_canonical_state = $1, updated_at = NOW()
WHERE id = $2;

-- name: GetIssueByID :one
SELECT * FROM issues WHERE id = $1;

-- name: UpdateIssueMetadata :exec
UPDATE issues SET 
    metadata_labels = $1,
    updated_at = NOW()
WHERE id = $2;

-- name: UpdateIssueAssignees :exec
-- Atualiza o histórico de assignees da issue.
UPDATE issues SET 
    assignees = $1,
    updated_at = NOW()
WHERE id = $2;

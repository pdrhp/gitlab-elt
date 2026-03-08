-- name: UpsertRawIssue :one
INSERT INTO raw_issues (gitlab_issue_id, project_id, iid, title, description, state, raw_payload)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (project_id, iid) DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    state = EXCLUDED.state,
    raw_payload = EXCLUDED.raw_payload,
    updated_at = NOW()
RETURNING id, gitlab_issue_id, project_id, iid, title, description, state, raw_payload, created_at, updated_at;

-- name: GetRawIssueByProjectAndIID :one
SELECT * FROM raw_issues WHERE project_id = $1 AND iid = $2;

-- name: ListRawIssuesByProject :many
SELECT * FROM raw_issues WHERE project_id = $1 ORDER BY iid;

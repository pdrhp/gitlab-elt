-- name: InsertIssueComment :one
-- Insere um comentario normalizado na camada Silver.
INSERT INTO issue_comments (
    gitlab_note_id, issue_id, author_name, body, comment_timestamp
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (gitlab_note_id) DO NOTHING
RETURNING *;

-- name: ListIssueCommentsByIssue :many
SELECT * FROM issue_comments
WHERE issue_id = $1
ORDER BY comment_timestamp ASC;

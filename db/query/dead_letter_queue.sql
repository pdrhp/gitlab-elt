-- db/query/dead_letter_queue.sql

-- name: InsertDLQEntry :one
-- Insere um evento que falhou apos max retries.
INSERT INTO dead_letter_queue (project_id, issue_iid, event_type, raw_payload, error_message, error_category)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListUnresolvedDLQ :many
-- Lista entradas DLQ nao resolvidas, mais antigas primeiro.
SELECT * FROM dead_letter_queue
WHERE resolved = FALSE
ORDER BY created_at ASC
LIMIT $1;

-- name: ListDLQByProject :many
SELECT * FROM dead_letter_queue
WHERE project_id = $1 AND resolved = FALSE
ORDER BY created_at ASC;

-- name: CountUnresolvedDLQ :one
SELECT COUNT(*) FROM dead_letter_queue WHERE resolved = FALSE;

-- name: MarkDLQResolved :exec
UPDATE dead_letter_queue SET resolved = TRUE WHERE id = $1;

-- name: IncrementDLQRetry :exec
-- Incrementa retry_count e atualiza last_retry_at.
UPDATE dead_letter_queue
SET retry_count = retry_count + 1, last_retry_at = NOW()
WHERE id = $1;

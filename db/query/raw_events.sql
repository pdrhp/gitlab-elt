-- name: InsertRawEvent :one
-- Insere um evento bruto na camada Bronze. Append-only, imutavel.
INSERT INTO raw_events (gitlab_event_id, project_id, issue_iid, event_type, raw_payload)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: BulkInsertRawEvent :exec
-- Insere evento bruto (usado em batch). ON CONFLICT ignora duplicatas.
INSERT INTO raw_events (gitlab_event_id, project_id, issue_iid, event_type, raw_payload)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT DO NOTHING;

-- name: ListUnprocessedRawEvents :many
-- Lista eventos Bronze ainda nao transformados para Silver.
-- Usado pelo Transformer para processar em batch.
SELECT * FROM raw_events
WHERE processed = FALSE
ORDER BY fetched_at ASC
LIMIT $1;

-- name: ListUnprocessedRawEventsByProject :many
-- Lista eventos nao processados de um projeto especifico.
SELECT * FROM raw_events
WHERE processed = FALSE AND project_id = $1
ORDER BY fetched_at ASC
LIMIT $2;

-- name: MarkRawEventProcessed :exec
-- Marca evento como processado apos transformacao Silver.
UPDATE raw_events SET processed = TRUE WHERE id = $1;

-- name: MarkRawEventsProcessedBatch :exec
-- Marca multiplos eventos como processados.
UPDATE raw_events SET processed = TRUE WHERE id = ANY($1::bigint[]);

-- name: CountRawEventsByProject :one
SELECT COUNT(*) FROM raw_events WHERE project_id = $1;

-- name: CountUnprocessedRawEvents :one
SELECT COUNT(*) FROM raw_events WHERE processed = FALSE;

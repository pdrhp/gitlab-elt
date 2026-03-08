-- name: UpsertUnknownLabel :one
-- Registra ou incrementa contagem de uma label desconhecida.
INSERT INTO unknown_labels_log (label_name, occurrence_count, first_seen_at, last_seen_at)
VALUES ($1, 1, NOW(), NOW())
ON CONFLICT (label_name) DO UPDATE SET
    occurrence_count = unknown_labels_log.occurrence_count + 1,
    last_seen_at = NOW()
RETURNING *;

-- name: ListUnknownLabels :many
-- Lista labels desconhecidas ordenadas por frequencia (mais comuns primeiro).
SELECT * FROM unknown_labels_log
ORDER BY occurrence_count DESC;

-- name: CountUnknownLabels :one
SELECT COUNT(*) FROM unknown_labels_log;

-- name: GetSyncState :one
SELECT * FROM sync_state
WHERE project_id = $1
LIMIT 1;

-- name: UpsertSyncState :one
INSERT INTO sync_state (project_id, last_synced_at, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (project_id)
DO UPDATE SET
    last_synced_at = EXCLUDED.last_synced_at,
    updated_at = NOW()
RETURNING *;

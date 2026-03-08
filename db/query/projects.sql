-- name: UpsertProject :one
-- Insere ou atualiza um projeto na camada Silver.
-- Usado pelo Transformer ao normalizar raw_projects -> projects.
INSERT INTO projects (id, name, path, last_synced_at, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    path = EXCLUDED.path,
    last_synced_at = GREATEST(projects.last_synced_at, EXCLUDED.last_synced_at),
    updated_at = NOW()
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1;

-- name: ListProjects :many
SELECT * FROM projects ORDER BY name;

-- name: SyncProjectLastSynced :one
WITH matched_projects AS (
    SELECT p.id
    FROM projects p
    JOIN raw_projects rp ON rp.id = p.id
    WHERE p.id = $2
    FOR UPDATE
),
updated_project AS (
    UPDATE projects AS p
    SET last_synced_at = $1, updated_at = NOW()
    FROM matched_projects mp
    WHERE p.id = mp.id
    RETURNING p.id
),
updated_raw AS (
    UPDATE raw_projects AS rp
    SET last_synced_at = $1, updated_at = NOW()
    FROM matched_projects mp
    WHERE rp.id = mp.id
    RETURNING rp.id
)
SELECT
    (SELECT COUNT(*) FROM updated_raw) AS raw_projects_updated,
    (SELECT COUNT(*) FROM updated_project) AS projects_updated;

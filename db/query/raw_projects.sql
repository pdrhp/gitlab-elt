-- name: UpsertRawProject :one
-- Insere ou atualiza um projeto na camada Bronze.
-- Usado pelo Discovery Service ao descobrir projetos novos.
INSERT INTO raw_projects (id, name, path, raw_metadata, last_synced_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    path = EXCLUDED.path,
    raw_metadata = EXCLUDED.raw_metadata,
    updated_at = NOW()
RETURNING *;

-- name: GetRawProject :one
SELECT * FROM raw_projects WHERE id = $1;

-- name: ListRawProjects :many
-- Lista todos os projetos Bronze, ordenados por nome.
SELECT * FROM raw_projects ORDER BY name;

-- name: ListRawProjectsDueSync :many
-- Lista projetos que precisam de sync (last_synced_at antes do threshold).
SELECT * FROM raw_projects
WHERE last_synced_at < $1
ORDER BY last_synced_at ASC;

-- name: UpdateRawProjectLastSynced :exec
-- Atualiza o cursor de sincronizacao apos extração Bronze bem-sucedida.
UPDATE raw_projects
SET last_synced_at = $1, updated_at = NOW()
WHERE id = $2;

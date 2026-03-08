-- name: ListStateMappings :many
-- Lista todos os mapeamentos de estado. Usado pelo StateMapper no startup.
SELECT * FROM state_mapping ORDER BY canonical_state, gitlab_label_name;

-- name: GetStateMappingByLabel :one
SELECT * FROM state_mapping WHERE gitlab_label_name = $1;

-- name: UpsertStateMapping :one
INSERT INTO state_mapping (gitlab_label_name, canonical_state, description, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (gitlab_label_name) DO UPDATE SET
    canonical_state = EXCLUDED.canonical_state,
    description = EXCLUDED.description,
    updated_at = NOW()
RETURNING *;

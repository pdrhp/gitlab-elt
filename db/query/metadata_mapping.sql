-- name: ListMetadataMappings :many
-- Lista todos os mapeamentos de metadados. Usado pelo StateMapper no startup.
SELECT * FROM metadata_mapping ORDER BY metadata_key, gitlab_label_name;

-- name: GetMetadataMappingByLabel :one
SELECT * FROM metadata_mapping WHERE gitlab_label_name = $1;

-- name: UpsertMetadataMapping :one
INSERT INTO metadata_mapping (gitlab_label_name, metadata_key)
VALUES ($1, $2)
ON CONFLICT (gitlab_label_name) DO UPDATE SET
    metadata_key = EXCLUDED.metadata_key
RETURNING *;

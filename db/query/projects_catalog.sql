-- name: ListProjectsCatalog :many
SELECT id, name, path, group_path, total_issues, last_synced_at
FROM vw_projects_catalog
ORDER BY name;

-- name: SearchProjectsCatalog :many
SELECT id, name, path, group_path, total_issues, last_synced_at
FROM vw_projects_catalog
WHERE ($1::text = '' OR name ILIKE '%' || $1 || '%' OR path ILIKE '%' || $1 || '%')
  AND ($2::text = '' OR group_path = $2)
ORDER BY name;

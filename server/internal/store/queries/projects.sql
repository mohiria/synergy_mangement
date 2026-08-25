-- name: ListProjects :many
SELECT p.*, u.display_name AS owner_name
FROM projects p
JOIN users u ON u.id = p.owner_id
ORDER BY p.created_at DESC;

-- name: GetProject :one
SELECT p.*, u.display_name AS owner_name
FROM projects p
JOIN users u ON u.id = p.owner_id
WHERE p.id = $1;

-- name: CreateProject :one
INSERT INTO projects (name, created_by, owner_id, status, stage, planned_start_date, planned_end_date)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateProject :one
UPDATE projects
SET name = $2,
    owner_id = $3,
    status = $4,
    stage = $5,
    planned_start_date = $6,
    planned_end_date = $7
WHERE id = $1
RETURNING *;

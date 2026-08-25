-- name: ListProjects :many
SELECT * FROM projects ORDER BY created_at DESC;

-- name: CreateProject :one
INSERT INTO projects (name, created_by)
VALUES ($1, $2)
RETURNING *;

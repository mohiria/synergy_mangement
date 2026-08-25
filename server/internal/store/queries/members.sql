-- name: ListProjectMembers :many
SELECT m.user_id, m.role, u.username, u.display_name
FROM project_members m
JOIN users u ON u.id = m.user_id
WHERE m.project_id = $1
ORDER BY u.display_name, u.username;

-- name: GetProjectMember :one
SELECT * FROM project_members
WHERE project_id = $1 AND user_id = $2;

-- name: AddProjectMember :one
INSERT INTO project_members (project_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateProjectMemberRole :one
UPDATE project_members
SET role = $3
WHERE project_id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteProjectMember :execrows
DELETE FROM project_members
WHERE project_id = $1 AND user_id = $2;

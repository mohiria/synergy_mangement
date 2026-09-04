-- name: ListProjectMembers :many
-- disabled_at：停用成员照常返回（历史记录与成员管理要显示名字并打「已停用」，#204）；
-- 对人员选择器的过滤在 handler 按 includeDisabled 参数做。
SELECT m.user_id, m.role, u.username, u.display_name, u.disabled_at
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

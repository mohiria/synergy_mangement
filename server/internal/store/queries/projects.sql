-- name: ListProjects :many
-- my_role：当前用户在各项目中的成员角色（非成员为 NULL），供 domain 层判定动作权限。
SELECT p.*, u.display_name AS owner_name, m.role AS my_role
FROM projects p
JOIN users u ON u.id = p.owner_id
LEFT JOIN project_members m ON m.project_id = p.id AND m.user_id = $1
ORDER BY p.created_at DESC;

-- name: GetProject :one
SELECT p.*, u.display_name AS owner_name, m.role AS my_role
FROM projects p
JOIN users u ON u.id = p.owner_id
LEFT JOIN project_members m ON m.project_id = p.id AND m.user_id = $2
WHERE p.id = $1;

-- name: CreateProject :one
-- 创建项目并在同一语句内把创建人写入成员表（角色由调用方传入，domain 定为 admin）。
WITH new_project AS (
    INSERT INTO projects (name, created_by, owner_id, status, stage, planned_start_date, planned_end_date)
    VALUES ($1, $2, $3, $4, $5, $6, $7)
    RETURNING *
), creator_member AS (
    INSERT INTO project_members (project_id, user_id, role)
    SELECT id, created_by, $8 FROM new_project
)
SELECT * FROM new_project;

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

-- name: ListActiveProjectIDs :many
-- 活跃项目（每小时 ticker 的扫描范围）：已完成与已归档项目不再补记卡点动态。
SELECT id FROM projects
WHERE status IN ('not_started', 'in_progress')
ORDER BY id;

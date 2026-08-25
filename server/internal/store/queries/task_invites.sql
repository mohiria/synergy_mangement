-- name: CreateTaskInvite :one
INSERT INTO task_invites (key_result_id, inviter_id, invitee_id, note)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetTaskInviteInProject :one
-- 邀请连同所属 KR 负责人与项目归属（权限判定与项目内寻址）。
SELECT ti.*, k.owner_id AS kr_owner_id, o.project_id
FROM task_invites ti
JOIN key_results k ON k.id = ti.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE ti.id = $1 AND o.project_id = $2;

-- name: ListProjectTaskInvites :many
-- 项目内全部邀请，含邀请人／受邀人姓名（派生动作标志在 domain 判定）。
SELECT ti.*, iu.display_name AS inviter_name, eu.display_name AS invitee_name
FROM task_invites ti
JOIN key_results k ON k.id = ti.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users iu ON iu.id = ti.inviter_id
JOIN users eu ON eu.id = ti.invitee_id
WHERE o.project_id = $1
ORDER BY ti.id DESC;

-- name: UpdateTaskInviteState :one
UPDATE task_invites
SET state = $2
WHERE id = $1
RETURNING *;

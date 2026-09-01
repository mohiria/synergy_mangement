-- name: ClearTaskParticipants :execrows
DELETE FROM task_participants WHERE task_id = $1;

-- name: SetTaskParticipant :exec
INSERT INTO task_participants (task_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListTaskParticipants :many
SELECT tp.user_id, u.display_name
FROM task_participants tp
JOIN users u ON u.id = tp.user_id
WHERE tp.task_id = $1
ORDER BY tp.user_id;

-- name: ListParticipantsByProject :many
-- 项目内全部任务的参与人名单（任务列表派生字段用；与接收方同口径一次取全项目）。
SELECT tp.task_id, tp.user_id, u.display_name
FROM task_participants tp
JOIN tasks t ON t.id = tp.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users u ON u.id = tp.user_id
WHERE o.project_id = $1
ORDER BY tp.task_id, tp.user_id;

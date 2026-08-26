-- name: CreateTaskActivity :one
-- 追加一条任务动态（ADR 0002）；文案在写入时定型。
INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListTaskActivitiesByTask :many
-- 任务动态，最新在前；连同行动人姓名（系统派生事件为空）。
SELECT a.*, u.display_name AS actor_name
FROM task_activities a
LEFT JOIN users u ON u.id = a.actor_id
WHERE a.task_id = $1
ORDER BY a.id DESC;

-- name: CreateBlockerActivity :execrows
-- 卡点动态：系统派生、无行动人；按 (任务, 类型, 合成键, 发生时刻) 去重，重复插入直接忽略。
INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at, blocker_key)
VALUES ($1, $2, NULL, $3, $4, $5)
ON CONFLICT DO NOTHING;

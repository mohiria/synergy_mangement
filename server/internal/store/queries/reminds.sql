-- name: GetLastRemind :one
-- 同一人对同一任务最近一次提醒（冷却判定用）。
SELECT * FROM remind_logs
WHERE task_id = $1 AND sender_id = $2
ORDER BY id DESC
LIMIT 1;

-- name: CreateRemindLog :one
INSERT INTO remind_logs (task_id, sender_id, target_key, remind_date)
VALUES ($1, $2, $3, $4)
RETURNING *;

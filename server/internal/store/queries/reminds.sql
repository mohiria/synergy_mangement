-- name: CountRemindsToday :one
-- 冷却判定（MW-13、AC-60）：同一发起人对同一被提醒人的同一任务当天已发出的提醒次数；
-- 上限取项目规则设置的 remind_daily_limit，换一个被提醒人不受影响。
SELECT count(*) FROM remind_logs
WHERE task_id = $1 AND sender_id = $2 AND recipient_id = $3 AND remind_date = $4;

-- name: CreateRemindLog :one
INSERT INTO remind_logs (task_id, sender_id, recipient_id, target_key, remind_date)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

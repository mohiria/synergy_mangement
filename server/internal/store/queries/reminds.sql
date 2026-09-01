-- name: CountRemindsToday :one
-- 冷却判定（MW-13、AC-60）：同一发起人对同一被提醒人的同一任务当天已发出的提醒次数；
-- 上限取项目规则设置的 remind_daily_limit，换一个被提醒人不受影响。
SELECT count(*) FROM remind_logs
WHERE task_id = $1 AND sender_id = $2 AND recipient_id = $3 AND remind_date = $4;

-- name: ListRemindCountsToday :many
-- 当日配额显隐（#129）：一次取回该发起人今天对（被提醒人、任务）的全部计数，
-- 卡点列表与我的工作按目标逐个判定「任一待行动人未用完即显示按钮」。
SELECT recipient_id, task_id, count(*) AS n FROM remind_logs
WHERE sender_id = $1 AND remind_date = $2
GROUP BY recipient_id, task_id;

-- name: CreateRemindLog :one
INSERT INTO remind_logs (task_id, sender_id, recipient_id, target_key, remind_date)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

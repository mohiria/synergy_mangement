-- name: SetTaskReceiverScope :one
UPDATE tasks
SET receiver_scope = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearTaskReceivers :execrows
DELETE FROM task_receivers WHERE task_id = $1;

-- name: SetTaskReceiver :exec
INSERT INTO task_receivers (task_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListTaskReceivers :many
SELECT tr.user_id, u.display_name
FROM task_receivers tr
JOIN users u ON u.id = tr.user_id
WHERE tr.task_id = $1
ORDER BY tr.user_id;

-- name: CreateTaskReceipt :exec
-- 终审通过时逐人生成待接收项；同一任务同一人只生成一次（重复终审不重复记账）。
INSERT INTO task_receipts (task_id, user_id)
VALUES ($1, $2)
ON CONFLICT (task_id, user_id) DO NOTHING;

-- name: ListTaskReceipts :many
SELECT tr.*, u.display_name
FROM task_receipts tr
JOIN users u ON u.id = tr.user_id
WHERE tr.task_id = $1
ORDER BY tr.user_id;

-- name: GetTaskReceipt :one
SELECT tr.*, u.display_name
FROM task_receipts tr
JOIN users u ON u.id = tr.user_id
WHERE tr.task_id = $1 AND tr.user_id = $2;

-- name: ConfirmTaskReceipt :one
UPDATE task_receipts
SET confirmed_at = now()
WHERE id = $1 AND confirmed_at IS NULL
RETURNING *;

-- name: ListReceiptsByProject :many
-- 项目内全部待接收项与接收记录（我的工作分组用），含任务名。
SELECT tr.*, t.name AS task_name, u.display_name
FROM task_receipts tr
JOIN tasks t ON t.id = tr.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users u ON u.id = tr.user_id
WHERE o.project_id = $1
ORDER BY tr.id;

-- name: ListReceiversByProject :many
-- 项目内全部任务的接收方名单（任务列表派生字段用）。
SELECT tr.task_id, tr.user_id, u.display_name
FROM task_receivers tr
JOIN tasks t ON t.id = tr.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users u ON u.id = tr.user_id
WHERE o.project_id = $1
ORDER BY tr.task_id, tr.user_id;

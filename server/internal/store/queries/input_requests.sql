-- name: CreateInputRequest :one
INSERT INTO input_requests (edge_id, provider_id, content_note, notified_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetInputRequestInProject :one
SELECT ir.*, u.display_name AS provider_name,
    e.target_task_id, e.name AS edge_name, t.name AS task_name
FROM input_requests ir
JOIN users u ON u.id = ir.provider_id
JOIN deliverable_edges e ON e.id = ir.edge_id
JOIN tasks t ON t.id = e.target_task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE ir.id = $1 AND o.project_id = $2;

-- name: ListInputRequestsByProject :many
SELECT ir.*, u.display_name AS provider_name, e.target_task_id
FROM input_requests ir
JOIN users u ON u.id = ir.provider_id
JOIN deliverable_edges e ON e.id = ir.edge_id
JOIN tasks t ON t.id = e.target_task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE o.project_id = $1
ORDER BY ir.id;

-- name: ListUnnotifiedInputRequestsByTask :many
-- 任务入池通过后需要补发的站内通知（草稿与待审批阶段不打扰对接人）。
SELECT ir.*, e.name AS edge_name
FROM input_requests ir
JOIN deliverable_edges e ON e.id = ir.edge_id
WHERE e.target_task_id = $1 AND ir.notified_at IS NULL;

-- name: MarkInputRequestNotified :exec
UPDATE input_requests SET notified_at = now() WHERE id = $1;

-- name: AcceptInputRequest :one
UPDATE input_requests
SET state = 'accepted', accepted_at = now()
WHERE id = $1
RETURNING *;

-- name: ProvideInputRequest :one
-- 带附件时先落 uploading（$5），客户端确认写入后才由 CommitInputRequestFile 转 provided。
UPDATE input_requests
SET state = $5, provided_at = now(), provided_text = $2, file_name = $3, object_key = $4
WHERE id = $1
RETURNING *;

-- name: ResetStaleInputUploads :many
-- 迟迟未确认的输入附件：回到已接受状态，等对接人重新提交（预签名地址已过期）。
UPDATE input_requests
SET state = 'accepted', provided_at = NULL, provided_text = '', file_name = '', object_key = ''
WHERE state = 'uploading' AND provided_at < now() - $1::interval
RETURNING object_key;

-- name: CommitInputRequestUpload :one
-- 确认输入附件已写入对象存储：uploading → provided。
UPDATE input_requests
SET state = 'provided', provided_at = now()
WHERE id = $1 AND state = 'uploading'
RETURNING *;

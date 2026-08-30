-- name: CreateDeliverable :one
INSERT INTO deliverables (task_id, name, created_by)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetDeliverableInProject :one
SELECT d.*, t.owner_id AS task_owner_id, t.created_by AS task_created_by, t.status AS task_status,
    t.result_update AS task_result_update
FROM deliverables d
JOIN tasks t ON t.id = d.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE d.id = $1 AND t.id = $2 AND o.project_id = $3;

-- name: ListDeliverablesByTask :many
SELECT * FROM deliverables
WHERE task_id = $1
ORDER BY id;

-- name: ListDeliverableFilesByTask :many
SELECT df.*, u.display_name AS uploaded_by_name
FROM deliverable_files df
JOIN deliverables d ON d.id = df.deliverable_id
JOIN users u ON u.id = df.uploaded_by
WHERE d.task_id = $1
ORDER BY df.id;

-- name: ListDeliverableNamesByProject :many
-- 任务列表「预期交付物」列展示用。
SELECT d.task_id, d.name
FROM deliverables d
JOIN tasks t ON t.id = d.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE o.project_id = $1
ORDER BY d.id;

-- name: GetCandidateFile :one
SELECT * FROM deliverable_files
WHERE deliverable_id = $1 AND state = 'candidate'
LIMIT 1;

-- name: GetUploadingFile :one
-- 已建记录但尚未确认写入对象存储的内容（两阶段提交的中间态）。
SELECT * FROM deliverable_files
WHERE deliverable_id = $1 AND state = 'uploading'
LIMIT 1;

-- name: DeleteStaleUploadingFiles :many
-- 清理迟迟未确认的待上传记录（预签名地址已过期，客户端不会再来 commit）。
DELETE FROM deliverable_files
WHERE state = 'uploading' AND uploaded_at < now() - $1::interval
RETURNING object_key;

-- name: CommitUploadingFile :one
-- 确认上传：uploading → candidate，并按对象存储的真实大小回填。
UPDATE deliverable_files
SET state = 'candidate', file_size = $2, uploaded_at = now()
WHERE id = $1 AND state = 'uploading'
RETURNING *;

-- name: CreateDeliverableFile :one
INSERT INTO deliverable_files (deliverable_id, state, file_name, file_type, file_size, object_key, uploaded_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: DeleteDeliverableFile :execrows
DELETE FROM deliverable_files WHERE id = $1;

-- name: GetDeliverableFileInProject :one
SELECT df.*, u.display_name AS uploaded_by_name
FROM deliverable_files df
JOIN deliverables d ON d.id = df.deliverable_id
JOIN tasks t ON t.id = d.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users u ON u.id = df.uploaded_by
WHERE df.id = $1 AND o.project_id = $2;

-- name: CreateTaskFile :one
-- 两阶段提交第一步：先落 uploading 记录，确认写入对象存储后才转 ready。
INSERT INTO task_files (task_id, kind, state, file_name, file_type, file_size, object_key, note, uploaded_by)
VALUES ($1, $2, 'uploading', $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CommitTaskFile :one
-- 确认上传：uploading → ready，并按对象存储的真实大小回填。
UPDATE task_files
SET state = 'ready', file_size = $2, uploaded_at = now()
WHERE id = $1 AND state = 'uploading'
RETURNING *;

-- name: GetTaskFileInProject :one
SELECT tf.*, u.display_name AS uploaded_by_name, t.owner_id AS task_owner_id,
    t.created_by AS task_created_by, t.status AS task_status
FROM task_files tf
JOIN tasks t ON t.id = tf.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users u ON u.id = tf.uploaded_by
WHERE tf.id = $1 AND o.project_id = $2;

-- name: ListTaskFilesByTask :many
-- 已确认写入的过程文件与外部材料；uploading 记录不参与任何展示。
SELECT tf.*, u.display_name AS uploaded_by_name
FROM task_files tf
JOIN users u ON u.id = tf.uploaded_by
WHERE tf.task_id = $1 AND tf.state = 'ready'
ORDER BY tf.id;

-- name: ListTaskFilesByProject :many
SELECT tf.*, u.display_name AS uploaded_by_name
FROM task_files tf
JOIN tasks t ON t.id = tf.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users u ON u.id = tf.uploaded_by
WHERE o.project_id = $1 AND tf.state = 'ready'
ORDER BY tf.id;

-- name: DeleteTaskFile :execrows
DELETE FROM task_files WHERE id = $1;

-- name: DeleteStaleUploadingTaskFiles :many
-- 清理迟迟未确认的待上传记录（预签名地址已过期，客户端不会再来 commit）。
DELETE FROM task_files
WHERE state = 'uploading' AND uploaded_at < now() - $1::interval
RETURNING object_key;

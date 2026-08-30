-- name: CreatePackage :one
INSERT INTO artifact_packages (project_id, name, created_by)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreatePackageItem :exec
INSERT INTO artifact_package_items (package_id, deliverable_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: CreatePackageTaskFileItem :exec
-- 成果包也可以收过程文件与重要外部材料（§7.7「可以按需选择」）。
-- 入包时快照来源事实（所属任务名、文件名、类型）：来源文件被删除后条目仍在，靠快照还原「谁的什么文件」。
INSERT INTO artifact_package_items (package_id, task_file_id, source_task_name, source_file_name, source_file_kind)
SELECT sqlc.arg(package_id), tf.id, t.name, tf.file_name, tf.kind
FROM task_files tf
JOIN tasks t ON t.id = tf.task_id
WHERE tf.id = sqlc.arg(task_file_id)
ON CONFLICT DO NOTHING;

-- name: GetPackageInProject :one
SELECT * FROM artifact_packages
WHERE id = $1 AND project_id = $2;

-- name: ListPackagesByProject :many
SELECT p.*, u.display_name AS created_by_name
FROM artifact_packages p
JOIN users u ON u.id = p.created_by
WHERE p.project_id = $1
ORDER BY p.id DESC;

-- name: ListPackageItems :many
-- 目录项二选一：交付物项解析当前内容（被覆盖后自动指向新内容；退回删除后无当前内容则 file 为空），
-- 或任务文件直接给出自身（过程文件与外部材料没有版本概念）。两侧字段都可能为空，由调用方合并。
-- 任务文件被删除后 task_file_id 置空、tf 侧全空，条目靠 source_* 快照存活（F-10、§7.7）。
SELECT i.id, i.deliverable_id, i.task_file_id,
    d.name AS deliverable_name, dt.name AS deliverable_task_name,
    cf.id AS current_file_id, cf.file_name AS current_file_name,
    cf.object_key AS current_object_key, cf.effective_at,
    tf.kind AS file_kind, tf.file_name AS task_file_name,
    tf.object_key AS task_file_object_key, ft.name AS task_file_task_name,
    i.source_task_name, i.source_file_name, i.source_file_kind
FROM artifact_package_items i
LEFT JOIN deliverables d ON d.id = i.deliverable_id
LEFT JOIN tasks dt ON dt.id = d.task_id
LEFT JOIN deliverable_files cf ON cf.deliverable_id = d.id AND cf.state = 'current'
LEFT JOIN task_files tf ON tf.id = i.task_file_id
LEFT JOIN tasks ft ON ft.id = tf.task_id
WHERE i.package_id = $1
ORDER BY i.id;

-- name: ListDeliverablesByProject :many
-- 归档视角（AC-17）：项目全部交付物项与任务/KR/O 归属。
SELECT d.*, t.name AS task_name, t.status AS task_status, t.key_result_id, k.objective_id
FROM deliverables d
JOIN tasks t ON t.id = d.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE o.project_id = $1
ORDER BY d.id;

-- name: ListDeliverableFilesByProject :many
SELECT df.*, u.display_name AS uploaded_by_name
FROM deliverable_files df
JOIN deliverables d ON d.id = df.deliverable_id
JOIN tasks t ON t.id = d.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users u ON u.id = df.uploaded_by
WHERE o.project_id = $1
ORDER BY df.id;

-- name: CompletionReviewCountsByProject :many
SELECT cr.task_id, COUNT(*) AS n
FROM completion_reviews cr
JOIN tasks t ON t.id = cr.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE o.project_id = $1
GROUP BY cr.task_id;

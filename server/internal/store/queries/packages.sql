-- name: CreatePackage :one
INSERT INTO artifact_packages (project_id, name, created_by)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreatePackageItem :exec
INSERT INTO artifact_package_items (package_id, deliverable_id)
VALUES ($1, $2)
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
-- 目录项解析当前内容（被覆盖后自动指向新内容；退回删除后无当前内容则 file 为空）。
SELECT i.deliverable_id, d.name AS deliverable_name, t.name AS task_name,
    cf.id AS file_id, cf.file_name, cf.object_key, cf.effective_at
FROM artifact_package_items i
JOIN deliverables d ON d.id = i.deliverable_id
JOIN tasks t ON t.id = d.task_id
LEFT JOIN deliverable_files cf ON cf.deliverable_id = d.id AND cf.state = 'current'
WHERE i.package_id = $1
ORDER BY i.deliverable_id;

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

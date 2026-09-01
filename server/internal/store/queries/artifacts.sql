-- 成果归档（AC-17）：GetArtifacts 与项目报告共用的项目级清单查询。
-- 原与成果包查询同文件；裁决 G1（#140）删除成果包后仅保留这三条。

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

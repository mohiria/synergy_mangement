-- name: CreateEdge :one
INSERT INTO deliverable_edges (target_task_id, source_task_id, source_user_id, deliverable_id, name, edge_type, necessity, expected_date, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetEdgeInProject :one
SELECT e.*, tt.owner_id AS target_owner_id, tt.created_by AS target_created_by, tt.status AS target_status,
    k.owner_id AS target_kr_owner_id
FROM deliverable_edges e
JOIN tasks tt ON tt.id = e.target_task_id
JOIN key_results k ON k.id = tt.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE e.id = $1 AND o.project_id = $2;

-- name: DeleteEdge :execrows
DELETE FROM deliverable_edges WHERE id = $1;

-- name: ListEdgesByProject :many
-- 项目全部交付物边，含两端任务事实与就绪派生所需的当前／候选内容存在性（AC-48）。
SELECT e.*,
    st.name AS source_task_name, st.status AS source_task_status,
    st.owner_id AS source_owner_id, su.display_name AS source_owner_name,
    mu.display_name AS source_user_name,
    tt.name AS target_task_name, tt.owner_id AS target_owner_id, tt.created_by AS target_created_by,
    d.name AS deliverable_name,
    cf.id AS current_file_id, cf.file_name AS current_file_name, cf.file_size AS current_file_size,
    EXISTS (
        SELECT 1 FROM deliverable_files df
        WHERE df.deliverable_id = e.deliverable_id AND df.state = 'candidate'
    ) AS has_candidate
FROM deliverable_edges e
JOIN tasks tt ON tt.id = e.target_task_id
JOIN key_results k ON k.id = tt.key_result_id
JOIN objectives o ON o.id = k.objective_id
LEFT JOIN tasks st ON st.id = e.source_task_id
LEFT JOIN users su ON su.id = st.owner_id
LEFT JOIN users mu ON mu.id = e.source_user_id
LEFT JOIN deliverables d ON d.id = e.deliverable_id
LEFT JOIN deliverable_files cf ON cf.deliverable_id = e.deliverable_id AND cf.state = 'current'
WHERE o.project_id = $1
ORDER BY e.id;

-- name: ListCurrentFilesByProjectTask :many
-- 裁决 J1（#142）：关系列表「当前交付物」列——项目内各任务全部已生效当前内容，
-- 供边未绑定具体交付物项时按来源任务归组展示。
SELECT d.task_id, df.id AS file_id, df.file_name, df.file_size
FROM deliverable_files df
JOIN deliverables d ON d.id = df.deliverable_id
JOIN tasks t ON t.id = d.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE o.project_id = $1 AND df.state = 'current'
ORDER BY df.id;

-- name: ListEdgeRefsByDeliverableTask :many
-- 交付物承接的关系边（AC-17 归档视角「来源关系边」列）：按来源交付物所属任务过滤。
SELECT e.id, e.deliverable_id, e.name, e.edge_type, e.target_task_id,
    tt.name AS target_task_name
FROM deliverable_edges e
JOIN tasks tt ON tt.id = e.target_task_id
JOIN deliverables d ON d.id = e.deliverable_id
WHERE d.task_id = $1
ORDER BY e.id;

-- name: ListEdgeRefsByProject :many
-- 同上，项目全量：归档视角一次性取齐所有交付物的来源关系边。
SELECT e.id, e.deliverable_id, e.name, e.edge_type, e.target_task_id,
    tt.name AS target_task_name
FROM deliverable_edges e
JOIN tasks tt ON tt.id = e.target_task_id
JOIN key_results k ON k.id = tt.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN deliverables d ON d.id = e.deliverable_id
WHERE o.project_id = $1
ORDER BY e.id;

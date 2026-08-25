-- name: CreateEdge :one
INSERT INTO deliverable_edges (target_task_id, source_task_id, source_user_id, deliverable_id, name, edge_type, necessity, expected_date, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetEdgeInProject :one
SELECT e.*, tt.owner_id AS target_owner_id, tt.created_by AS target_created_by, tt.status AS target_status
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
    su.display_name AS source_owner_name,
    mu.display_name AS source_user_name,
    tt.name AS target_task_name, tt.owner_id AS target_owner_id, tt.created_by AS target_created_by,
    d.name AS deliverable_name,
    cf.id AS current_file_id, cf.file_name AS current_file_name,
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

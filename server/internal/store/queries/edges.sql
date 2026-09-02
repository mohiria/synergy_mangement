-- name: CreateEdge :one
-- #178：来源恒为任务（source_user_id 与 expected_date 已随输入请求机制删除）。
INSERT INTO deliverable_edges (target_task_id, source_task_id, name, necessity, created_by)
VALUES ($1, $2, $3, $4, $5)
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
-- 项目全部交付物边，含两端任务事实（AC-48 修订，裁决 #163：就绪只看来源任务状态，
-- 成员来源由输入请求状态判定，均在 domain/handler 计算）。
SELECT e.*,
    st.name AS source_task_name, st.status AS source_task_status,
    st.end_date AS source_end_date,
    st.owner_id AS source_owner_id, su.display_name AS source_owner_name,
    tt.name AS target_task_name, tt.owner_id AS target_owner_id, tt.created_by AS target_created_by
FROM deliverable_edges e
JOIN tasks tt ON tt.id = e.target_task_id
JOIN key_results k ON k.id = tt.key_result_id
JOIN objectives o ON o.id = k.objective_id
LEFT JOIN tasks st ON st.id = e.source_task_id
LEFT JOIN users su ON su.id = st.owner_id
WHERE o.project_id = $1
ORDER BY e.id;

-- name: ListInputReadinessByProject :many
-- #150 风险队列「未就绪摘要」：项目全部输入边的就绪事实——目标任务所属 KR、
-- 目标任务状态、来源任务状态（与 AC-48 修订同源；#178 后来源恒为任务）；计数规则在 domain。
SELECT tt.key_result_id, tt.status AS target_status,
    st.status AS source_task_status
FROM deliverable_edges e
JOIN tasks tt ON tt.id = e.target_task_id
JOIN key_results k ON k.id = tt.key_result_id
JOIN objectives o ON o.id = k.objective_id
LEFT JOIN tasks st ON st.id = e.source_task_id
WHERE o.project_id = $1;

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
-- 交付物承接的关系边（AC-17 归档视角「来源关系边」列；裁决 #163：按来源任务归属）。
SELECT e.id, e.source_task_id, e.name, e.necessity, e.target_task_id,
    tt.name AS target_task_name
FROM deliverable_edges e
JOIN tasks tt ON tt.id = e.target_task_id
WHERE e.source_task_id = $1
ORDER BY e.id;

-- name: ListEdgeRefsByProject :many
-- 同上，项目全量：归档视角一次性取齐所有来源任务的输出边。
SELECT e.id, e.source_task_id, e.name, e.necessity, e.target_task_id,
    tt.name AS target_task_name
FROM deliverable_edges e
JOIN tasks tt ON tt.id = e.target_task_id
JOIN key_results k ON k.id = tt.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE e.source_task_id IS NOT NULL AND o.project_id = $1
ORDER BY e.id;

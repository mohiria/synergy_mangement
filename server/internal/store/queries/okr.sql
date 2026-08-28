-- name: ListObjectives :many
SELECT * FROM objectives
WHERE project_id = $1
ORDER BY sort_order, id;

-- name: ListKeyResultsByProject :many
-- owner_name：KR 负责人姓名（派生字段，前端直接展示；未指定负责人时为 NULL）。
SELECT kr.*, u.display_name AS owner_name
FROM key_results kr
JOIN objectives o ON o.id = kr.objective_id
LEFT JOIN users u ON u.id = kr.owner_id
WHERE o.project_id = $1
ORDER BY kr.sort_order, kr.id;

-- name: GetObjective :one
SELECT * FROM objectives
WHERE id = $1 AND project_id = $2;

-- name: CreateObjective :one
-- 排序追加到项目末尾；批量创建在同一事务内串行执行，MAX+1 不会互相踩踏。
INSERT INTO objectives (project_id, title, description, sort_order)
VALUES ($1, $2, $3, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM objectives WHERE project_id = $1))
RETURNING *;

-- name: CreateKeyResult :one
INSERT INTO key_results (objective_id, description, metric, owner_id, start_date, end_date, sort_order)
VALUES ($1, $2, $3, $4, $5, $6, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM key_results WHERE objective_id = $1))
RETURNING *;

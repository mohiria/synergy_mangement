-- name: ListObjectives :many
SELECT o.*, cu.display_name AS created_by_name
FROM objectives o
LEFT JOIN users cu ON cu.id = o.created_by
WHERE o.project_id = $1
ORDER BY o.sort_order, o.id;

-- name: ListKeyResultsByProject :many
-- created_by_name：创建人姓名（裁决 12，#183；存量无创建人时为 NULL，前端显示「—」）。
SELECT kr.*, cu.display_name AS created_by_name, o.code_seq AS objective_code_seq
FROM key_results kr
JOIN objectives o ON o.id = kr.objective_id
LEFT JOIN users cu ON cu.id = kr.created_by
WHERE o.project_id = $1
ORDER BY kr.sort_order, kr.id;

-- name: GetObjective :one
SELECT * FROM objectives
WHERE id = $1 AND project_id = $2;

-- name: CreateObjective :one
-- 排序与编号序号都追加到项目末尾；批量创建在同一事务内串行执行，MAX+1 不会互相踩踏。
-- code_seq 取历史最大值加一，不复用被删 O 的序号（AC-64）。
INSERT INTO objectives (project_id, title, description, created_by, sort_order, code_seq)
VALUES ($1, $2, $3, $4,
    (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM objectives WHERE project_id = $1),
    (SELECT COALESCE(MAX(code_seq), 0) + 1 FROM objectives WHERE project_id = $1))
RETURNING *;

-- name: CreateKeyResult :one
INSERT INTO key_results (objective_id, description, metric, created_by, sort_order, code_seq)
VALUES ($1, $2, $3, $4,
    (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM key_results WHERE objective_id = $1),
    (SELECT COALESCE(MAX(code_seq), 0) + 1 FROM key_results WHERE objective_id = $1))
RETURNING *;

-- name: UpdateObjective :one
-- 只覆盖拟议的字段（NULL 表示不改，AC-65）。
UPDATE objectives
SET title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description)
WHERE id = sqlc.arg('id') AND project_id = sqlc.arg('project_id')
RETURNING *;

-- name: UpdateKeyResult :one
-- 裁决 12（#183）：KR 只剩结构字段（描述、量化指标）。
UPDATE key_results
SET description = COALESCE(sqlc.narg('description'), description),
    metric = COALESCE(sqlc.narg('metric'), metric)
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteObjective :execrows
DELETE FROM objectives WHERE id = $1 AND project_id = $2;

-- name: DeleteKeyResult :execrows
DELETE FROM key_results WHERE id = $1;

-- name: CountKeyResultsByObjective :one
SELECT COUNT(*) AS n FROM key_results WHERE objective_id = $1;

-- name: CountTasksByKeyResultInProject :many
-- 每个 KR 下的任务数（含已完成与已取消，AC-65 删除守卫与 OKR 表「任务」列共用）。
SELECT kr.id AS key_result_id, COUNT(t.id) AS n
FROM key_results kr
JOIN objectives o ON o.id = kr.objective_id
LEFT JOIN tasks t ON t.key_result_id = kr.id
WHERE o.project_id = $1
GROUP BY kr.id;

-- name: ListTasksOwnedBy :many
SELECT t.id, t.name
FROM tasks t
JOIN key_results kr ON kr.id = t.key_result_id
JOIN objectives o ON o.id = kr.objective_id
WHERE o.project_id = $1 AND t.owner_id = $2 AND t.status NOT IN ('completed', 'cancelled');

-- name: ListReviewerDutiesOf :many
SELECT DISTINCT t.name
FROM task_reviewers tr
JOIN tasks t ON t.id = tr.task_id
JOIN key_results kr ON kr.id = t.key_result_id
JOIN objectives o ON o.id = kr.objective_id
WHERE o.project_id = $1 AND tr.user_id = $2 AND t.status NOT IN ('completed', 'cancelled');

-- name: ListReceiverDutiesOf :many
SELECT DISTINCT t.name
FROM task_receivers rc
JOIN tasks t ON t.id = rc.task_id
JOIN key_results kr ON kr.id = t.key_result_id
JOIN objectives o ON o.id = kr.objective_id
WHERE o.project_id = $1 AND rc.user_id = $2 AND t.status NOT IN ('completed', 'cancelled');


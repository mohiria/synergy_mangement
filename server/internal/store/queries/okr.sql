-- name: ListObjectives :many
SELECT * FROM objectives
WHERE project_id = $1
ORDER BY sort_order, id;

-- name: ListKeyResultsByProject :many
-- owner_name：KR 负责人姓名（派生字段，前端直接展示；未指定负责人时为 NULL）。
SELECT kr.*, u.display_name AS owner_name, o.code_seq AS objective_code_seq
FROM key_results kr
JOIN objectives o ON o.id = kr.objective_id
LEFT JOIN users u ON u.id = kr.owner_id
WHERE o.project_id = $1
ORDER BY kr.sort_order, kr.id;

-- name: GetObjective :one
SELECT * FROM objectives
WHERE id = $1 AND project_id = $2;

-- name: CreateObjective :one
-- 排序与编号序号都追加到项目末尾；批量创建在同一事务内串行执行，MAX+1 不会互相踩踏。
-- code_seq 取历史最大值加一，不复用被删 O 的序号（AC-64）。
INSERT INTO objectives (project_id, title, description, sort_order, code_seq)
VALUES ($1, $2, $3,
    (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM objectives WHERE project_id = $1),
    (SELECT COALESCE(MAX(code_seq), 0) + 1 FROM objectives WHERE project_id = $1))
RETURNING *;

-- name: CreateKeyResult :one
INSERT INTO key_results (objective_id, description, metric, owner_id, start_date, end_date, sort_order, code_seq)
VALUES ($1, $2, $3, $4, $5, $6,
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
UPDATE key_results
SET description = COALESCE(sqlc.narg('description'), description),
    metric = COALESCE(sqlc.narg('metric'), metric),
    owner_id = COALESCE(sqlc.narg('owner_id'), owner_id),
    start_date = COALESCE(sqlc.narg('start_date'), start_date),
    end_date = COALESCE(sqlc.narg('end_date'), end_date)
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

-- name: CountPendingApprovalsByKeyResult :one
-- 该 KR 下未决审批单条数：待审批关闭申请、待终审完成申请
-- （AC-61 交接确认；裁决 #162 无入池审批，裁决 #172 转交范围缩小为完成审批与关闭申请）。
SELECT (
    (SELECT COUNT(*) FROM field_change_requests fc
        JOIN tasks t ON t.id = fc.task_id
        WHERE t.key_result_id = $1 AND fc.state = 'pending')
  + (SELECT COUNT(*) FROM tasks t WHERE t.key_result_id = $1 AND t.status = 'pending_final_review')
) AS n;

-- name: ListKeyResultsOwnedBy :many
SELECT kr.id, kr.description
FROM key_results kr
JOIN objectives o ON o.id = kr.objective_id
WHERE o.project_id = $1 AND kr.owner_id = $2;

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


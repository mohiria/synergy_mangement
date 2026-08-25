-- name: CreateFieldChange :one
INSERT INTO field_change_requests (
    task_id, submitted_by, reason, state, exempt, opinion, decided_by, decided_at,
    old_name, new_name, old_description, new_description,
    old_completion_criteria, new_completion_criteria,
    old_owner_id, new_owner_id, old_end_date, new_end_date
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
RETURNING *;

-- name: GetFieldChange :one
SELECT * FROM field_change_requests
WHERE id = $1 AND task_id = $2;

-- name: ListFieldChangesByTask :many
SELECT fc.*, su.display_name AS submitted_by_name, du.display_name AS decided_by_name
FROM field_change_requests fc
JOIN users su ON su.id = fc.submitted_by
LEFT JOIN users du ON du.id = fc.decided_by
WHERE fc.task_id = $1
ORDER BY fc.id DESC;

-- name: LatestFieldChangesByProject :many
-- 每个任务最近一张需要关注的变更单（待审批，或退回未处理），列表标示用。
SELECT DISTINCT ON (fc.task_id) fc.*,
    su.display_name AS submitted_by_name,
    du.display_name AS decided_by_name
FROM field_change_requests fc
JOIN tasks t ON t.id = fc.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users su ON su.id = fc.submitted_by
LEFT JOIN users du ON du.id = fc.decided_by
WHERE o.project_id = $1
  AND (fc.state = 'pending' OR (fc.state = 'rejected' AND NOT fc.resolved))
ORDER BY fc.task_id, fc.id DESC;

-- name: HasPendingFieldChange :one
SELECT EXISTS (
    SELECT 1 FROM field_change_requests WHERE task_id = $1 AND state = 'pending'
) AS has_pending;

-- name: DecideFieldChange :one
UPDATE field_change_requests
SET state = $2, opinion = $3, decided_by = $4, decided_at = now()
WHERE id = $1
RETURNING *;

-- name: ResolveFieldChange :one
UPDATE field_change_requests
SET resolved = TRUE
WHERE id = $1
RETURNING *;

-- name: ResolveRejectedFieldChanges :execrows
-- 原提交人重新提交时，其此前退回未处理的变更单视为已处理（词汇表「退回待处理事项」）。
UPDATE field_change_requests
SET resolved = TRUE
WHERE task_id = $1 AND submitted_by = $2 AND state = 'rejected' AND NOT resolved;

-- name: ApplyTaskKeyFields :one
-- 变更单通过／免审生效：仅覆盖拟议的字段（NULL 表示未修改）。
UPDATE tasks
SET name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    completion_criteria = COALESCE(sqlc.narg('completion_criteria'), completion_criteria),
    owner_id = COALESCE(sqlc.narg('owner_id'), owner_id),
    end_date = COALESCE(sqlc.narg('end_date'), end_date)
WHERE id = sqlc.arg('id')
RETURNING *;

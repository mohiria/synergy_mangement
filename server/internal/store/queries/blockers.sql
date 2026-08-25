-- name: CreateBlocker :one
INSERT INTO blockers (task_id, kind, missing, reason, action_owner_id, level, expected_recovery_date, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetBlockerInProject :one
SELECT b.*, t.name AS task_name, t.owner_id AS task_owner_id, t.created_by AS task_created_by
FROM blockers b
JOIN tasks t ON t.id = b.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE b.id = $1 AND o.project_id = $2;

-- name: ListBlockersByProject :many
SELECT b.*, au.display_name AS action_owner_name, cu.display_name AS created_by_name,
    t.owner_id AS task_owner_id, t.created_by AS task_created_by,
    t.name AS task_name, k.owner_id AS kr_owner_id
FROM blockers b
JOIN tasks t ON t.id = b.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users au ON au.id = b.action_owner_id
JOIN users cu ON cu.id = b.created_by
WHERE o.project_id = $1
ORDER BY (b.state = 'open') DESC, b.id DESC;

-- name: ListBlockersByTask :many
SELECT b.*, au.display_name AS action_owner_name, cu.display_name AS created_by_name
FROM blockers b
JOIN users au ON au.id = b.action_owner_id
JOIN users cu ON cu.id = b.created_by
WHERE b.task_id = $1
ORDER BY (b.state = 'open') DESC, b.id DESC;

-- name: OpenBlockerCountsByProject :many
SELECT b.task_id, COUNT(*) AS n
FROM blockers b
JOIN tasks t ON t.id = b.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE o.project_id = $1 AND b.state = 'open'
GROUP BY b.task_id;

-- name: ResolveBlocker :one
UPDATE blockers
SET state = 'resolved', resolved_at = now(), resolved_note = $2
WHERE id = $1
RETURNING *;

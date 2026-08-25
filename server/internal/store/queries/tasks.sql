-- name: CreateTask :one
INSERT INTO tasks (key_result_id, name, owner_id, start_date, end_date, status, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetTaskInProject :one
-- 任务连同所属 KR 负责人与项目归属（用于权限判定与项目内寻址）。
SELECT t.*, k.owner_id AS kr_owner_id, o.project_id
FROM tasks t
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE t.id = $1 AND o.project_id = $2;

-- name: ListProjectTasks :many
-- 项目全部任务，含负责人姓名与 KR 负责人（派生动作标志在 domain 判定）。
SELECT t.*, u.display_name AS owner_name, k.owner_id AS kr_owner_id
FROM tasks t
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users u ON u.id = t.owner_id
WHERE o.project_id = $1
ORDER BY t.id;

-- name: UpdateTaskStatus :one
UPDATE tasks
SET status = $2
WHERE id = $1
RETURNING *;

-- name: UpdateTaskStatusWithReason :one
UPDATE tasks
SET status = $2, cancel_reason = $3
WHERE id = $1
RETURNING *;

-- name: UpdateTaskProgress :one
UPDATE tasks
SET progress = $2
WHERE id = $1
RETURNING *;

-- name: ListTaskProgressByProject :many
-- KR 层进度覆盖度的原始事实（状态与可选进度），聚合规则在 domain。
SELECT t.key_result_id, t.status, t.progress
FROM tasks t
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE o.project_id = $1;

-- name: GetKeyResultInProject :one
-- KR 连同项目归属（任务创建时校验所属 KR 属于本项目）。
SELECT k.*, o.project_id
FROM key_results k
JOIN objectives o ON o.id = k.objective_id
WHERE k.id = $1 AND o.project_id = $2;

-- name: CreatePoolReview :one
INSERT INTO pool_reviews (task_id, submitted_by, status, exempt, opinion, decided_by, decided_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetLatestPoolReview :one
SELECT * FROM pool_reviews
WHERE task_id = $1
ORDER BY id DESC
LIMIT 1;

-- name: LatestPoolReviewsByProject :many
-- 每个任务最近一次入池审批单，连同提交人／处理人姓名（列表展示用）。
SELECT DISTINCT ON (pr.task_id) pr.*,
    su.display_name AS submitted_by_name,
    du.display_name AS decided_by_name
FROM pool_reviews pr
JOIN tasks t ON t.id = pr.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users su ON su.id = pr.submitted_by
LEFT JOIN users du ON du.id = pr.decided_by
WHERE o.project_id = $1
ORDER BY pr.task_id, pr.id DESC;

-- name: DecidePoolReview :one
UPDATE pool_reviews
SET status = $2, opinion = $3, decided_by = $4, decided_at = now()
WHERE id = $1
RETURNING *;

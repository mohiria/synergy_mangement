-- name: CreateTask :one
-- code_seq 取所属 KR 下历史最大值加一，不复用被删任务的序号（AC-64）；
-- 批量创建在同一事务内串行执行，MAX+1 不会互相踩踏。
INSERT INTO tasks (key_result_id, name, owner_id, start_date, end_date, status, created_by, code_seq)
VALUES ($1, $2, $3, $4, $5, $6, $7,
    (SELECT COALESCE(MAX(code_seq), 0) + 1 FROM tasks WHERE key_result_id = $1))
RETURNING *;

-- name: GetTaskInProject :one
-- 任务连同所属 KR 负责人与项目归属（用于权限判定与项目内寻址）。
SELECT t.*, k.owner_id AS kr_owner_id, o.project_id
FROM tasks t
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE t.id = $1 AND o.project_id = $2;

-- name: LockTaskInProject :one
-- 与 GetTaskInProject 同形，但对任务行加写锁：三道审批的决策必须在锁内重读状态、重跑规则，
-- 否则并发决策（如或签一人通过、一人退回）会各自基于过期事实写库。
SELECT t.*, k.owner_id AS kr_owner_id, o.project_id
FROM tasks t
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE t.id = $1 AND o.project_id = $2
FOR UPDATE OF t;

-- name: ListProjectTasks :many
-- 项目全部任务，含负责人／创建人／KR 负责人姓名（派生动作标志与待行动人在 domain 判定）。
SELECT t.*, u.display_name AS owner_name, cu.display_name AS creator_name,
    k.owner_id AS kr_owner_id, ku.display_name AS kr_owner_name,
    k.code_seq AS kr_code_seq, o.code_seq AS objective_code_seq
FROM tasks t
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
JOIN users u ON u.id = t.owner_id
JOIN users cu ON cu.id = t.created_by
LEFT JOIN users ku ON ku.id = k.owner_id
WHERE o.project_id = $1
ORDER BY t.id;

-- name: ListPoolReviewsByTask :many
-- 任务全部入池审批记录（词汇表「审核记录」），最新在前。
SELECT pr.*, su.display_name AS submitted_by_name, du.display_name AS decided_by_name
FROM pool_reviews pr
JOIN users su ON su.id = pr.submitted_by
LEFT JOIN users du ON du.id = pr.decided_by
WHERE pr.task_id = $1
ORDER BY pr.id DESC;

-- name: UpdateTaskStatus :one
UPDATE tasks
SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateTaskStatusWithReason :one
UPDATE tasks
SET status = $2, cancel_reason = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetTaskResultUpdate :one
-- 成果更新进程流转（AC-66）：发起→open，提交完成申请→reviewing，终审通过或退回→''。
-- 任务生命周期状态不在这里动。
UPDATE tasks
SET result_update = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateTaskProgress :one
UPDATE tasks
SET progress = $2, updated_at = now()
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
-- KR 连同项目归属与所属 O 的编号序号（任务创建时校验归属；邀请通知要拼 KR 编号）。
SELECT k.*, o.project_id, o.code_seq AS objective_code_seq
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

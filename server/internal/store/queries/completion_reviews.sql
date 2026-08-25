-- name: SetTaskReviewer :exec
INSERT INTO task_reviewers (task_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ClearTaskReviewers :execrows
DELETE FROM task_reviewers WHERE task_id = $1;

-- name: ListTaskReviewers :many
SELECT tr.user_id, u.display_name
FROM task_reviewers tr
JOIN users u ON u.id = tr.user_id
WHERE tr.task_id = $1
ORDER BY tr.user_id;

-- name: CreateReviewReviewer :exec
INSERT INTO completion_review_reviewers (review_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListReviewReviewers :many
SELECT crr.user_id, u.display_name
FROM completion_review_reviewers crr
JOIN users u ON u.id = crr.user_id
WHERE crr.review_id = $1
ORDER BY crr.user_id;

-- name: CreateCompletionReview :one
INSERT INTO completion_reviews (task_id, submitted_by, note, state)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateCompletionReviewItem :exec
INSERT INTO completion_review_items (review_id, deliverable_id, deliverable_name, file_name, file_id)
VALUES ($1, $2, $3, $4, $5);

-- name: GetCompletionReview :one
SELECT * FROM completion_reviews
WHERE id = $1 AND task_id = $2;

-- name: ListCompletionReviewsByTask :many
SELECT cr.*, su.display_name AS submitted_by_name, du.display_name AS decided_by_name
FROM completion_reviews cr
JOIN users su ON su.id = cr.submitted_by
LEFT JOIN users du ON du.id = cr.decided_by
WHERE cr.task_id = $1
ORDER BY cr.id DESC;

-- name: ListCompletionReviewItems :many
SELECT * FROM completion_review_items
WHERE review_id = $1
ORDER BY deliverable_id;

-- name: RecordIntermediateApproval :one
-- 或签任一人通过：记录处理事实并进入待 KR 终审（其余待办随状态关闭）。
UPDATE completion_reviews
SET state = $2, intermediate_by = $3, intermediate_at = now(), intermediate_opinion = $4
WHERE id = $1
RETURNING *;

-- name: DecideCompletionReview :one
UPDATE completion_reviews
SET state = $2, opinion = $3, decided_by = $4, decided_at = now()
WHERE id = $1
RETURNING *;

-- name: ListCandidateFilesByTask :many
-- 任务当前全部候选内容（提交完成申请时整体纳入）。
SELECT df.*, d.name AS deliverable_name
FROM deliverable_files df
JOIN deliverables d ON d.id = df.deliverable_id
WHERE d.task_id = $1 AND df.state = 'candidate'
ORDER BY df.id;

-- name: LatestCompletionReviewsByProject :many
-- 每个任务最近一次完成申请（我的工作分组用），含任务事实。
SELECT DISTINCT ON (cr.task_id) cr.*,
    t.name AS task_name, t.owner_id AS task_owner_id, t.end_date AS task_end_date,
    k.owner_id AS kr_owner_id
FROM completion_reviews cr
JOIN tasks t ON t.id = cr.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE o.project_id = $1
ORDER BY cr.task_id, cr.id DESC;

-- name: CandidateCountsByProject :many
-- 每个任务的候选内容数量（canSubmitCompletion 派生用）。
SELECT d.task_id, COUNT(*) AS n
FROM deliverable_files df
JOIN deliverables d ON d.id = df.deliverable_id
JOIN tasks t ON t.id = d.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE o.project_id = $1 AND df.state = 'candidate'
GROUP BY d.task_id;

-- name: GetCurrentFile :one
SELECT * FROM deliverable_files
WHERE deliverable_id = $1 AND state = 'current'
LIMIT 1;

-- name: PromoteCandidateToCurrent :one
-- 终审通过：候选成为当前内容并盖生效时间（旧当前内容由调用方先删除）。
UPDATE deliverable_files
SET state = 'current', effective_at = now()
WHERE id = $1
RETURNING *;

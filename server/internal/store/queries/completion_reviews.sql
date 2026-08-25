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

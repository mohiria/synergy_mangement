-- name: CreateDiscussion :one
INSERT INTO discussions (task_id, author_id, content)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateDiscussionMention :exec
INSERT INTO discussion_mentions (discussion_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListDiscussionsByTask :many
-- 讨论按时间正序，含作者与被 @ 成员姓名。
SELECT d.*, u.display_name AS author_name,
    COALESCE(array_agg(mu.display_name ORDER BY mu.display_name) FILTER (WHERE mu.id IS NOT NULL), '{}')::text[] AS mention_names
FROM discussions d
JOIN users u ON u.id = d.author_id
LEFT JOIN discussion_mentions dm ON dm.discussion_id = d.id
LEFT JOIN users mu ON mu.id = dm.user_id
WHERE d.task_id = $1
GROUP BY d.id, u.display_name
ORDER BY d.id;

-- name: CreateNotification :one
INSERT INTO notifications (user_id, kind, content, project_id, task_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListNotificationsByUser :many
-- 分页取通知（P1）：before_id 为 0 表示取最新一页，否则取 id 更小的更早一页。
SELECT * FROM notifications
WHERE user_id = $1
  AND (sqlc.arg('before_id')::bigint = 0 OR id < sqlc.arg('before_id')::bigint)
ORDER BY id DESC
LIMIT sqlc.arg('row_limit')::int;

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications
SET read_at = now()
WHERE user_id = $1 AND read_at IS NULL;

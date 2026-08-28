-- name: CreateTaskActivity :one
-- 追加一条任务动态（ADR 0002）；文案在写入时定型。
INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListTaskActivitiesByTask :many
-- 任务动态，最新在前；连同行动人姓名（系统派生事件为空）。
SELECT a.*, u.display_name AS actor_name
FROM task_activities a
LEFT JOIN users u ON u.id = a.actor_id
WHERE a.task_id = $1
ORDER BY a.id DESC;

-- name: CreateBlockerActivity :execrows
-- 卡点动态：系统派生、无行动人。去重由 BlockerActivityOpen 判定，不再靠唯一键。
INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at, blocker_key)
VALUES ($1, $2, NULL, $3, $4, $5);

-- name: BlockerActivityOpen :one
-- 该卡点当前是否处于「已记出现、尚未记解除」的状态（R9 去重口径：自上次解除以来只记一条出现）。
SELECT COALESCE(
    (SELECT a.kind = 'blocker_opened'
       FROM task_activities a
      WHERE a.task_id = $1 AND a.blocker_key = $2
        AND a.kind IN ('blocker_opened', 'blocker_resolved')
      ORDER BY a.id DESC LIMIT 1),
    FALSE)::boolean AS is_open;

-- name: ListOpenBlockerActivities :many
-- 全库仍处于「出现未解除」的卡点动态（ticker 补偿扫描解除事件用，R9）。
SELECT DISTINCT ON (a.task_id, a.blocker_key) a.task_id, a.blocker_key, a.summary, a.kind
FROM task_activities a
JOIN tasks t ON t.id = a.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE a.blocker_key IS NOT NULL
  AND a.kind IN ('blocker_opened', 'blocker_resolved')
  AND o.project_id = $1
ORDER BY a.task_id, a.blocker_key, a.id DESC;

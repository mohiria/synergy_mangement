-- name: EnqueueObjectDeletion :exec
-- 删除失败排队重试（E3）；同一个对象重复失败只累加次数。
INSERT INTO pending_object_deletions (object_key, last_error)
VALUES ($1, $2)
ON CONFLICT (object_key) DO UPDATE
SET attempts = pending_object_deletions.attempts + 1,
    last_error = EXCLUDED.last_error,
    last_try_at = now();

-- name: ListPendingObjectDeletions :many
SELECT * FROM pending_object_deletions
ORDER BY last_try_at
LIMIT $1;

-- name: DeletePendingObjectDeletion :exec
DELETE FROM pending_object_deletions WHERE object_key = $1;

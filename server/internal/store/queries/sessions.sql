-- name: CreateSession :exec
INSERT INTO sessions (token, user_id, expires_at)
VALUES ($1, $2, $3);

-- name: GetSessionUser :one
SELECT u.* FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token = $1 AND s.expires_at > now();

-- name: GetSession :one
SELECT * FROM sessions WHERE token = $1 AND expires_at > now();

-- name: UpdateSessionExpiry :exec
UPDATE sessions SET expires_at = $2 WHERE token = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= now();

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: DeleteOtherUserSessions :execrows
-- 改密码后吊销本人其余会话（S3）：当前会话保留，免得改完自己被踢出去。
DELETE FROM sessions WHERE user_id = $1 AND token <> $2;

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
-- 滑动续期时一并记最近活动时间（#208）。
UPDATE sessions SET expires_at = $2, last_active_at = now() WHERE token = $1;

-- name: ListUserSessions :many
-- #208：本人活跃会话（未过期），最新活动在前。
SELECT token, created_at, last_active_at, expires_at FROM sessions
WHERE user_id = $1 AND expires_at > now()
ORDER BY last_active_at DESC, created_at DESC;

-- name: UpdateUserLastLogin :exec
-- #208：登录成功记录最近登录时间。
UPDATE users SET last_login_at = $2 WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= now();

-- name: UpdateUserPassword :exec
-- 本人改密同时清除「须改密码」标记（#203）；管理员重置走另一条查询（置真）。
UPDATE users SET password_hash = $2, must_change_password = false WHERE id = $1;

-- name: DeleteUserSessions :execrows
-- #204：停用账号时吊销其全部会话。
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteOtherUserSessions :execrows
-- 改密码后吊销本人其余会话（S3）：当前会话保留，免得改完自己被踢出去。
DELETE FROM sessions WHERE user_id = $1 AND token <> $2;

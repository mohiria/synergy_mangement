-- name: InvalidatePasswordResetTokens :exec
-- #214：同一用户新请求作废旧 token。
UPDATE password_reset_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL;

-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetPasswordResetToken :one
SELECT * FROM password_reset_tokens WHERE token_hash = $1;

-- name: ConsumePasswordResetToken :execrows
-- #215：原子消费。未使用、未过期且用户未停用才会被标记，预检查之后才过期或被停用的
-- 请求同样拿到 0 行被拒；返回 0 行也可能是并发请求抢先用掉。
UPDATE password_reset_tokens t SET used_at = now()
FROM users u
WHERE t.id = $1 AND t.used_at IS NULL AND t.expires_at > now()
  AND u.id = t.user_id AND u.disabled_at IS NULL;

-- name: LockUserForPasswordReset :one
-- #215：签发重置 token 时按用户加行锁，串行化同一账号的并发请求。
SELECT id FROM users WHERE id = $1 FOR UPDATE;

-- name: GetUserByUsernameOrEmail :one
-- 找回密码按用户名或邮箱定位账号（邮箱大小写不敏感）。
SELECT * FROM users WHERE username = $1 OR lower(email) = lower($1) LIMIT 1;

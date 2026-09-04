-- name: InvalidatePasswordResetTokens :exec
-- #214：同一用户新请求作废旧 token。
UPDATE password_reset_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL;

-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetPasswordResetToken :one
SELECT * FROM password_reset_tokens WHERE token_hash = $1;

-- name: MarkPasswordResetTokenUsed :exec
UPDATE password_reset_tokens SET used_at = now() WHERE id = $1;

-- name: GetUserByUsernameOrEmail :one
-- 找回密码按用户名或邮箱定位账号（邮箱大小写不敏感）。
SELECT * FROM users WHERE username = $1 OR lower(email) = lower($1) LIMIT 1;

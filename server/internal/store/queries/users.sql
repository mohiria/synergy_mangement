-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: ListUsers :many
SELECT id, username, display_name, email FROM users ORDER BY id;

-- name: ListSystemUsers :many
-- #201：系统设置 → 用户管理列表（仅系统管理员）。
SELECT id, username, display_name, email, is_system_admin, created_at FROM users ORDER BY id;

-- name: GetUserByEmail :one
-- #202：邮箱大小写不敏感（唯一索引建在 lower(email) 上）。
SELECT * FROM users WHERE lower(email) = lower($1);

-- name: CreateUser :one
-- email 由调用方先经 domain.NormalizeEmail／ValidateEmail；重复由唯一索引兜底（#202）。
INSERT INTO users (username, display_name, password_hash, is_system_admin, email)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: SetUserSystemAdmin :one
-- #200：设／撤系统管理员标记（CLI usermod；界面入口见 #205）。
UPDATE users SET is_system_admin = $2 WHERE id = $1
RETURNING *;

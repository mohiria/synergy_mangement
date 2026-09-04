-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: ListUsers :many
SELECT id, username, display_name FROM users ORDER BY id;

-- name: CreateUser :one
INSERT INTO users (username, display_name, password_hash, is_system_admin)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: SetUserSystemAdmin :one
-- #200：设／撤系统管理员标记（CLI usermod；界面入口见 #205）。
UPDATE users SET is_system_admin = $2 WHERE id = $1
RETURNING *;

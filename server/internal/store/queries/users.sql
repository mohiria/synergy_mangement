-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: ListUsers :many
-- 人员选择与建立成员关系用：停用用户默认不出现（#204）。
SELECT id, username, display_name, email FROM users WHERE disabled_at IS NULL ORDER BY id;

-- name: ListSystemUsers :many
-- #201：系统设置 → 用户管理列表（仅系统管理员）。
SELECT id, username, display_name, email, is_system_admin, must_change_password, disabled_at, created_at, last_login_at FROM users ORDER BY id;

-- name: SetUserDisabledAt :one
-- #204：停用（传时间）／启用（传 NULL）。
UPDATE users SET disabled_at = $2 WHERE id = $1
RETURNING *;

-- name: GetUserByEmail :one
-- #202：邮箱大小写不敏感（唯一索引建在 lower(email) 上）。
SELECT * FROM users WHERE lower(email) = lower($1);

-- name: CreateUser :one
-- email 由调用方先经 domain.NormalizeEmail／ValidateEmail；重复由唯一索引兜底（#202）。
INSERT INTO users (username, display_name, password_hash, is_system_admin, email, must_change_password)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: SetUserMustChangePassword :exec
-- #203：设／清「须改密码」标记。
UPDATE users SET must_change_password = $2 WHERE id = $1;

-- name: ResetUserPassword :one
-- #205：管理员重置密码——新哈希 + 置「须改密码」，会话由调用方吊销。
UPDATE users SET password_hash = $2, must_change_password = true WHERE id = $1
RETURNING *;

-- name: UpdateUserProfile :one
-- #205／#207：改显示名与邮箱（邮箱已归一，重复由唯一索引兜底）。
UPDATE users SET display_name = $2, email = $3 WHERE id = $1
RETURNING *;

-- name: SetUserSystemAdmin :one
-- #200：设／撤系统管理员标记（CLI usermod；界面入口见 #205）。
UPDATE users SET is_system_admin = $2 WHERE id = $1
RETURNING *;

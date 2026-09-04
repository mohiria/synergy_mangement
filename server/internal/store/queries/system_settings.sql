-- name: GetSystemSettings :one
-- #210：单行系统配置；行在迁移里预置。
SELECT * FROM system_settings WHERE id = 1;

-- name: UpdateSystemSettings :one
UPDATE system_settings
SET system_name = $1, subtitle = $2, login_hint = $3, base_url = $4, updated_at = now()
WHERE id = 1
RETURNING *;

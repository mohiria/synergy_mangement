-- name: GetSystemSettings :one
-- #210：单行系统配置；行在迁移里预置。
SELECT * FROM system_settings WHERE id = 1;

-- name: SetSystemLogo :one
-- #211：上传（传 key 与类型）或删除（传空串）logo，版本号自增。
UPDATE system_settings
SET logo_key = $1, logo_content_type = $2, logo_version = logo_version + 1, updated_at = now()
WHERE id = 1
RETURNING *;

-- name: UpdateSystemSettings :one
UPDATE system_settings
SET system_name = $1, subtitle = $2, login_hint = $3, base_url = $4, updated_at = now()
WHERE id = 1
RETURNING *;

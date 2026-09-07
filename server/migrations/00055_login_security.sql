-- +goose Up
-- #208：登录安全——用户最近登录时间；会话最近活动时间（随滑动续期一并更新）。
ALTER TABLE users ADD COLUMN last_login_at TIMESTAMPTZ;
ALTER TABLE sessions ADD COLUMN last_active_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE sessions DROP COLUMN last_active_at;
ALTER TABLE users DROP COLUMN last_login_at;

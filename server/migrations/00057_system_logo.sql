-- +goose Up
-- #211：系统 logo——对象存储 key、内容类型与版本号（出图 URL 带版本，换图后浏览器能拿到新图）。
ALTER TABLE system_settings
    ADD COLUMN logo_key          TEXT    NOT NULL DEFAULT '',
    ADD COLUMN logo_content_type TEXT    NOT NULL DEFAULT '',
    ADD COLUMN logo_version      INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE system_settings
    DROP COLUMN logo_key,
    DROP COLUMN logo_content_type,
    DROP COLUMN logo_version;

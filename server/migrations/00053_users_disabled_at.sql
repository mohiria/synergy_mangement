-- +goose Up
-- #204：停用账号（词汇表「停用」）：非空即停用，可启用恢复；不删除用户。
ALTER TABLE users ADD COLUMN disabled_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users DROP COLUMN disabled_at;

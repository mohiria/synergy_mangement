-- +goose Up
-- #200：系统管理员标记（ADR 0003）。隐式视同所有项目的管理员，不进审批链；
-- 首个管理员由 CLI（useradd -admin / usermod）产生，不做迁移自动提权。
ALTER TABLE users ADD COLUMN is_system_admin BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE users DROP COLUMN is_system_admin;

-- +goose Up
-- #203：「须改密码」标记（词汇表「首次改密」）：管理员建号／重置密码置真，本人改密或找回密码成功后清除。
ALTER TABLE users ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE users DROP COLUMN must_change_password;

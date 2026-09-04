-- +goose Up
-- #202：用户邮箱必填且全局唯一（大小写不敏感）。三步：加可空列 → 存量回填占位 → 收紧约束。
-- 占位域 .invalid 是保留顶级域（RFC 2606），永不投递；管理员或本人事后改正。
ALTER TABLE users ADD COLUMN email TEXT;
UPDATE users SET email = username || '@local.invalid' WHERE email IS NULL;
ALTER TABLE users ALTER COLUMN email SET NOT NULL;
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

-- +goose Down
DROP INDEX users_email_lower_idx;
ALTER TABLE users DROP COLUMN email;

-- +goose Up
-- #202：用户邮箱必填且全局唯一（大小写不敏感）。三步：加可空列 → 存量回填占位 → 收紧约束。
-- 占位域 .invalid 是保留顶级域（RFC 2606），永不投递；管理员或本人事后改正。
ALTER TABLE users ADD COLUMN email TEXT;
-- #215：用户名只区分大小写唯一，lower(email) 索引会把 'legacy' 与 'Legacy' 判成同一邮箱；
-- 与更早账号大小写不敏感冲突的用户名，占位邮箱再拼上 id 以保证唯一（最早的一个保持原样）。
UPDATE users u SET email = CASE
    WHEN EXISTS (SELECT 1 FROM users o WHERE o.id < u.id AND lower(o.username) = lower(u.username))
        THEN u.username || '-' || u.id || '@local.invalid'
    ELSE u.username || '@local.invalid'
END WHERE email IS NULL;
ALTER TABLE users ALTER COLUMN email SET NOT NULL;
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

-- +goose Down
DROP INDEX users_email_lower_idx;
ALTER TABLE users DROP COLUMN email;

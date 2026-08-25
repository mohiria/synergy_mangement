-- +goose Up
-- V4.4.3：取消“可编辑成员”角色，存量 editor 并入项目管理员。
UPDATE project_members SET role = 'admin' WHERE role = 'editor';

-- +goose Down
-- 角色合并不可逆（无法区分原生 admin 与并入的 editor），Down 不做变更。
SELECT 1;

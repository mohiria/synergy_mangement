-- +goose Up
-- #206：审计表支持无项目作用域——系统级写操作（用户管理、系统设置）的 project_id 为空。
ALTER TABLE audit_logs ALTER COLUMN project_id DROP NOT NULL;
CREATE INDEX idx_audit_logs_system ON audit_logs (id DESC) WHERE project_id IS NULL;

-- +goose Down
DROP INDEX idx_audit_logs_system;
DELETE FROM audit_logs WHERE project_id IS NULL;
ALTER TABLE audit_logs ALTER COLUMN project_id SET NOT NULL;

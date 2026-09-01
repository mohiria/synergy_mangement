-- +goose Up
-- 结构变更（输入、输入源、输出、接收方）复用关键字段变更单，差异与待执行动作存 payload
-- （PRD §5.2.B 关键字段清单；AC-23）。
ALTER TABLE field_change_requests ADD COLUMN payload JSONB;

-- +goose Down
ALTER TABLE field_change_requests DROP COLUMN payload;

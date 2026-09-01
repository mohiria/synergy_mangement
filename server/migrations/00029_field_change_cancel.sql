-- +goose Up
-- 任务取消复用关键字段变更单，作为一种变更类型记录（PRD §5.2.B；AC-57）。
ALTER TABLE field_change_requests
    ADD COLUMN change_type TEXT NOT NULL DEFAULT 'key_fields',
    ADD COLUMN old_status  TEXT,
    ADD COLUMN new_status  TEXT;

-- +goose Down
ALTER TABLE field_change_requests
    DROP COLUMN change_type,
    DROP COLUMN old_status,
    DROP COLUMN new_status;

-- +goose Up
-- 卡点动态的合成键（ADR 0001）：时间型卡点由每小时 ticker 补记，写触发的 diff 也会记同一条，
-- 两条路径对同一条卡点产出完全相同的一行，靠唯一键挡住重复记账；业务动作类动态该列为空。
ALTER TABLE task_activities ADD COLUMN blocker_key TEXT;

CREATE UNIQUE INDEX idx_task_activities_blocker
    ON task_activities (task_id, kind, blocker_key, occurred_at)
    WHERE blocker_key IS NOT NULL;

-- +goose Down
DROP INDEX idx_task_activities_blocker;
ALTER TABLE task_activities DROP COLUMN blocker_key;

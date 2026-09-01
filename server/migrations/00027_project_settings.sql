-- +goose Up
-- 项目规则设置（词汇表「项目规则设置」；主 PRD §7.9、我的工作 PRD §8.8；AC-60）。
-- 按项目生效、仅项目管理员可改，均有默认值；规则不写死在代码里。
ALTER TABLE projects
    ADD COLUMN approval_timeout_days INT NOT NULL DEFAULT 3,
    ADD COLUMN due_soon_days         INT NOT NULL DEFAULT 3,
    ADD COLUMN remind_daily_limit    INT NOT NULL DEFAULT 1;

-- 一键提醒冷却按（发起人、被提醒人、任务）三元组计（CONTEXT.md「提醒目标」）：
-- 原唯一约束只到（任务、发起人、日期），换一个被提醒人会被误拒。
-- 迁移前的历史行没有被提醒人信息，按发起人回填；冷却只看当天，历史行不影响判定。
ALTER TABLE remind_logs ADD COLUMN recipient_id BIGINT REFERENCES users (id);
UPDATE remind_logs SET recipient_id = sender_id WHERE recipient_id IS NULL;
ALTER TABLE remind_logs ALTER COLUMN recipient_id SET NOT NULL;

ALTER TABLE remind_logs DROP CONSTRAINT remind_logs_task_id_sender_id_remind_date_key;
CREATE INDEX idx_remind_logs_cooldown ON remind_logs (task_id, sender_id, recipient_id, remind_date);

-- +goose Down
DROP INDEX idx_remind_logs_cooldown;
ALTER TABLE remind_logs ADD CONSTRAINT remind_logs_task_id_sender_id_remind_date_key UNIQUE (task_id, sender_id, remind_date);
ALTER TABLE remind_logs DROP COLUMN recipient_id;
ALTER TABLE projects
    DROP COLUMN approval_timeout_days,
    DROP COLUMN due_soon_days,
    DROP COLUMN remind_daily_limit;

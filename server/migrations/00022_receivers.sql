-- +goose Up
-- 接收方与接收记录（词汇表「接收方」「接收记录」；主 PRD §4.1、模块 PRD §8.6；MW-09）。
-- 接收方是任务按需字段：不配置（none）、指定成员（members）或所有项目成员（all）。
ALTER TABLE tasks ADD COLUMN receiver_scope TEXT NOT NULL DEFAULT 'none';

CREATE TABLE task_receivers (
    task_id BIGINT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users (id),
    PRIMARY KEY (task_id, user_id)
);

-- 一行既是待接收项（confirmed_at IS NULL）也是接收记录（confirmed_at 非空）：
-- 终审通过时按当时接收方名单逐人生成，接收方确认后盖时间，事实只追加不删除。
CREATE TABLE task_receipts (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id      BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id      BIGINT      NOT NULL REFERENCES users (id),
    generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at TIMESTAMPTZ,
    UNIQUE (task_id, user_id)
);

CREATE INDEX idx_task_receipts_user ON task_receipts (user_id);

-- +goose Down
DROP TABLE task_receipts;
DROP TABLE task_receivers;
ALTER TABLE tasks DROP COLUMN receiver_scope;

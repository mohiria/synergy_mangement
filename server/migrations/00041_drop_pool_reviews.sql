-- +goose Up
-- 裁决 #162：取消入池审批——任务创建与导入直接入池。
-- 存量草稿与待入池审批任务并入正式任务池（未开始）；入池审批单与入池类动态一并清除。
UPDATE tasks SET status = 'not_started' WHERE status IN ('draft', 'pending_pool_review');
ALTER TABLE tasks ALTER COLUMN status SET DEFAULT 'not_started';
DELETE FROM task_activities WHERE kind IN ('pool_submitted', 'pool_approved', 'pool_rejected');
DROP TABLE pool_reviews;

-- +goose Down
ALTER TABLE tasks ALTER COLUMN status SET DEFAULT 'draft';
CREATE TABLE pool_reviews (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id      BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    submitted_by BIGINT      NOT NULL REFERENCES users (id),
    status       TEXT        NOT NULL DEFAULT 'pending',
    exempt       BOOLEAN     NOT NULL DEFAULT FALSE,
    opinion      TEXT        NOT NULL DEFAULT '',
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_by   BIGINT      REFERENCES users (id),
    decided_at   TIMESTAMPTZ
);
CREATE INDEX idx_pool_reviews_task ON pool_reviews (task_id);

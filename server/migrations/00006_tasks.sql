-- +goose Up
-- 任务与入池审批单（PRD §4.1、§5.1、§5.2.A；AC-04、AC-26）。
-- 生命周期状态与审批状态为文本，枚举在 domain 层校验。
CREATE TABLE tasks (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key_result_id BIGINT      NOT NULL REFERENCES key_results (id) ON DELETE CASCADE,
    name          TEXT        NOT NULL,
    owner_id      BIGINT      NOT NULL REFERENCES users (id),
    start_date    DATE        NOT NULL,
    end_date      DATE        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'draft',
    created_by    BIGINT      NOT NULL REFERENCES users (id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 同一任务可有多张审批单（退回后重新提交生成新单，旧单保留：词汇表「审核记录」）。
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

CREATE INDEX idx_tasks_key_result ON tasks (key_result_id);
CREATE INDEX idx_pool_reviews_task ON pool_reviews (task_id);

-- +goose Down
DROP TABLE pool_reviews;
DROP TABLE tasks;

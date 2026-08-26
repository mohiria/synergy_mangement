-- +goose Up
-- 任务动态（词汇表「任务动态」；ADR 0002）：只追加的业务事实留痕，不参与任何状态派生。
-- 文案在写入时定型并落库，读时不再拼装；系统派生事件（卡点出现／解除）没有行动人。
CREATE TABLE task_activities (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id     BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL,
    actor_id    BIGINT      REFERENCES users (id),
    summary     TEXT        NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_task_activities_task ON task_activities (task_id, id DESC);

-- +goose Down
DROP TABLE task_activities;

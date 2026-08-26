-- +goose Up
-- 一键提醒留痕与冷却（模块 PRD §5.3、MW-13）：同一人对同一任务每天只能提醒一次。
-- 提醒目标既可以是派生卡点，也可以是尚未成卡点的等待事项，故只记目标键不记外键。
CREATE TABLE remind_logs (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id     BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    sender_id   BIGINT      NOT NULL REFERENCES users (id),
    target_key  TEXT        NOT NULL,
    remind_date DATE        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (task_id, sender_id, remind_date)
);

-- +goose Down
DROP TABLE remind_logs;

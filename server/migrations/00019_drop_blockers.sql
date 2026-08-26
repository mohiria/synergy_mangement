-- +goose Up
-- V4.5：卡点改为由必要输入未就绪、任务超期、审批超时和硬依赖互锁四类结构化事实读时派生，
-- 不再人工上报，也不再落库（主 PRD §0.2、§8.4；我的工作 PRD §8.7）。
DROP TABLE blockers;

-- +goose Down
CREATE TABLE blockers (
    id                     BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id                BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    kind                   TEXT        NOT NULL,
    missing                TEXT        NOT NULL,
    reason                 TEXT        NOT NULL,
    action_owner_id        BIGINT      NOT NULL REFERENCES users (id),
    level                  TEXT        NOT NULL,
    expected_recovery_date DATE,
    state                  TEXT        NOT NULL DEFAULT 'open',
    created_by             BIGINT      NOT NULL REFERENCES users (id),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at            TIMESTAMPTZ,
    resolved_note          TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX idx_blockers_task ON blockers (task_id);

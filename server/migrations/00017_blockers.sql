-- +goose Up
-- 结构化卡点（词汇表「结构化卡点」；PRD §8.4；AC-11）。解除后保留处理事实。
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

-- +goose Down
DROP TABLE blockers;

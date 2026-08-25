-- +goose Up
-- 关键字段变更单（词汇表「关键字段变更单」；PRD §5.2.B；AC-23）。
-- 保留修改前后差异快照；rejected 且 resolved=false 即「退回待处理事项」。
CREATE TABLE field_change_requests (
    id                      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id                 BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    submitted_by            BIGINT      NOT NULL REFERENCES users (id),
    reason                  TEXT        NOT NULL DEFAULT '',
    state                   TEXT        NOT NULL DEFAULT 'pending',
    exempt                  BOOLEAN     NOT NULL DEFAULT FALSE,
    opinion                 TEXT        NOT NULL DEFAULT '',
    resolved                BOOLEAN     NOT NULL DEFAULT FALSE,
    old_name                TEXT,
    new_name                TEXT,
    old_description         TEXT,
    new_description         TEXT,
    old_completion_criteria TEXT,
    new_completion_criteria TEXT,
    old_owner_id            BIGINT      REFERENCES users (id),
    new_owner_id            BIGINT      REFERENCES users (id),
    old_end_date            DATE,
    new_end_date            DATE,
    submitted_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_by              BIGINT      REFERENCES users (id),
    decided_at              TIMESTAMPTZ
);

CREATE INDEX idx_field_change_requests_task ON field_change_requests (task_id);

-- +goose Down
DROP TABLE field_change_requests;

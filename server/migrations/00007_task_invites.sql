-- +goose Up
-- 任务创建邀请（词汇表「任务创建邀请」；AC-03）。状态文本，枚举在 domain 层校验。
CREATE TABLE task_invites (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key_result_id BIGINT      NOT NULL REFERENCES key_results (id) ON DELETE CASCADE,
    inviter_id    BIGINT      NOT NULL REFERENCES users (id),
    invitee_id    BIGINT      NOT NULL REFERENCES users (id),
    note          TEXT        NOT NULL DEFAULT '',
    state         TEXT        NOT NULL DEFAULT 'pending',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_task_invites_key_result ON task_invites (key_result_id);
CREATE INDEX idx_task_invites_invitee ON task_invites (invitee_id);

-- +goose Down
DROP TABLE task_invites;

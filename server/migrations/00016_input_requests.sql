-- +goose Up
-- 输入请求（词汇表「输入请求」；PRD §5.5、§8.2；AC-29/30）。
-- 附着在「成员 → 目标任务」的交付物边上；一期无拒绝、转派、多人协商。
CREATE TABLE input_requests (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    edge_id       BIGINT      NOT NULL UNIQUE REFERENCES deliverable_edges (id) ON DELETE CASCADE,
    provider_id   BIGINT      NOT NULL REFERENCES users (id),
    content_note  TEXT        NOT NULL,
    state         TEXT        NOT NULL DEFAULT 'pending',
    notified_at   TIMESTAMPTZ,
    accepted_at   TIMESTAMPTZ,
    provided_at   TIMESTAMPTZ,
    provided_text TEXT        NOT NULL DEFAULT '',
    file_name     TEXT        NOT NULL DEFAULT '',
    object_key    TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_input_requests_provider ON input_requests (provider_id);

-- +goose Down
DROP TABLE input_requests;

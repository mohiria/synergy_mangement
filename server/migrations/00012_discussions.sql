-- +goose Up
-- 任务讨论与站内通知（词汇表「任务讨论」「站内通知」；AC-35/36）。
-- 讨论不可编辑或删除：无 UPDATE/DELETE 路径，纠正追加新意见。
CREATE TABLE discussions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id    BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    author_id  BIGINT      NOT NULL REFERENCES users (id),
    content    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE discussion_mentions (
    discussion_id BIGINT NOT NULL REFERENCES discussions (id) ON DELETE CASCADE,
    user_id       BIGINT NOT NULL REFERENCES users (id),
    PRIMARY KEY (discussion_id, user_id)
);

CREATE TABLE notifications (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id),
    kind       TEXT        NOT NULL,
    content    TEXT        NOT NULL,
    project_id BIGINT      REFERENCES projects (id) ON DELETE CASCADE,
    task_id    BIGINT      REFERENCES tasks (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at    TIMESTAMPTZ
);

CREATE INDEX idx_discussions_task ON discussions (task_id);
CREATE INDEX idx_notifications_user ON notifications (user_id, id DESC);

-- +goose Down
DROP TABLE notifications;
DROP TABLE discussion_mentions;
DROP TABLE discussions;

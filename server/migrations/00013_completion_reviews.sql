-- +goose Up
-- 完成申请（词汇表「完成申请」；PRD §5.2.C、§8.3；AC-13/15/38-40）。
-- 申请项保留交付物与文件名快照：候选文件被覆盖或退回删除后审核记录仍在。
CREATE TABLE completion_reviews (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id      BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    submitted_by BIGINT      NOT NULL REFERENCES users (id),
    note         TEXT        NOT NULL,
    state        TEXT        NOT NULL DEFAULT 'pending_final',
    opinion      TEXT        NOT NULL DEFAULT '',
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_by   BIGINT      REFERENCES users (id),
    decided_at   TIMESTAMPTZ
);

CREATE TABLE completion_review_items (
    review_id        BIGINT NOT NULL REFERENCES completion_reviews (id) ON DELETE CASCADE,
    deliverable_id   BIGINT NOT NULL REFERENCES deliverables (id) ON DELETE CASCADE,
    deliverable_name TEXT   NOT NULL,
    file_name        TEXT   NOT NULL,
    file_id          BIGINT REFERENCES deliverable_files (id) ON DELETE SET NULL,
    PRIMARY KEY (review_id, deliverable_id)
);

CREATE INDEX idx_completion_reviews_task ON completion_reviews (task_id);

-- +goose Down
DROP TABLE completion_review_items;
DROP TABLE completion_reviews;

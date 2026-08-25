-- +goose Up
-- 交付物项与交付内容（词汇表「交付物」「当前交付物」「候选交付物」；PRD §5.3；AC-32/33）。
-- 每项至多一份 current 与一份 candidate（应用层保证）；不保留历史版本。
CREATE TABLE deliverables (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id    BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    created_by BIGINT      NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE deliverable_files (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    deliverable_id BIGINT      NOT NULL REFERENCES deliverables (id) ON DELETE CASCADE,
    state          TEXT        NOT NULL DEFAULT 'candidate',
    file_name      TEXT        NOT NULL,
    file_type      TEXT        NOT NULL DEFAULT '',
    file_size      BIGINT      NOT NULL DEFAULT 0,
    object_key     TEXT        NOT NULL,
    uploaded_by    BIGINT      NOT NULL REFERENCES users (id),
    uploaded_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    effective_at   TIMESTAMPTZ
);

CREATE INDEX idx_deliverables_task ON deliverables (task_id);
CREATE INDEX idx_deliverable_files_deliverable ON deliverable_files (deliverable_id);

-- +goose Down
DROP TABLE deliverable_files;
DROP TABLE deliverables;

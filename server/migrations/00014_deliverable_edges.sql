-- +goose Up
-- 交付物边（词汇表「交付物边」；PRD §4.3/4.4；AC-07/28/48）。
-- 来源二选一：来源任务（source_task_id）或指定项目成员（source_user_id，#14 输入请求）。
-- 就绪状态派生自对应交付物项是否已有当前内容，不落库。
CREATE TABLE deliverable_edges (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    target_task_id BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    source_task_id BIGINT      REFERENCES tasks (id) ON DELETE CASCADE,
    source_user_id BIGINT      REFERENCES users (id),
    deliverable_id BIGINT      REFERENCES deliverables (id) ON DELETE SET NULL,
    name           TEXT        NOT NULL,
    edge_type      TEXT        NOT NULL,
    necessity      TEXT        NOT NULL DEFAULT 'required',
    expected_date  DATE,
    created_by     BIGINT      NOT NULL REFERENCES users (id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_edges_target ON deliverable_edges (target_task_id);
CREATE INDEX idx_edges_source_task ON deliverable_edges (source_task_id);

-- +goose Down
DROP TABLE deliverable_edges;

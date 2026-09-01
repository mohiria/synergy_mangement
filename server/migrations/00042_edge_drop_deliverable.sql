-- +goose Up
-- 裁决 #163：交付物边不再挂具体交付物项——就绪判定改为「来源任务已完成」，
-- 存量边的交付物项关联作废。
ALTER TABLE deliverable_edges DROP COLUMN deliverable_id;

-- +goose Down
ALTER TABLE deliverable_edges ADD COLUMN deliverable_id BIGINT REFERENCES deliverables (id) ON DELETE SET NULL;

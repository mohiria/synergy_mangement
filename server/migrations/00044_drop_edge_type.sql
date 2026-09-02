-- +goose Up
-- 裁决 #173：交付物边的「关系类型」删除，只留必要性；
-- 存量边直接沿用其必要性取值，互锁/关键路径/影响路径改沿「必要」边推导。
ALTER TABLE deliverable_edges DROP COLUMN edge_type;

-- +goose Down
ALTER TABLE deliverable_edges ADD COLUMN edge_type TEXT NOT NULL DEFAULT 'information';

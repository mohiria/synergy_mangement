-- +goose Up
-- 任务最后更新时间（词汇表「任务基础信息」：任务详情页头展示更新时间，AC-50）。
-- 已有任务回填为创建时间，之后由任务写入路径显式维护。
ALTER TABLE tasks ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
UPDATE tasks SET updated_at = created_at;

-- +goose Down
ALTER TABLE tasks DROP COLUMN updated_at;

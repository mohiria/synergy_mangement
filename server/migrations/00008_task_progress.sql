-- +goose Up
-- 任务可选进度与取消原因（PRD §5.1、§5.6；AC-12）。progress 为空即未填写。
ALTER TABLE tasks ADD COLUMN progress INT;
ALTER TABLE tasks ADD COLUMN cancel_reason TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tasks DROP COLUMN cancel_reason;
ALTER TABLE tasks DROP COLUMN progress;

-- +goose Up
-- 任务说明与完成标准（词汇表「任务基础信息」；选填，编辑走关键字段修改审批 #12）。
ALTER TABLE tasks ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN completion_criteria TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tasks DROP COLUMN completion_criteria;
ALTER TABLE tasks DROP COLUMN description;

-- +goose Up
-- 成果更新（词汇表「成果更新」；主 PRD §5.1、§5.3、AC-66）：已完成任务对交付物再次发起的更新。
-- 进程落在任务上：''＝无、'open'＝已发起未提交、'reviewing'＝已随完成申请提交在审。
-- 任务生命周期状态在整个过程中保持 completed，不回退——这是「已生效 · 有更新审核中」的唯一入口。
ALTER TABLE tasks ADD COLUMN result_update TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tasks DROP COLUMN result_update;

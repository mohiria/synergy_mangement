-- +goose Up
-- 每个交付物项至多一份 current 与一份 candidate（§5.3「唯一正式内容」）。
-- 此前只由应用层保证：删旧建新非事务、GetCurrentFile 吞错跳过删除，都会留下两行同态记录，
-- 进而使 GetCurrentFile ... LIMIT 1 随机取值，或让完成申请因主键冲突永久 500。
CREATE UNIQUE INDEX idx_deliverable_files_one_current
    ON deliverable_files (deliverable_id) WHERE state = 'current';
CREATE UNIQUE INDEX idx_deliverable_files_one_candidate
    ON deliverable_files (deliverable_id) WHERE state = 'candidate';

-- +goose Down
DROP INDEX idx_deliverable_files_one_candidate;
DROP INDEX idx_deliverable_files_one_current;

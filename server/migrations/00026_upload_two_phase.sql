-- +goose Up
-- 上传两阶段提交（R4）：建记录时先落 uploading，客户端确认写入对象存储后才转 candidate／provided。
-- 未确认的记录不参与就绪判定，因此同样限制每个交付物项至多一条。
CREATE UNIQUE INDEX idx_deliverable_files_one_uploading
    ON deliverable_files (deliverable_id) WHERE state = 'uploading';

-- +goose Down
DROP INDEX idx_deliverable_files_one_uploading;

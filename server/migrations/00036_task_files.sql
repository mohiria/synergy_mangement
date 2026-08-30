-- +goose Up
-- 过程文件与重要外部材料（词汇表两条同名词；主 PRD §7.7 文件对象边界表、§8.5；AC-17／AC-18）。
-- 与交付物内容同走两阶段提交（state uploading → ready），落在任务下，不游离于任务之外。
-- 边界与交付物完全不同：不进完成审批、不作下游正式输入，只可按需选进成果包——规则在 domain。
CREATE TABLE task_files (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id     BIGINT      NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL,
    state       TEXT        NOT NULL DEFAULT 'uploading',
    file_name   TEXT        NOT NULL,
    file_type   TEXT        NOT NULL DEFAULT '',
    file_size   BIGINT      NOT NULL DEFAULT 0,
    object_key  TEXT        NOT NULL,
    note        TEXT        NOT NULL DEFAULT '',
    uploaded_by BIGINT      NOT NULL REFERENCES users (id),
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_task_files_task ON task_files (task_id);

-- 成果包目录改为二选一引用：交付物项（解析当前内容）或任务文件（§7.7「可以按需选择」）。
-- 原主键 (package_id, deliverable_id) 让位给代理主键，两种引用各自去重。
ALTER TABLE artifact_package_items DROP CONSTRAINT artifact_package_items_pkey;
ALTER TABLE artifact_package_items ALTER COLUMN deliverable_id DROP NOT NULL;
ALTER TABLE artifact_package_items ADD COLUMN id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY;
ALTER TABLE artifact_package_items ADD COLUMN task_file_id BIGINT REFERENCES task_files (id) ON DELETE CASCADE;
ALTER TABLE artifact_package_items ADD CONSTRAINT chk_package_item_one_ref
    CHECK ((deliverable_id IS NULL) <> (task_file_id IS NULL));
CREATE UNIQUE INDEX idx_package_items_deliverable
    ON artifact_package_items (package_id, deliverable_id) WHERE deliverable_id IS NOT NULL;
CREATE UNIQUE INDEX idx_package_items_task_file
    ON artifact_package_items (package_id, task_file_id) WHERE task_file_id IS NOT NULL;

-- +goose Down
DROP INDEX idx_package_items_task_file;
DROP INDEX idx_package_items_deliverable;
ALTER TABLE artifact_package_items DROP CONSTRAINT chk_package_item_one_ref;
ALTER TABLE artifact_package_items DROP COLUMN task_file_id;
ALTER TABLE artifact_package_items DROP COLUMN id;
DELETE FROM artifact_package_items WHERE deliverable_id IS NULL;
ALTER TABLE artifact_package_items ALTER COLUMN deliverable_id SET NOT NULL;
ALTER TABLE artifact_package_items ADD PRIMARY KEY (package_id, deliverable_id);
DROP TABLE task_files;

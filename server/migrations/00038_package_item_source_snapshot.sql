-- +goose Up
-- F-10（#88）：成果包引用的过程文件／重要外部材料被删除后，目录与来源清单保留条目并标注「来源文件已删除」。
-- 原来 task_file_id 是 ON DELETE CASCADE，来源一删条目行也跟着没了——包从 3 项变 2 项、清单少一行，
-- 与 §7.7「成果包保留逻辑清单和来源事实」冲突（AC-18）。
-- 改 SET NULL，并把来源事实（所属任务名、文件名、文件类型）快照到条目上，删除后仍能还原「谁的什么文件」。
ALTER TABLE artifact_package_items ADD COLUMN source_task_name TEXT NOT NULL DEFAULT '';
ALTER TABLE artifact_package_items ADD COLUMN source_file_name TEXT NOT NULL DEFAULT '';
ALTER TABLE artifact_package_items ADD COLUMN source_file_kind TEXT NOT NULL DEFAULT '';

UPDATE artifact_package_items i
SET source_task_name = t.name,
    source_file_name = tf.file_name,
    source_file_kind = tf.kind
FROM task_files tf
JOIN tasks t ON t.id = tf.task_id
WHERE i.task_file_id = tf.id;

ALTER TABLE artifact_package_items DROP CONSTRAINT artifact_package_items_task_file_id_fkey;
ALTER TABLE artifact_package_items ADD CONSTRAINT artifact_package_items_task_file_id_fkey
    FOREIGN KEY (task_file_id) REFERENCES task_files (id) ON DELETE SET NULL;

-- 二选一引用改按快照判定：交付物项没有来源快照，任务文件项即使来源被删（task_file_id 置空）也留着快照。
ALTER TABLE artifact_package_items DROP CONSTRAINT chk_package_item_one_ref;
ALTER TABLE artifact_package_items ADD CONSTRAINT chk_package_item_one_ref
    CHECK ((deliverable_id IS NULL) <> (source_file_name = ''));

-- +goose Down
ALTER TABLE artifact_package_items DROP CONSTRAINT chk_package_item_one_ref;
DELETE FROM artifact_package_items WHERE deliverable_id IS NULL AND task_file_id IS NULL;
ALTER TABLE artifact_package_items ADD CONSTRAINT chk_package_item_one_ref
    CHECK ((deliverable_id IS NULL) <> (task_file_id IS NULL));
ALTER TABLE artifact_package_items DROP CONSTRAINT artifact_package_items_task_file_id_fkey;
ALTER TABLE artifact_package_items ADD CONSTRAINT artifact_package_items_task_file_id_fkey
    FOREIGN KEY (task_file_id) REFERENCES task_files (id) ON DELETE CASCADE;
ALTER TABLE artifact_package_items DROP COLUMN source_file_kind;
ALTER TABLE artifact_package_items DROP COLUMN source_file_name;
ALTER TABLE artifact_package_items DROP COLUMN source_task_name;

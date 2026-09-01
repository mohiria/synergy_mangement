-- +goose Up
-- 裁决 G1（#140）：删除阶段成果包——目录、条目与 #88 的来源快照一并移除；
-- 成果归档只保留当前成果、候选与过程文件的统一视角，不再有成果包实体。
DROP TABLE IF EXISTS artifact_package_items;
DROP TABLE IF EXISTS artifact_packages;

-- +goose Down
-- 按 00018 + 00038 的最终形态重建空表（数据不可恢复，仅保证结构可回退）。
CREATE TABLE artifact_packages (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id BIGINT      NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    created_by BIGINT      NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE artifact_package_items (
    package_id       BIGINT NOT NULL REFERENCES artifact_packages (id) ON DELETE CASCADE,
    deliverable_id   BIGINT REFERENCES deliverables (id) ON DELETE CASCADE,
    task_file_id     BIGINT REFERENCES task_files (id) ON DELETE SET NULL,
    source_task_name TEXT NOT NULL DEFAULT '',
    source_file_name TEXT NOT NULL DEFAULT '',
    source_file_kind TEXT NOT NULL DEFAULT '',
    CONSTRAINT chk_package_item_one_ref CHECK ((deliverable_id IS NULL) <> (source_file_name = ''))
);

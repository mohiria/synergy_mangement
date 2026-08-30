-- +goose Up
-- 导入记录（词汇表「导入记录」；PRD §7.9、§11.1；AC-68）：每次表格导入留存的业务事实。
-- 与通用操作审计并列而非重复：审计只记 actor／action／route／对象类型与 ID，
-- 这里记源文件名、本次真实新建的 O／KR／任务数量与结果；失败的那一次同样留记录。
CREATE TABLE import_records (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id      BIGINT      NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    operator_id     BIGINT      NOT NULL REFERENCES users (id),
    source_file_name TEXT       NOT NULL DEFAULT '',
    objective_count INT         NOT NULL DEFAULT 0,
    key_result_count INT        NOT NULL DEFAULT 0,
    task_count      INT         NOT NULL DEFAULT 0,
    result          TEXT        NOT NULL,
    failure_summary TEXT        NOT NULL DEFAULT '',
    imported_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_import_records_project ON import_records (project_id, id DESC);

-- +goose Down
DROP TABLE import_records;

-- +goose Up
-- 项目字段扩展（PRD §0.4 V4.4.2）：项目负责人、状态、阶段、计划周期。
ALTER TABLE projects
    ADD COLUMN owner_id           BIGINT REFERENCES users (id),
    ADD COLUMN status             TEXT NOT NULL DEFAULT 'not_started',
    ADD COLUMN stage              TEXT,
    ADD COLUMN planned_start_date DATE,
    ADD COLUMN planned_end_date   DATE;

-- 存量项目回填：创建人暂任项目负责人。
UPDATE projects SET owner_id = created_by WHERE owner_id IS NULL;

ALTER TABLE projects
    ALTER COLUMN owner_id SET NOT NULL;

-- +goose Down
ALTER TABLE projects
    DROP COLUMN planned_end_date,
    DROP COLUMN planned_start_date,
    DROP COLUMN stage,
    DROP COLUMN status,
    DROP COLUMN owner_id;

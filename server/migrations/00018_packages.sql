-- +goose Up
-- 轻量成果包（词汇表「成果包」；PRD §7.7、§8.5；AC-18）。
-- 目录只引用交付物项，不复制文件；下载时解析当前内容。
CREATE TABLE artifact_packages (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id BIGINT      NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    created_by BIGINT      NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE artifact_package_items (
    package_id     BIGINT NOT NULL REFERENCES artifact_packages (id) ON DELETE CASCADE,
    deliverable_id BIGINT NOT NULL REFERENCES deliverables (id) ON DELETE CASCADE,
    PRIMARY KEY (package_id, deliverable_id)
);

-- +goose Down
DROP TABLE artifact_package_items;
DROP TABLE artifact_packages;

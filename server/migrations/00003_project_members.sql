-- +goose Up
-- 项目成员与成员角色（PRD §3.2 系统权限、§3.4 权限矩阵；AC-21 骨架）。
-- 角色四值枚举 admin/editor/member/viewer 在 domain 层校验，库里存文本。
CREATE TABLE project_members (
    project_id BIGINT      NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    BIGINT      NOT NULL REFERENCES users (id),
    role       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

-- 存量项目回填：创建人作为项目管理员成员。
-- 项目负责人不自动成为成员／管理员（PRD §0.4 V4.4.2），不回填 owner_id。
INSERT INTO project_members (project_id, user_id, role)
SELECT id, created_by, 'admin'
FROM projects
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE project_members;

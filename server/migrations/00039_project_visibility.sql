-- +goose Up
-- 项目可见性（词汇表「项目可见性」；裁决 D1、#111）：默认私有，已有项目回填 private，
-- 行为与现状完全一致；public 只放开读，写路径不受影响。
ALTER TABLE projects
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private'
        CHECK (visibility IN ('private', 'public'));

-- +goose Down
ALTER TABLE projects DROP COLUMN visibility;

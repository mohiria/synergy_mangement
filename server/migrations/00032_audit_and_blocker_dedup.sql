-- +goose Up
-- R8：项目域审计（成员、角色、项目字段、成果包等不属于任何任务的变化）另立一张表。
-- 由写路径装饰器统一落，新增写路径无需手工挂载。
CREATE TABLE audit_logs (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id  BIGINT      NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    actor_id    BIGINT      REFERENCES users (id),
    action      TEXT        NOT NULL,
    method      TEXT        NOT NULL,
    route       TEXT        NOT NULL,
    object_type TEXT        NOT NULL DEFAULT '',
    object_id   BIGINT,
    summary     TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_project ON audit_logs (project_id, id DESC);

-- R9：卡点「出现」的去重键改为「自上次解除以来只记一条」。
-- 旧唯一键含 occurred_at，而上游未就绪取任务开始日、任务超期取截止日，都是常量——
-- 同一卡点解除后二次出现会被 ON CONFLICT DO NOTHING 静默丢弃。
DROP INDEX IF EXISTS idx_task_activities_blocker;

CREATE INDEX idx_task_activities_blocker_key
    ON task_activities (task_id, blocker_key, id DESC)
    WHERE blocker_key IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_task_activities_blocker_key;
CREATE UNIQUE INDEX idx_task_activities_blocker
    ON task_activities (task_id, kind, blocker_key, occurred_at)
    WHERE blocker_key IS NOT NULL;
DROP TABLE audit_logs;

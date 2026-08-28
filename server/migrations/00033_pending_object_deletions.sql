-- +goose Up
-- E3：对象存储删除失败的补偿队列（§5.3「旧文件永久删除、不保留副本」）。
-- 库里的行删了但对象还在桶里，从合规角度是真问题而不只是清理问题——
-- 失败的删除排进这张表，由每小时 ticker 重试到成功为止。
CREATE TABLE pending_object_deletions (
    object_key  TEXT PRIMARY KEY,
    attempts    INT         NOT NULL DEFAULT 1,
    last_error  TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_try_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE pending_object_deletions;

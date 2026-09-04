-- +goose Up
-- #210：系统设置单行配置（模块 PRD §7.1）：系统名称、副标题、登录页提示语、访问地址。
-- 上限按 Unicode 字符计，由 domain 校验；库里只做兜底长度约束。
CREATE TABLE system_settings (
    id          SMALLINT    PRIMARY KEY CHECK (id = 1),
    system_name TEXT        NOT NULL DEFAULT '协同管理工具',
    subtitle    TEXT        NOT NULL DEFAULT 'O／KR／任务协同推进',
    login_hint  TEXT        NOT NULL DEFAULT '账号由管理员分配',
    base_url    TEXT        NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO system_settings (id) VALUES (1);

-- +goose Down
DROP TABLE system_settings;

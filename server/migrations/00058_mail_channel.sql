-- +goose Up
-- #212：邮件通道（单行配置，密码 AES-GCM 密文落库、密钥来自环境变量 APP_SECRET_KEY）与 outbox。
CREATE TABLE mail_settings (
    id            SMALLINT    PRIMARY KEY CHECK (id = 1),
    host          TEXT        NOT NULL DEFAULT '',
    port          INTEGER     NOT NULL DEFAULT 587,
    encryption    TEXT        NOT NULL DEFAULT 'starttls' CHECK (encryption IN ('none', 'starttls', 'ssl')),
    username      TEXT        NOT NULL DEFAULT '',
    password_enc  TEXT        NOT NULL DEFAULT '',
    from_name     TEXT        NOT NULL DEFAULT '',
    from_address  TEXT        NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO mail_settings (id) VALUES (1);

-- outbox：所有邮件先入队，由进程内后台协程取出发送；失败按退避重试若干次后标记失败。
CREATE TABLE mail_outbox (
    id              BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    to_address      TEXT        NOT NULL,
    subject         TEXT        NOT NULL,
    body            TEXT        NOT NULL,
    event           TEXT        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts        INTEGER     NOT NULL DEFAULT 0,
    last_error      TEXT        NOT NULL DEFAULT '',
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ
);
CREATE INDEX idx_mail_outbox_due ON mail_outbox (next_attempt_at) WHERE status = 'pending';
CREATE INDEX idx_mail_outbox_recent ON mail_outbox (id DESC);

-- +goose Down
DROP TABLE mail_outbox;
DROP TABLE mail_settings;

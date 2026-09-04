-- +goose Up
-- #213：站内通知同步邮件——系统级总开关 + 五个事件开关（默认全开），个人偏好同构（无行即全开）。
ALTER TABLE mail_settings
    ADD COLUMN notify_enabled                BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN notify_discussion_mention     BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN notify_discussion_owner       BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN notify_task_invite            BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN notify_upstream_task_assigned BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN notify_blocker_remind         BOOLEAN NOT NULL DEFAULT true;

CREATE TABLE user_mail_prefs (
    user_id                       BIGINT  PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    enabled                       BOOLEAN NOT NULL DEFAULT true,
    notify_discussion_mention     BOOLEAN NOT NULL DEFAULT true,
    notify_discussion_owner       BOOLEAN NOT NULL DEFAULT true,
    notify_task_invite            BOOLEAN NOT NULL DEFAULT true,
    notify_upstream_task_assigned BOOLEAN NOT NULL DEFAULT true,
    notify_blocker_remind         BOOLEAN NOT NULL DEFAULT true,
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE user_mail_prefs;
ALTER TABLE mail_settings
    DROP COLUMN notify_enabled,
    DROP COLUMN notify_discussion_mention,
    DROP COLUMN notify_discussion_owner,
    DROP COLUMN notify_task_invite,
    DROP COLUMN notify_upstream_task_assigned,
    DROP COLUMN notify_blocker_remind;

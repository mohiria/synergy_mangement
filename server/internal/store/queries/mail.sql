-- name: GetMailSettings :one
-- #212：单行邮件通道配置；password_enc 是密文，handler 永不回显。
SELECT * FROM mail_settings WHERE id = 1;

-- name: UpdateMailSettings :one
-- 不含密码：密码留空表示保持原值，由 SetMailPassword 单独写。
UPDATE mail_settings
SET host = $1, port = $2, encryption = $3, username = $4, from_name = $5, from_address = $6, updated_at = now()
WHERE id = 1
RETURNING *;

-- name: SetMailPassword :exec
UPDATE mail_settings SET password_enc = $1, updated_at = now() WHERE id = 1;

-- name: EnqueueMail :one
INSERT INTO mail_outbox (to_address, subject, body, event)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListMailOutbox :many
-- 最近发送记录，最新在前。
SELECT id, to_address, subject, body, event, status, attempts, last_error, created_at, sent_at
FROM mail_outbox ORDER BY id DESC LIMIT $1;

-- name: ClaimDueMail :many
-- 后台协程取到期待发件；SKIP LOCKED 让多实例／重入互不干扰。
SELECT * FROM mail_outbox
WHERE status = 'pending' AND next_attempt_at <= now()
ORDER BY id
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: MarkMailSent :exec
UPDATE mail_outbox SET status = 'sent', attempts = attempts + 1, last_error = '', sent_at = now() WHERE id = $1;

-- name: MarkMailRetry :exec
UPDATE mail_outbox SET attempts = attempts + 1, last_error = $2, next_attempt_at = $3 WHERE id = $1;

-- name: MarkMailFailed :exec
UPDATE mail_outbox SET status = 'failed', attempts = attempts + 1, last_error = $2 WHERE id = $1;

-- name: CountMailOutboxByEvent :one
SELECT count(*) FROM mail_outbox WHERE event = $1;

-- name: UpdateMailNotifySwitches :one
-- #213：系统级总开关与五个事件开关。
UPDATE mail_settings
SET notify_enabled = $1, notify_discussion_mention = $2, notify_discussion_owner = $3,
    notify_task_invite = $4, notify_upstream_task_assigned = $5, notify_blocker_remind = $6, updated_at = now()
WHERE id = 1
RETURNING *;

-- name: GetUserMailPrefs :one
SELECT * FROM user_mail_prefs WHERE user_id = $1;

-- name: UpsertUserMailPrefs :one
INSERT INTO user_mail_prefs (user_id, enabled, notify_discussion_mention, notify_discussion_owner,
    notify_task_invite, notify_upstream_task_assigned, notify_blocker_remind)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    notify_discussion_mention = EXCLUDED.notify_discussion_mention,
    notify_discussion_owner = EXCLUDED.notify_discussion_owner,
    notify_task_invite = EXCLUDED.notify_task_invite,
    notify_upstream_task_assigned = EXCLUDED.notify_upstream_task_assigned,
    notify_blocker_remind = EXCLUDED.notify_blocker_remind,
    updated_at = now()
RETURNING *;


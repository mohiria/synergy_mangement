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
SELECT id, to_address, subject, event, status, attempts, last_error, created_at, sent_at
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

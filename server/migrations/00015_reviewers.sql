-- +goose Up
-- 中间审核组（词汇表「中间审核组」；PRD §5.4；AC-14/24/37）。
-- 任务级配置可直接调整；提交完成申请时快照进申请，或签由申请上的处理事实表达。
CREATE TABLE task_reviewers (
    task_id BIGINT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users (id),
    PRIMARY KEY (task_id, user_id)
);

CREATE TABLE completion_review_reviewers (
    review_id BIGINT NOT NULL REFERENCES completion_reviews (id) ON DELETE CASCADE,
    user_id   BIGINT NOT NULL REFERENCES users (id),
    PRIMARY KEY (review_id, user_id)
);

-- 中间或签通过的处理事实单独留痕（§5.4 通过与退回均记录处理人、时间和意见）；
-- decided_* 字段保留给终审／整体退回。
ALTER TABLE completion_reviews ADD COLUMN intermediate_by BIGINT REFERENCES users (id);
ALTER TABLE completion_reviews ADD COLUMN intermediate_at TIMESTAMPTZ;
ALTER TABLE completion_reviews ADD COLUMN intermediate_opinion TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE completion_reviews DROP COLUMN intermediate_opinion;
ALTER TABLE completion_reviews DROP COLUMN intermediate_at;
ALTER TABLE completion_reviews DROP COLUMN intermediate_by;
DROP TABLE completion_review_reviewers;
DROP TABLE task_reviewers;

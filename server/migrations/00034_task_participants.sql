-- +goose Up
-- 参与人（词汇表「参与人」；主 PRD §4.1、§9.2）：任务上除负责人以外的协作者名单。
-- 只作展示与检索——不产生待办、不进审批链、不影响权限、不参与我的工作归组与排序，
-- 因此这里只需要一张纯关联表，不带状态、时间戳等任何会被派生逻辑读到的字段。
CREATE TABLE task_participants (
    task_id BIGINT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users (id),
    PRIMARY KEY (task_id, user_id)
);

-- +goose Down
DROP TABLE task_participants;

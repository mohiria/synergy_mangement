-- +goose Up
-- 裁决 #178（裁决 8）：「指定项目成员提供」改为替他人创建上游任务，
-- 输入请求机制整体退场——关系回归任务与任务之间。
-- 存量口径：成员来源边与其输入请求一并删除（演示数据随 seed 重建）；
-- 边表随之收紧：来源恒为任务（source_task_id 非空），期望时间列删除（#174 后已只剩成员来源在用）。
DROP TABLE input_requests;
DELETE FROM deliverable_edges WHERE source_task_id IS NULL;
ALTER TABLE deliverable_edges
    DROP COLUMN source_user_id,
    DROP COLUMN expected_date,
    ALTER COLUMN source_task_id SET NOT NULL;

-- +goose Down
-- 输入请求机制已退场，不提供回滚重建。
SELECT 1;

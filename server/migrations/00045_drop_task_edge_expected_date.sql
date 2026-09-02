-- +goose Up
-- 裁决 #174：任务来源的交付物边不再单独维护「期望时间」（与上游任务截止时间零联动、
-- 可互相矛盾），展示与超期判断统一取上游任务截止日期；存量值作废。
-- 成员来源边的期望时间保留（随「指定成员改为创建上游任务」裁决整体退场）。
UPDATE deliverable_edges SET expected_date = NULL WHERE source_task_id IS NOT NULL;

-- +goose Down
-- 存量值已作废，无法恢复。
SELECT 1;

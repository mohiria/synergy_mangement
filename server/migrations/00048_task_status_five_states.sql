-- +goose Up
-- 裁决 13（#182）：任务生命周期存储状态缩减为五态——未开始／进行中／审核中／已完成／已关闭。
-- 「待成果审核（或签）」「待终审」合并为审核中（当前环节从完成申请单读取）；
-- 「等待输入」移出存储枚举成为纯显示派生态（存量如有映射回未开始）。
UPDATE tasks SET status = 'in_review'
WHERE status IN ('pending_intermediate_review', 'pending_final_review');
UPDATE tasks SET status = 'not_started' WHERE status = 'waiting_input';

-- +goose Down
-- 合并后无法区分原来停在哪个环节，不提供回滚拆分。
SELECT 1;

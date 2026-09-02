-- +goose Up
-- 裁决 10（#180）：关闭申请审批机制整体退场——关闭改为项目管理员直接操作
-- （原因必填、即时生效、写任务动态）。#172 后本表只剩 change_type='cancel' 一类，
-- 存量关闭申请数据随表一并清除（演示数据随 seed 重建）。
DROP TABLE field_change_requests;

-- +goose Down
-- 关闭申请机制已退场，不提供回滚重建。
SELECT 1;

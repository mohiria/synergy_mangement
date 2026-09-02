-- +goose Up
-- 裁决 #172（第二刀）：取消关键字段变更审批——关键字段（含结构字段）直接修改生效，
-- 任务关闭审批独立保留为「关闭申请」。表 field_change_requests 只再承载关闭申请
-- （change_type='cancel'）；存量关键字段／结构变更单（含待审批的，拟议值作废）一并清除。
DELETE FROM field_change_requests WHERE change_type <> 'cancel';

COMMENT ON TABLE field_change_requests IS '关闭申请（裁决 #172 后仅存 change_type=cancel；历史表名保留）';

-- +goose Down
COMMENT ON TABLE field_change_requests IS NULL;

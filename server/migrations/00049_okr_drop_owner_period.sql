-- +goose Up
-- 裁决 12（#183）：O／KR 不再有负责人与周期属性——风险、进度、卡点数本就只由任务派生
-- （KR＝下级任务取最大，O＝下级 KR 取最大），删除 KR 的 owner_id／start_date／end_date。
-- O／KR 补创建人 created_by（可空；存量数据不伪造回填，前端显示「—」）。
ALTER TABLE key_results
    DROP COLUMN owner_id,
    DROP COLUMN start_date,
    DROP COLUMN end_date,
    ADD COLUMN created_by BIGINT REFERENCES users (id);
ALTER TABLE objectives
    ADD COLUMN created_by BIGINT REFERENCES users (id);

-- +goose Down
-- 负责人与周期数据已删除，不提供回滚重建。
SELECT 1;

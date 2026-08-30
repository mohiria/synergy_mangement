-- name: CreateImportRecord :one
-- 成功的一次随导入同事务写入；失败的一次在事务回滚后单独写入（AC-68）。
INSERT INTO import_records (
    project_id, operator_id, source_file_name,
    objective_count, key_result_count, task_count, result, failure_summary
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListImportRecordsByProject :many
-- 项目设置「导入记录」分节，只读，最新在前。
SELECT ir.*, u.display_name AS operator_name
FROM import_records ir
JOIN users u ON u.id = ir.operator_id
WHERE ir.project_id = $1
ORDER BY ir.id DESC;

-- name: CreateAuditLog :exec
-- 项目域审计留痕（§10.4、R8）：由写路径装饰器统一落，新增写路径无需手工挂载。
INSERT INTO audit_logs (project_id, actor_id, action, method, route, object_type, object_id, summary)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListAuditLogsByProject :many
SELECT a.*, u.display_name AS actor_name
FROM audit_logs a
LEFT JOIN users u ON u.id = a.actor_id
WHERE a.project_id = $1
ORDER BY a.id DESC
LIMIT $2;

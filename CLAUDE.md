# 协同管理工具

单团队自用（约 100 人、内网单机部署）的项目协作系统：O／KR／任务三级模型、交付物边、三道审批、我的工作、关系图谱。

## 文档指针（唯一事实源，不在本文件重复）

- 需求：`docs/协同管理工具_详细PRD_V4.4.md`（含 §0.3 V4.4.1 修订记录）+ 我的工作、协作关系两份模块 PRD
- 技术选型与依据：`docs/adr/0001-tech-stack.md`
- 领域词汇：`docs/CONTEXT.md` —— 代码命名、API 字段用词必须与词汇表一致，含义冲突时先改词汇表再写代码

## 目录结构

- `docs/`：PRD、词汇表、ADR
- `collaboration-prototype/`：V4.4 需求验证原型（纯前端、可抛弃；只作交互参照，不复用其代码）
- `server/`：Go 后端（`cmd/`、`internal/domain/`、`internal/api/`、`internal/store/`、`migrations/`）
- `web/`：React + TypeScript SPA（Vite + Ant Design 5）
- `openapi.yaml`：唯一 API 契约源（spec-first，手写）

## Coding 流程（每个功能循环）

1. 从 PRD 验收场景（AC-01～AC-49，主 PRD §12）出发，先改 `openapi.yaml`；
2. 重新生成代码：后端 oapi-codegen，前端 openapi-typescript + openapi-fetch；生成物不手改；
3. 业务规则（状态派生、卡点、互锁、审批链、权限、进度、五组归类）只写 `server/internal/domain/`；先写覆盖对应 AC 的表驱动单测，再实现；
4. API handler 保持薄层；集成测试用 httptest + Docker 真 Postgres；
5. 前端不复刻任何规则，界面反馈只消费 API 派生字段；字段不够时回到契约补字段，不在前端计算；
6. 数据访问用 sqlc 生成，库结构变更一律走 goose 迁移（`server/migrations/`），不手改数据库。

## 验证门槛

下「完成」结论前必须真实跑过并附结果：

- `server/`：`go build ./... && go vet ./... && go test ./...`
- `web/`：`npm run build`（含 tsc 类型检查）
- 契约变更时：两端代码重新生成，确认编译通过

## 常用命令

工具链用 Go 1.24+ 的 tool 指令钉在 `server/go.mod`（oapi-codegen、sqlc、goose），无需全局安装。

`server/` 目录下：

- 契约生成：`go tool oapi-codegen -config oapi-codegen.yaml ..\openapi.yaml` → `internal/api/api.gen.go`
- 查询生成：`go tool sqlc generate`（schema 读 `migrations/`，查询在 `internal/store/queries/`）
- 迁移：`go tool goose -dir migrations postgres "postgres://synergy:synergy@localhost:5432/synergy?sslmode=disable" up`（`status` 查看状态）

`web/` 目录下：

- 契约类型生成：`npm run gen:api` → `src/api/schema.d.ts`
- 构建（含 tsc）：`npm run build`；开发：`npm run dev`（/api 代理到 :8080）

仓库根：

- 开发只起数据库：`docker compose up -d postgres`（凭据 synergy/synergy，库名 synergy）
- 全量五容器：`docker compose up -d`
- Playwright 冒烟：尚未搭建，首个业务功能落地后补

生成物（api.gen.go、store/*.sql.go、schema.d.ts）提交进仓库，但不手改。

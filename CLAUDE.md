# 协同管理工具

单团队自用（约 100 人、内网单机部署）的项目协作系统：O／KR／任务三级模型、交付物边、三道审批、我的工作、关系图谱。

## 文档指针（唯一事实源，不在本文件重复）

- 需求：`docs/协同管理工具_详细PRD_V4.5.md`（含 §0.3～0.5 继承修订与 §0.6 参考稿取舍说明）+ `docs/我的工作模块_详细PRD_V1.1.md`、`docs/协作关系模块_详细PRD_V1.1.md`；旧版（V4.4、V1.0）仅作历史参考
- 技术选型与依据：`docs/adr/0001-tech-stack.md`
- 领域词汇：`docs/CONTEXT.md` —— 代码命名、API 字段用词必须与词汇表一致，含义冲突时先改词汇表再写代码

## 目录结构

- `docs/`：PRD、词汇表、ADR
- `collaboration-prototype-v2/`：V4.5 UI 重设计原型（纯前端、可抛弃；是前端视觉与交互的还原基准，但不复用其代码）
- `collaboration-prototype/`：旧 V4.4 原型，已废弃，仅作历史参考
- `server/`：Go 后端（`cmd/`、`internal/domain/`、`internal/api/`、`internal/store/`、`migrations/`）
- `web/`：React + TypeScript SPA（Vite + Ant Design 5）
- `openapi.yaml`：唯一 API 契约源（spec-first，手写）

## Coding 流程（每个功能循环）

1. 从 PRD 验收场景（AC-01～AC-68，主 PRD §12）出发，先改 `openapi.yaml`；
2. 重新生成代码：后端 oapi-codegen，前端 openapi-typescript + openapi-fetch；生成物不手改；
3. 业务规则（状态派生、卡点、互锁、审批链、权限、进度、五组归类）只写 `server/internal/domain/`；严格 red-green-refactor：先写覆盖对应 AC 的表驱动单测；若编译不过，先补最小桩（空实现／零值返回）让测试可编译运行，再真实跑一次、确认**断言级失败**（红 = 断言失败，编译失败只是中间过程、不算红），然后实现转绿，最后按需重构；从未见断言红的测试不算数，「先写后跑直接全绿」不满足本条；
4. API handler 保持薄层；集成测试用 httptest + Docker 真 Postgres；
5. 前端不复刻任何规则，界面反馈只消费 API 派生字段；字段不够时回到契约补字段，不在前端计算；功能范围以 PRD 为准，视觉与交互按 `collaboration-prototype-v2/` 原型逐页还原（布局、配色、组件形态、文案、默认预置项尽量一致，不复用原型代码）；数据模型或范围导致无法还原处，在实现说明中明确指出；调色板、字号契约与组件规格见 `docs/原型设计风格基线.md`（注意 CSS 覆盖顺序：styles.css → collaboration-prototype.css → redesign-v1.css，redesign-v1.css 尾部 4px 圆角 `!important` 契约才是生效样式）；
6. 数据访问用 sqlc 生成，库结构变更一律走 goose 迁移（`server/migrations/`），不手改数据库。

## 验证门槛

下「完成」结论前必须真实跑过并附结果：

- `server/`：`go build ./... && go vet ./... && go test ./...`（集成测试需要 postgres 与 minio 两个容器：`docker compose up -d postgres minio`；MinIO 凭据从环境变量 `MINIO_ROOT_USER`／`MINIO_ROOT_PASSWORD` 读，值取本机 `.env`）
- `web/`：`npm run build`（含 tsc 类型检查）；改动前端结构或样式时再跑 `npm run test:e2e`（Playwright 冒烟，见 `web/e2e/README.md`）
- 契约变更时：两端代码重新生成，确认编译通过

验证／冒烟启动的临时进程（`go run` 起的 server、`npm run dev`、临时端口上的服务等）测试完成后必须关闭，不留后台；`docker compose` 的常驻 postgres 容器除外。

## 常用命令

工具链用 Go 1.24+ 的 tool 指令钉在 `server/go.mod`（oapi-codegen、sqlc、goose），无需全局安装。

`server/` 目录下：

- 契约生成：`go tool oapi-codegen -config oapi-codegen.yaml ..\openapi.yaml` → `internal/api/api.gen.go`
- 查询生成：`go tool sqlc generate`（schema 读 `migrations/`，查询在 `internal/store/queries/`）
- 迁移：先 `$env:DATABASE_URL = "postgres://<用户>:<口令>@localhost:5432/synergy?sslmode=disable"`（口令取本机 `.env`，不写进文档与命令历史），再 `go tool goose -dir migrations postgres $env:DATABASE_URL up`（`status` 查看状态）
- 重置演示数据：`$env:SEED_PASSWORD = "<自定口令>"; go run ./cmd/seed`（清空含用户在内的全部业务数据后重建，数据在 `cmd/seed/sql/`；演示账号口令统一取 `SEED_PASSWORD`，必填、不入库；`-skip-files` 跳过 MinIO 占位文件）

`web/` 目录下：

- 契约类型生成：`npm run gen:api` → `src/api/schema.d.ts`
- 构建（含 tsc）：`npm run build`；开发：`npm run dev`（/api 代理到 :8080）

仓库根（先 `cp .env.example .env` 填好口令，compose 对口令类变量不设默认值）：

- 开发只起数据库：`docker compose up -d postgres`（用户名与库名默认 synergy，口令取 `.env`）；跑集成测试还需 `docker compose up -d minio`（上传走两阶段提交，候选内容必须真的落进对象存储）
- 全量启动（本地构建镜像）：`docker compose up -d --build`
- 部署见 `docs/部署.md`（服务器上 clone + `docker compose up -d --build`，镜像本地构建，不走镜像仓库）
- Playwright 冒烟：`cd web && npm run test:e2e`（需要 postgres 与 minio；会先跑 `cmd/seed` 重建演示数据，**清空全部业务数据**，只在开发库上跑）。覆盖范围与前置见 `web/e2e/README.md`

生成物（api.gen.go、store/*.sql.go、schema.d.ts）提交进仓库，但不手改。

## 上下文与交接

- 每完成一个 ticket（commit 之后）或每落地一项重要决定，主动执行 `/handoff` 更新交接文档，不等用户提醒。
- 上下文压缩交给 auto-compact，模型不主动执行也不模拟 `/compact`。

## Agent skills

### Issue tracker

工作项用 GitHub Issues（gh CLI）跟踪。见 `docs/agents/issue-tracker.md`。

### Triage labels

采用默认五标签（needs-triage / needs-info / ready-for-agent / ready-for-human / wontfix）。见 `docs/agents/triage-labels.md`。

### Domain docs

单一上下文：词汇表在 `docs/CONTEXT.md`，决策在 `docs/adr/`。见 `docs/agents/domain.md`。

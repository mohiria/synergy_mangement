# 协同管理工具

单团队自用（约 100 人、公有云单机部署，公网 IP:80、HTTP 明文）的项目协作系统：O／KR／任务三级模型、交付物边、三道审批、我的工作、关系图谱。

## 文档指针（唯一事实源，不在本文件重复）

- 需求：`docs/协同管理工具_详细PRD_V4.5.md`（含 §0.3～0.5 继承修订与 §0.6 参考稿取舍说明）+ `docs/我的工作模块_详细PRD_V1.1.md`、`docs/协作关系模块_详细PRD_V1.1.md`、`docs/系统设置与账号模块_详细PRD_V1.0.md`（登录页、找回密码、个人中心、系统设置、系统管理员、邮件；AC-70～AC-85）
- 技术选型与依据：`docs/adr/0001-tech-stack.md`
- 领域词汇：`docs/CONTEXT.md` —— 代码命名、API 字段用词必须与词汇表一致，含义冲突时先改词汇表再写代码

## 目录结构

- `docs/`：PRD、词汇表、ADR
- `collaboration-prototype-v4.5-overview-okr-merged/`：最新原型（纯前端、可抛弃；是前端视觉与交互的还原基准，但不复用其代码；旧版 v2 已删除，需要时从 git 历史取回）
- `server/`：Go 后端（`cmd/`、`internal/domain/`、`internal/api/`、`internal/store/`、`migrations/`）
- `web/`：React + TypeScript SPA（Vite + Ant Design 5）
- `openapi.yaml`：唯一 API 契约源（spec-first，手写）

## Coding 流程（每个功能循环）

1. 从 PRD 验收场景（AC-01～AC-69 在主 PRD §12，AC-70～AC-85 在系统设置与账号模块 PRD §14）出发，先改 `openapi.yaml`；
2. 重新生成代码：后端 oapi-codegen，前端 openapi-typescript + openapi-fetch；生成物不手改；
3. 业务规则（状态派生、卡点、互锁、审批链、权限、进度、五组归类）只写 `server/internal/domain/`；严格 red-green-refactor：先写覆盖对应 AC 的表驱动单测；若编译不过，先补最小桩（空实现／零值返回）让测试可编译运行，再真实跑一次、确认**断言级失败**（红 = 断言失败，编译失败只是中间过程、不算红），然后实现转绿，最后按需重构；从未见断言红的测试不算数，「先写后跑直接全绿」不满足本条；
4. API handler 保持薄层；集成测试用 httptest + 真 Postgres（本地经 SSH 隧道用腾讯云开发库）；
5. 前端不复刻任何规则，界面反馈只消费 API 派生字段；字段不够时回到契约补字段，不在前端计算；功能范围以 PRD 为准，视觉与交互按 `collaboration-prototype-v4.5-overview-okr-merged/` 原型逐页还原（布局、配色、组件形态、文案、默认预置项尽量一致，不复用原型代码）；数据模型或范围导致无法还原处，在实现说明中明确指出；调色板、字号契约与组件规格见 `docs/原型设计风格基线.md`（注意 CSS 覆盖顺序：styles.css → collaboration-prototype.css → redesign-v1.css → overview-okr-merged.css，redesign-v1.css 尾部 4px 圆角 `!important` 契约才是生效样式，overview-okr-merged.css 无 `!important`、只加总览与 O/KR 合并页样式）；
6. 数据访问用 sqlc 生成，库结构变更一律走 goose 迁移（`server/migrations/`），不手改数据库。

## 验证门槛

下「完成」结论前必须真实跑过并附结果：

- `server/`：`go build ./... && go vet ./... && go test ./...`（集成测试需要 postgres 与 minio，经 SSH 隧道用腾讯云中间件，见下文「本地中间件」；载入 `.env.tunnel` 即可，它已含测试要的 `TEST_DATABASE_URL`（测试用它建临时库 `synergy_test_*`）、`TEST_MINIO_ENDPOINT` 与 `MINIO_ROOT_USER`／`MINIO_ROOT_PASSWORD`）
- `web/`：`npm run build`（含 tsc 类型检查）＋ `npm test`（vitest 跑 `src/**/*.test.ts` 的纯函数单测，目前是导入器解析层）；改动前端结构或样式时再跑 `npm run test:e2e`（Playwright 冒烟，见 `web/e2e/README.md`）
- 契约变更时：两端代码重新生成，确认编译通过

验证／冒烟启动的临时进程（`go run` 起的 server、`npm run dev`、临时端口上的服务等）测试完成后必须关闭，不留后台；用户明确要求起的本地前后端是长期开发进程，用户说「关掉前后端」再按端口结束。

## 常用命令

工具链用 Go 1.24+ 的 tool 指令钉在 `server/go.mod`（oapi-codegen、sqlc、goose），无需全局安装。

`server/` 目录下：

- 契约生成：`go tool oapi-codegen -config oapi-codegen.yaml ..\openapi.yaml` → `internal/api/api.gen.go`
- 查询生成：`go tool sqlc generate`（schema 读 `migrations/`，查询在 `internal/store/queries/`）
- 迁移：先载入 `.env.tunnel`（`DATABASE_URL` 指向开发库 `synergy_dev`，密码不写进文档与命令历史），再 `go tool goose -dir migrations postgres $env:DATABASE_URL up`（`status` 查看状态）；新迁移先在 `synergy_dev` 上跑通，演示库 `synergy` 由部署时的 `migrate` 服务执行
- 重置演示数据：载入 `.env.tunnel` 后 `$env:SEED_PASSWORD = "<自定密码>"; go run ./cmd/seed`（清空含用户在内的全部业务数据后重建，数据在 `cmd/seed/sql/`；演示账号密码统一取 `SEED_PASSWORD`，必填、不入库；`-skip-files` 跳过 MinIO 占位文件；只对 `synergy_dev` 跑）
- 起后端：载入 `.env.tunnel` 后 `go run ./cmd/server`（:8080；无热重载，改代码后重启）

`web/` 目录下：

- 契约类型生成：`npm run gen:api` → `src/api/schema.d.ts`
- 构建（含 tsc）：`npm run build`；开发：`npm run dev`（/api 代理到 :8080）
- 纯函数单测：`npm test`（vitest，只跑 `src/**/*.test.ts`；e2e 归 Playwright）
- SheetJS（xlsx 解析与模板生成）走仓库内的 `web/vendor/xlsx-0.20.3.tgz`：npm 上的 xlsx 停在 0.18.5 且有两条未修复的高危公告，官方新版只从 cdn.sheetjs.com 分发，内网离线构建因此把 tarball 入库

本地中间件（自 2026-09-10 起本地不跑 Docker Desktop，减少资源占用；postgres／minio／gotenberg 一律经 SSH 隧道用腾讯云服务器上的 compose 服务，前后端在本地起）：

- 隧道由用户在自己的 PowerShell 挂着（模型直接发 `ssh` 会被权限拦截，断了请用户重挂）：`ssh -N -L 5432:127.0.0.1:5432 -L 9000:127.0.0.1:9000 -L 3000:127.0.0.1:3000 ubuntu@<服务器IP>`（IP 与密码在 `.env.tencent`）。服务器上三个端口只绑 127.0.0.1，隧道不改服务器、compose 与安全组。若 ssh 报 `bind: Permission denied`，是本地 Docker 的 postgres/minio 还占着 5432/9000，先停掉
- 只连开发库 `synergy_dev` 与开发桶 `synergy-dev`；**绝不能把 `DATABASE_URL`／`MINIO_BUCKET` 指向演示库 `synergy`／桶 `synergy`**（seed 与 e2e 会清库）
- 环境变量集中在仓库根 `.env.tunnel`（被 `.gitignore` 忽略，模板 `.env.tunnel.example` 入库；含 `DATABASE_URL`、`MINIO_ENDPOINT`／`MINIO_PUBLIC_ENDPOINT`／`MINIO_ACCESS_KEY`／`MINIO_SECRET_KEY`／`MINIO_BUCKET`、`GOTENBERG_URL`、本地 `APP_SECRET_KEY`，以及集成测试用的 `TEST_DATABASE_URL`／`TEST_MINIO_ENDPOINT`／`MINIO_ROOT_USER`／`MINIO_ROOT_PASSWORD`）。**文件必须纯 ASCII**：Windows PowerShell 5.1 把无 BOM 的 UTF-8 当 ANSI 读，中文注释会吞掉换行、丢掉下一行的键
- 载入：PowerShell `Get-Content .env.tunnel | ? { $_ -match '^[A-Z_]+=' } | % { $k,$v = $_ -split '=',2; Set-Item "env:$k" $v }`；sh `set -a; . ./.env.tunnel; set +a`。起服务前先 `Test-NetConnection 127.0.0.1 -Port 5432` 确认隧道在，再查 8080/5173 是否已被占用，占用则不重复起
- 全量启动（本地构建镜像，仅在需要验证 compose 本身时用）：`docker compose up -d --build`（先 `cp .env.example .env` 填好密码，compose 对密码类变量不设默认值）
- 部署见 `docs/部署.md`（服务器上 clone + `docker compose up -d --build`，镜像本地构建，不走镜像仓库）
- Playwright 冒烟：`cd web && npm run test:e2e`（隧道在即可：`playwright.config.ts` 在未设 `DATABASE_URL` 时自动读 `.env.tunnel`，已设的环境变量优先；会先跑 `cmd/seed` 重建演示数据，**清空全部业务数据**，只对 `synergy_dev` 跑；找回密码用例用 `pg` 直连开发库取重置链接）。覆盖范围与前置见 `web/e2e/README.md`

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

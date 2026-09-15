# Playwright 冒烟

覆盖 QA 复核里**能精确断言**的那部分契约（#71）。业务规则的验证在 `server/internal/domain`
的单测与 `internal/api` 的集成测试里，这里不重复；这套只盯前端的结构与视觉契约：

| spec | 覆盖 |
| --- | --- |
| `visual-contract.spec.ts` | 八个业务页与任务详情抽屉的字号 ⊆ {12,14,16}、圆角 ⊆ {4px, 50%}（基线 §3、§4） |
| `task-detail.spec.ts` | 抽屉 Tab 顺序、任务概况五块顺序、切 Tab 不重挂载（AC-31／AC-50／AC-51／AC-56）、新增交付物项入口为选文件（#113） |
| `my-work.spec.ts` | 五分组顺序、计数 pill 徽标与徽标口径、身份卡三要素（AC-16、MW-16） |
| `graph-completed-toggle.spec.ts` | 「显示已完成」开关在 KR 层／聚焦层／全局展开层／关系列表四处一致（AC-45、AC-46） |
| `list-truncation.spec.ts` | 各列表字段单行截断：行高恒定、单元格不换行、页面无横向滚动（#91；1440／1920／2560 三档） |
| `my-work-drawer.spec.ts` | 我的工作卡片在本页打开任务详情抽屉：URL 不切页、按卡片 Tab 落位、关闭回到五分组（#110） |
| `member-zones.spec.ts` | 项目设置成员分两区：角色不混排、跨区转换、无权限时只读（#108） |
| `project-visibility.spec.ts` | 项目可见性开关：设置页有开关且默认私有；切公开后非成员在项目列表看到并标只读、进项目顶部标只读浏览、设置页无保存入口（#111、AC-69） |
| `okr-management.spec.ts` | OKR 管理并入总览：导航无「OKR 管理」、有权限者页头进「管理 O/KR」全页模式（仅结构字段列）、无权限者无入口且直访 /okr 被挡回总览（#125） |
| `graph-drawer.spec.ts` | 协作关系页内联任务抽屉：面板「打开任务详情」与列表「跳转任务」不跳页、「在关系图谱中查看」关抽屉回图谱并保持选中（#121） |
| `progress-slider.spec.ts` | 任务进度行内进度条：页脚无「更新进度」、键盘/拖动 1% 步进即保存并持久化、已完成 100% 不可拖（#119） |
| `completion-review.spec.ts` | 完成申请或签全链路：负责人配置成果审核人并提交、审核人在抽屉「审核」Tab 或签通过、留痕与按钮显隐（#116） |
| `task-import.spec.ts` | 任务批量导入：入口权限（负责人／管理员可见、项目成员不可见）、所属 KR 按编号定位、编号不存在时在预览阶段报错（#107） |
| `okr-batch-groups.spec.ts` | 新增 O / KR 弹窗按所属 O 分组：每组就地加 KR、改归属后行移动、删 O 行后 KR 不丢（#104） |
| `input-source.spec.ts` | 输入源区块：区块名、单行事实与 title、点行进来源任务、逐级返回回到原来的 Tab（#101） |
| `long-title.spec.ts` | 长 O／KR 标题不撑破配置输入弹窗：弹窗与两侧面板无横向滚动、分组标题截断且带全称（#100；1440／1920 两档） |
| `import-csv.spec.ts` | 表格导入的读取：三种 CSV 编码、引号包裹字段、按首行判定分隔符、全空行剔除（#97）；xlsx 前端解析、导入流程不发外链请求、模板现生成（#105）；O／KR 导入器只有六个字段、模板表头被原样认出、未填负责人的行走统一指派（#106）。fixture 在 `e2e/fixtures/` |
| `system-settings.spec.ts` | 系统设置入口与用户管理只读列表：系统管理员在两套壳侧栏底部有入口、四节顺序、主导航七项含项目设置；普通用户无入口、直访 `/system/users` 得 403 页（#201，AC-71） |
| `me.spec.ts` | 个人中心：两套壳浮层进入、浮层无「修改密码」、改显示名后顶栏即时更新、修改密码节可用（#207，AC-77） |
| `login.spec.ts` | 登录页体验：限速后剩余秒数倒计时与按钮禁用、密码框显隐切换、显隐两态禁复制／剪切但可粘贴（#209，AC-79）；找回密码全链路：访问地址与邮件通道都配置后才有入口、重置链接从开发库取（接口不回显正文，#214／#215，AC-84） |

解析层本身的口径由 vitest 单测覆盖（`cd web && npm test`，见 `src/import/parseTable.test.ts`），
这里只验它在真实浏览器与真实上传入口下的表现。

## 跑之前

需要 postgres 与 minio 可达，且库已迁移到最新版本。本地开发不跑 Docker：经 SSH 隧道用腾讯云的
开发库 `synergy_dev` 与开发桶 `synergy-dev`，变量放仓库根 `.env.tunnel`（模板 `.env.tunnel.example`，
隧道命令与载入方式见根目录 `CLAUDE.md`「本地中间件」）：

```sh
ssh -N -L 5432:127.0.0.1:5432 -L 9000:127.0.0.1:9000 -L 3000:127.0.0.1:3000 ubuntu@<服务器IP>   # 另开一个终端挂着
set -a; . ./.env.tunnel; set +a
cd server && go tool goose -dir migrations postgres "$DATABASE_URL" up
```

```sh
cd web
export SEED_PASSWORD='<自定密码>'
npm run test:e2e
```

`playwright.config.ts` 在没设 `DATABASE_URL` 时会自己读仓库根 `.env.tunnel`（已设的环境变量优先），
所以 `webServer` 拉起的后端、`global-setup` 的 seed 与用例都连同一套中间件；不想用隧道时显式导出
`DATABASE_URL` 等变量即可。

`login.spec.ts` 的找回密码用例要从 `mail_outbox` 取重置链接（发送记录接口对该类邮件不回显正文），
用 `pg` 按 `DATABASE_URL` 直连开发库查询，不依赖本地 Docker。

`global-setup.ts` 会先跑 `go run ./cmd/seed -skip-files` 重建演示数据——**该命令清空全部业务
数据**，只在开发库上跑。断言依赖种子里的固定坐标（编号、任务名、已完成任务的分布），见
`fixtures.ts`。已经手工准备好数据时用 `E2E_SKIP_SEED=1` 跳过重建。

后端与前端由 `playwright.config.ts` 的 `webServer` 自动拉起（`reuseExistingServer`，
已经手工起着就直接复用）。

## 浏览器

`@playwright/test` 钉在 1.47：更新的版本不再提供 macOS 12 的浏览器包，本机装不上。
换到 macOS 13+ 或 Linux 后可以直接升。首次要跑一次 `npx playwright install chromium`。

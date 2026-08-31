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
| `graph-drawer.spec.ts` | 协作关系页内联任务抽屉：面板「打开任务详情」与列表「跳转任务」不跳页、「在关系图谱中查看」关抽屉回图谱并保持选中（#121） |
| `progress-slider.spec.ts` | 任务进度行内进度条：页脚无「更新进度」、键盘/拖动 1% 步进即保存并持久化、已完成 100% 不可拖（#119） |
| `completion-review.spec.ts` | 完成申请或签全链路：负责人配置中间审核人并提交、审核人在抽屉「审核」Tab 或签通过、留痕与按钮显隐（#116） |
| `task-import.spec.ts` | 任务批量导入：入口权限（负责人／管理员可见、项目成员不可见）、所属 KR 按编号定位、编号不存在时在预览阶段报错（#107） |
| `okr-batch-groups.spec.ts` | 新增 O / KR 弹窗按所属 O 分组：每组就地加 KR、改归属后行移动、删 O 行后 KR 不丢（#104） |
| `input-source.spec.ts` | 输入源区块：区块名、单行事实与 title、点行进来源任务、逐级返回回到原来的 Tab（#101） |
| `long-title.spec.ts` | 长 O／KR 标题不撑破配置输入弹窗：弹窗与两侧面板无横向滚动、分组标题截断且带全称（#100；1440／1920 两档） |
| `import-csv.spec.ts` | 表格导入的读取：三种 CSV 编码、引号包裹字段、按首行判定分隔符、全空行剔除（#97）；xlsx 前端解析、导入流程不发外链请求、模板现生成（#105）；O／KR 导入器只有六个字段、模板表头被原样认出、未填负责人的行走统一指派（#106）。fixture 在 `e2e/fixtures/` |

解析层本身的口径由 vitest 单测覆盖（`cd web && npm test`，见 `src/import/parseTable.test.ts`），
这里只验它在真实浏览器与真实上传入口下的表现。

## 跑之前

需要 postgres 与 minio 起着，且库已迁移到最新版本：

```sh
docker compose up -d postgres minio      # 无 Docker 时手工起对应服务
cd server && go tool goose -dir migrations postgres "$DATABASE_URL" up
```

```sh
cd web
export DATABASE_URL='postgres://<用户>:<口令>@localhost:5432/synergy?sslmode=disable'
export SEED_PASSWORD='<自定口令>'
npm run test:e2e
```

`global-setup.ts` 会先跑 `go run ./cmd/seed -skip-files` 重建演示数据——**该命令清空全部业务
数据**，只在开发库上跑。断言依赖种子里的固定坐标（编号、任务名、已完成任务的分布），见
`fixtures.ts`。已经手工准备好数据时用 `E2E_SKIP_SEED=1` 跳过重建。

后端与前端由 `playwright.config.ts` 的 `webServer` 自动拉起（`reuseExistingServer`，
已经手工起着就直接复用）。

## 浏览器

`@playwright/test` 钉在 1.47：更新的版本不再提供 macOS 12 的浏览器包，本机装不上。
换到 macOS 13+ 或 Linux 后可以直接升。首次要跑一次 `npx playwright install chromium`。

# Playwright 冒烟

覆盖 QA 复核里**能精确断言**的那部分契约（#71）。业务规则的验证在 `server/internal/domain`
的单测与 `internal/api` 的集成测试里，这里不重复；这套只盯前端的结构与视觉契约：

| spec | 覆盖 |
| --- | --- |
| `visual-contract.spec.ts` | 八个业务页与任务详情抽屉的字号 ⊆ {12,14,16}、圆角 ⊆ {4px, 50%}（基线 §3、§4） |
| `task-detail.spec.ts` | 抽屉 Tab 顺序、任务概况五块顺序、切 Tab 不重挂载（AC-31／AC-50／AC-51／AC-56） |
| `my-work.spec.ts` | 五分组顺序、计数 pill 徽标与徽标口径、身份卡三要素（AC-16、MW-16） |
| `graph-completed-toggle.spec.ts` | 「显示已完成」开关在 KR 层／聚焦层／全局展开层／关系列表四处一致（AC-45、AC-46） |
| `list-truncation.spec.ts` | 各列表字段单行截断：行高恒定、单元格不换行、页面无横向滚动（#91；1440／1920／2560 三档） |
| `okr-batch-groups.spec.ts` | 新增 O / KR 弹窗按所属 O 分组：每组就地加 KR、改归属后行移动、删 O 行后 KR 不丢（#104） |
| `input-source.spec.ts` | 输入源区块：区块名、单行事实与 title、点行进来源任务、逐级返回回到原来的 Tab（#101） |
| `long-title.spec.ts` | 长 O／KR 标题不撑破配置输入弹窗：弹窗与两侧面板无横向滚动、分组标题截断且带全称（#100；1440／1920 两档） |
| `import-csv.spec.ts` | 表格导入的 CSV 读取：UTF-8／UTF-8 BOM／GB18030 编码、引号包裹字段、按首行判定分隔符、全空行剔除（#97；fixture 在 `e2e/fixtures/`） |

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

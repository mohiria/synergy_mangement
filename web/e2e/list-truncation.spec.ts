import { expect, test, type Page } from "@playwright/test";
import { gotoPage, login } from "./fixtures";

// 列表单行截断契约（#91）：所有列表字段单行显示、超长按列宽省略，行高因此恒定。
// 这里只断言能精确验证的三条——不换行、行高一致、页面不出横向滚动条；
// 「悬停显示全称」由 title 属性承载，随文本截断一并断言。

const LIST_PAGES = [
  { path: "/okr", heading: "管理 O/KR" },
  { path: "/tasks", heading: "全部任务" },
  { path: "/artifacts", heading: "成果归档" },
  { path: "/graph", heading: "协作关系" },
  { path: "/reports", heading: "项目报告" },
] as const;

// 校准分辨率（#91）：1920×1080 为主，1440×900 与 2560×1440 同样不换行、页面不横向撑破。
const VIEWPORTS = [
  { width: 1440, height: 900 },
  { width: 1920, height: 1080 },
  { width: 2560, height: 1440 },
] as const;

// 关系页默认进图谱视图，列表在「列表」分段里。
async function openList(page: Page, path: string) {
  if (path === "/graph") await page.getByRole("button", { name: "列表" }).click();
}

// 数据行（分组行 .table-group 是另一档行高）在同一张表内高度必须一致。
const ROW_HEIGHTS = `
Array.from(document.querySelectorAll('.data-table')).map((t) =>
  Array.from(t.querySelectorAll('tbody tr:not(.table-group)'))
    .filter((tr) => !tr.querySelector('.empty'))
    .map((tr) => Math.round(tr.getBoundingClientRect().height)),
)`;

const WRAPPING_CELLS = `
Array.from(document.querySelectorAll('.data-table td, .ant-table-tbody td')).filter((td) => {
  if (td.querySelector('.empty')) return false;
  return getComputedStyle(td).whiteSpace !== 'nowrap';
}).length`;

for (const vp of VIEWPORTS) {
  test.describe(`列表单行截断 ${vp.width}×${vp.height}`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test("项目列表：字段不换行、页面无横向滚动", async ({ page }) => {
      await login(page);
      expect(await page.evaluate(WRAPPING_CELLS)).toBe(0);
      expect(
        await page.evaluate("document.documentElement.scrollWidth <= document.documentElement.clientWidth"),
      ).toBe(true);
    });

    for (const { path, heading } of LIST_PAGES) {
      test(`${heading}：行高恒定、字段不换行、页面无横向滚动`, async ({ page }) => {
        await login(page);
        await gotoPage(page, path);
        await expect(page.getByRole("heading", { name: heading })).toBeVisible();
        await openList(page, path);

        const heights = (await page.evaluate(ROW_HEIGHTS)) as number[][];
        for (const table of heights) {
          expect(new Set(table).size).toBeLessThanOrEqual(1);
        }
        expect(await page.evaluate(WRAPPING_CELLS)).toBe(0);
        // 表格容器自身的横向滚动保留，撑破的是页面时才算违约。
        expect(
          await page.evaluate(
            "document.documentElement.scrollWidth <= document.documentElement.clientWidth",
          ),
        ).toBe(true);
      });
    }
  });
}

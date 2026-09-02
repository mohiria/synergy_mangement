import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 长 O／KR 标题不撑破弹窗与抽屉（#100；#176 弹窗改表格多选后同口径）：
// 「海外专班」这类长标题下，「所属 KR」列在列宽内截断并带全称，弹窗正文不出横向滚动。
// 不改演示数据，改在响应上把标题拉长——本票要验的是布局，不是数据。

const LONG_O =
  "海外专班：东南亚与中东多国分支机构核心业务系统数据库国产化替换整体推进".repeat(4);
const LONG_KR =
  "完成三套核心库在多时区多语种环境下的兼容性评估、改造清单与回退预案编制".repeat(4);

const VIEWPORTS = [
  { width: 1440, height: 900 },
  { width: 1920, height: 1080 },
] as const;

// 只看当前可见的那层浮层：抽屉里预挂载着好几个用同样 class 的弹窗。
const OVERFLOW = `
(() => {
  const wraps = [...document.querySelectorAll('.ant-modal-wrap')]
    .filter((w) => getComputedStyle(w).display !== 'none');
  const root = wraps[wraps.length - 1];
  const els = [...root.querySelectorAll('.ant-modal-body, .input-source-table')];
  return els
    .map((el) => [el.className.split(' ')[0], el.scrollWidth - el.clientWidth])
    .filter(([, d]) => d > 1);
})()`;

for (const vp of VIEWPORTS) {
  test.describe(`长 O／KR 标题 ${vp.width}×${vp.height}`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test("选择输入源弹窗不被撑宽，所属 KR 列截断且带全称", async ({ page }) => {
      await page.route("**/api/v1/projects/*/objectives", async (route) => {
        const res = await route.fetch();
        const body = await res.json();
        for (const o of body) {
          o.title = LONG_O;
          for (const k of o.keyResults) k.description = LONG_KR;
        }
        await route.fulfill({ response: res, json: body });
      });

      await login(page);
      await gotoPage(page, "/tasks");
      await page.getByRole("button", { name: DEMO.activeTask.name }).click();
      await page.getByRole("button", { name: "选择输入源" }).click();
      const table = page.locator(".input-source-table");
      await expect(table.locator(".ant-table-row").first()).toBeVisible();
      await page.waitForTimeout(300);

      // 弹窗正文与表格都不出横向滚动
      expect(await page.evaluate(OVERFLOW)).toEqual([]);

      // 「所属 KR」列带全称（截断后靠 title 悬停看），且列启用省略截断
      const krCell = table.locator("tbody td span[title*='KR']").first();
      const title = await krCell.getAttribute("title");
      expect(title).toContain(LONG_KR);
      const ellipsised = await krCell.evaluate(
        (el) => !!el.closest("td")?.classList.contains("ant-table-cell-ellipsis"),
      );
      expect(ellipsised).toBe(true);
    });
  });
}

import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 长 O／KR 标题不撑破弹窗与抽屉（#100）：在「海外专班」这类长标题下，
// 树形穿梭框的分组标题与任务行都要在面板宽度内截断，弹窗正文不出横向滚动。
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
  const els = [...root.querySelectorAll(
    '.ant-modal-body, .tree-transfer, .tree-transfer .ant-transfer-list,' +
    ' .tree-transfer .ant-transfer-list-body-customize-wrapper')];
  return els
    .map((el) => [el.className.split(' ')[0], el.scrollWidth - el.clientWidth])
    .filter(([, d]) => d > 1);
})()`;

for (const vp of VIEWPORTS) {
  test.describe(`长 O／KR 标题 ${vp.width}×${vp.height}`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test("配置输入弹窗不被撑宽，分组标题截断且带全称", async ({ page }) => {
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
      await page.getByRole("button", { name: "配置输入" }).click();
      await expect(page.locator(".tree-transfer-group b").first()).toBeVisible();
      await page.waitForTimeout(300);

      // 弹窗正文与两侧面板都不出横向滚动
      expect(await page.evaluate(OVERFLOW)).toEqual([]);

      // 分组标题带全称（截断后靠 title 悬停看），并且确实被截断了
      const titles = await page
        .locator(".tree-transfer-group b")
        .evaluateAll((els) => els.map((el) => el.getAttribute("title")));
      expect(titles).toContain(LONG_O);
      const clipped = await page
        .locator(".tree-transfer-group b")
        .first()
        .evaluate((el) => el.scrollWidth > el.clientWidth);
      expect(clipped).toBe(true);
    });
  });
}

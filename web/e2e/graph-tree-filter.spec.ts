import { expect, test, type Page } from "@playwright/test";
import { gotoPage, login } from "./fixtures";

// F-11（#114，裁决 K＝A）：O／KR／人员筛选在层级树（默认层）也要淡化不匹配内容，
// 与全局展开、KR 任务关系层同口径（AC-45「淡化不匹配内容并保留上下文」）。
// 固定坐标（cmd/seed）：1 号项目有 3 个 O，选中其一后另外两个 O 及其 KR 应淡化。

async function openTree(page: Page) {
  await gotoPage(page, "/graph");
  await expect(page.locator(".page h1").first()).toHaveText("协作关系");
  await expect(page.locator(".gnode-o").first()).toBeVisible();
}

async function pickFilter(page: Page, current: string, optionIndex: number) {
  await page.locator(".toolbar .ant-select", { hasText: current }).first().click();
  await page
    .locator(".ant-select-dropdown:visible .ant-select-item-option")
    .nth(optionIndex)
    .click();
}

test.describe("层级树筛选淡化", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test("O 筛选淡化非选中 O 及其 KR，选中 O 保持高亮", async ({ page }) => {
    await openTree(page);
    await expect(page.locator(".gnode.dimmed")).toHaveCount(0);
    await pickFilter(page, "全部 O", 1);
    await expect(page.locator(".gnode-o.dimmed")).toHaveCount(2);
    await expect(page.locator(".gnode-o:not(.dimmed)")).toHaveCount(1);
    await expect(page.locator(".gnode-kr.dimmed").first()).toBeVisible();
    await expect(page.locator(".gnode-kr:not(.dimmed)").first()).toBeVisible();
  });

  test("人员筛选按该 KR 下有无该人员的任务淡化", async ({ page }) => {
    await openTree(page);
    await pickFilter(page, "全部人员", 1);
    // 任何一个人都不会在项目全部 KR 下都有任务：必有淡化，也必有保留。
    await expect(page.locator(".gnode-kr.dimmed").first()).toBeVisible();
    await expect(page.locator(".gnode-kr:not(.dimmed)").first()).toBeVisible();
  });
});

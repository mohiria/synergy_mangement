import { expect, test } from "@playwright/test";
import { gotoPage, login } from "./fixtures";

// 新增 O / KR 弹窗按所属 O 分组（#104）：每个 O 自成一组、组末尾就地加 KR，
// 行内「所属 O」下拉仍可改归属。断言只到结构与编号预览，不落库。

test.describe("新增 O / KR 弹窗分组", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await gotoPage(page, "/okr");
    await page.getByRole("button", { name: /新增 O \/ KR/ }).click();
    await expect(page.locator(".okr-sheet-group").first()).toBeVisible();
  });

  test("本次新建 O 与已有 O 各自成组，每组末尾都能就地加 KR", async ({ page }) => {
    const groups = page.locator(".okr-sheet-group");
    // 打开即预置一个新 O 组；演示数据有 O1～O3 三个已有 O。
    await expect(groups).toHaveCount(4);
    // 每组都有自己的「＋ 添加 KR」
    await expect(page.getByRole("button", { name: /添加 KR/ })).toHaveCount(4);

    // 点 O2 那一组的按钮，新行落在 O2 组里，编号接 O2 现有 KR
    const o2 = groups.nth(2);
    await expect(o2.locator(".okr-sheet-grouphead")).toContainText("O2");
    await o2.getByRole("button", { name: /添加 KR/ }).click();
    await expect(o2.locator(".okr-sheet-row")).toHaveCount(1);
    await expect(o2.locator(".sheet-code")).toHaveText("KR2.3");

    // 其余组没有被加行
    await expect(groups.nth(3).locator(".okr-sheet-row")).toHaveCount(0);
  });

  test("改「所属 O」后该行移动到目标组", async ({ page }) => {
    const groups = page.locator(".okr-sheet-group");
    // 打开即预置的一组是「O 行 + 一条 KR 行」。
    const newO = groups.nth(0);
    await expect(newO.locator(".okr-sheet-row")).toHaveCount(2);

    // 把这条 KR 改挂到 O3
    await newO.locator(".ant-select-selector").first().click();
    await page.locator(".ant-select-dropdown:visible .ant-select-item-option", { hasText: "O3" }).click();

    await expect(newO.locator(".okr-sheet-row")).toHaveCount(1); // 只剩 O 行
    const o3 = groups.nth(3);
    await expect(o3.locator(".okr-sheet-grouphead")).toContainText("O3");
    await expect(o3.locator(".okr-sheet-row")).toHaveCount(1);
    await expect(o3.locator(".sheet-code")).toHaveText("KR3.2");
  });

  test("删掉新建 O 行后，组内 KR 落到「待指定所属 O」不静默丢失", async ({ page }) => {
    const newO = page.locator(".okr-sheet-group").nth(0);
    await newO.getByRole("button", { name: /添加 KR/ }).click();
    await expect(newO.locator(".okr-sheet-row")).toHaveCount(3); // O 行 + 预置 KR + 新加 KR
    // 删掉组头的 O 行
    await newO.getByRole("button", { name: "删除该行" }).first().click();

    const orphan = page.locator(".okr-sheet-group", { hasText: "待指定所属 O" });
    await expect(orphan).toBeVisible();
    await expect(orphan.locator(".okr-sheet-row")).toHaveCount(2);
  });
});

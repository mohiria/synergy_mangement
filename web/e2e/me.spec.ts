import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 个人中心（#207，AC-77）：两套壳右上角浮层进入；浮层不再有「修改密码」；改显示名后顶栏即时更新；
// 修改密码节可用。种子每次重建，改动不会累积。

test("两套壳的浮层都能进入个人中心，且不再有修改密码按钮", async ({ page }) => {
  await login(page);
  await page.getByRole("button", { name: "当前身份" }).click();
  const popover = page.locator(".identity-popover");
  await expect(popover.getByRole("button", { name: "修改密码" })).toHaveCount(0);
  await popover.getByRole("button", { name: "个人中心" }).click();
  await expect(page).toHaveURL(/\/me\/profile$/);
  await expect(page.locator(".page h1").first()).toHaveText("个人中心");

  await gotoPage(page, "");
  await expect(page.locator(".page h1").first()).toHaveText("项目总览");
  await page.getByRole("button", { name: "当前身份" }).click();
  await expect(page.locator(".identity-popover").getByRole("button", { name: "修改密码" })).toHaveCount(0);
  await page.locator(".identity-popover").getByRole("button", { name: "个人中心" }).click();
  await expect(page.locator(".page h1").first()).toHaveText("个人中心");
});

test("改显示名后顶栏即时更新", async ({ page }) => {
  await login(page);
  await page.goto("/me/profile");
  await page.getByLabel("显示名").fill("赵文琪·改");
  await page.getByRole("button", { name: /保\s*存/ }).click();
  await expect(page.locator(".topbar .identity .who b")).toHaveText("赵文琪·改");
  // 改回去，避免影响同一轮里依赖显示名的其他用例（各用例独立浏览器上下文，但数据共享）。
  await page.getByLabel("显示名").fill(DEMO.admin.displayName);
  await page.getByRole("button", { name: /保\s*存/ }).click();
  await expect(page.locator(".topbar .identity .who b")).toHaveText(DEMO.admin.displayName);
});

// 用没有其他用例依赖登录的何静（hejing）改密，避免同一轮里连累依赖固定密码的用例。
test("修改密码节：新密码可登录", async ({ page }) => {
  await login(page, "hejing");
  await page.goto("/me/password");
  await page.getByPlaceholder("当前密码").fill(DEMO.password);
  await page.getByPlaceholder("新密码（8～32 位）").fill("changed-by-me-1");
  await page.getByPlaceholder("再次输入新密码").fill("changed-by-me-1");
  await page.getByRole("button", { name: "确认修改" }).click();
  // 成功后表单清空（toast 3 秒即逝，不拿它当断言）。
  await expect(page.getByPlaceholder("当前密码")).toHaveValue("");
  await page.getByRole("button", { name: "当前身份" }).click();
  await page.getByRole("button", { name: "登 出" }).click();
  await page.goto("/"); // 登出后仍停在 /me/password；从根路径登录才会落到项目列表
  await page.getByPlaceholder("请输入用户名").fill("hejing");
  await page.getByPlaceholder("请输入密码").fill("changed-by-me-1");
  await page.locator('button[type="submit"]').click();
  await expect(page.getByRole("heading", { name: "项目列表" })).toBeVisible();
});

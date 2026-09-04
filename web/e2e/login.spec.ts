import { expect, test } from "@playwright/test";
import { DEMO } from "./fixtures";

// 登录页体验（#209，AC-79）：限速后显示剩余秒数并倒计时、按钮禁用；密码框显隐切换；
// 显隐两种状态下都不能复制／剪切、可粘贴（共享 PasswordInput）。
// 限速按（用户名, IP）计数，用一个不存在的用户名触发，不影响演示账号。

test("触发限速后显示剩余秒数并倒计时", async ({ page }) => {
  await page.goto("/");
  for (let i = 0; i < 5; i++) {
    await page.getByPlaceholder("请输入用户名").fill("e2e_nobody_ratelimit");
    await page.getByPlaceholder("请输入密码").fill("wrong-pass-1");
    await page.locator('button[type="submit"]').click();
    await expect(page.getByText("用户名或密码错误")).toBeVisible();
  }
  await page.locator('button[type="submit"]').click();
  const warn = page.locator(".ant-alert-warning");
  await expect(warn).toContainText(/尝试过多，请 \d+ 秒后再试/);
  const first = Number((await warn.innerText()).match(/(\d+) 秒/)?.[1]);
  expect(first).toBeGreaterThan(0);
  await expect(page.locator('button[type="submit"]')).toBeDisabled();
  // 一秒后数字减小。
  await expect.poll(async () => Number((await warn.innerText()).match(/(\d+) 秒/)?.[1]), { timeout: 5000 }).toBeLessThan(first);
});

test("密码框显隐切换，且两种状态下都禁复制、剪切，可粘贴", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/");
  const pwd = page.getByPlaceholder("请输入密码");
  await pwd.fill(DEMO.password);
  await expect(pwd).toHaveAttribute("type", "password");
  // 预置剪贴板内容，之后判断复制是否被拦。
  await page.evaluate(() => navigator.clipboard.writeText("sentinel"));
  const tryCopyCut = async () => {
    await pwd.focus();
    await page.keyboard.press("ControlOrMeta+A");
    await page.keyboard.press("ControlOrMeta+C");
    await page.keyboard.press("ControlOrMeta+X");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("sentinel");
    await expect(pwd).toHaveValue(DEMO.password); // 剪切被拦，内容不变
  };
  await tryCopyCut();
  await page.locator(".ant-input-password-icon").click(); // 切到明文
  await expect(pwd).toHaveAttribute("type", "text");
  await tryCopyCut();
  // 粘贴可用。
  await pwd.fill("");
  await page.evaluate(() => navigator.clipboard.writeText("pasted-pass-1"));
  await pwd.focus();
  await page.keyboard.press("ControlOrMeta+V");
  await expect(pwd).toHaveValue("pasted-pass-1");
});

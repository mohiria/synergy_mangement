import { expect, test } from "@playwright/test";
import { DEMO, login } from "./fixtures";

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

// #214：SMTP 未配置时登录页无「忘记密码」；配置后出现；请求后从发送记录（管理员可见正文）取链接完成重置，
// 新密码可登录且不被强制改密（AC-84）。用郑凯（zhengkai）：其余用例不依赖他的密码。
test("找回密码：入口按邮件通道显示，链接可完成重置", async ({ page, browser }) => {
  await page.goto("/");
  await expect(page.getByRole("link", { name: "忘记密码？" })).toHaveCount(0);

  // 管理员配置通道。
  const admin = await browser.newContext();
  const adminPage = await admin.newPage();
  await login(adminPage);
  await adminPage.goto("/system/notifications");
  await adminPage.getByLabel("SMTP 主机").fill("smtp.invalid");
  await adminPage.getByLabel("端口").fill("2525");
  await adminPage.getByLabel("发件人地址").fill("bot@example.com");
  await adminPage.getByRole("button", { name: "保存通道" }).click();
  await expect(adminPage.getByText("通道已配置")).toBeVisible();

  await page.reload();
  await page.getByRole("link", { name: "忘记密码？" }).click();
  await page.getByPlaceholder("用户名或邮箱").fill("zhengkai");
  await page.getByRole("button", { name: "发送重置邮件" }).click();
  await expect(page.getByText("若账号存在，重置邮件已发送")).toBeVisible();

  // 从发送记录正文里取链接。
  const res = await adminPage.request.get("/api/v1/system/mail-outbox");
  const items = (await res.json()) as { toAddress: string; body?: string }[];
  const mail = items.find((x) => x.toAddress === "zhengkai@example.com");
  const link = mail?.body?.match(/https?:\/\/\S+\/reset-password\?token=[0-9a-f]+/)?.[0];
  expect(link).toBeTruthy();
  const url = new URL(link!);
  await page.goto(`/reset-password?token=${url.searchParams.get("token")}`);
  await page.getByPlaceholder("新密码（8～32 位）").fill("recovered-pass-1");
  await page.getByPlaceholder("再次输入新密码").fill("recovered-pass-1");
  await page.getByRole("button", { name: "设置新密码" }).click();
  await expect(page.getByText("密码已重置")).toBeVisible();

  await page.goto("/");
  await page.getByPlaceholder("请输入用户名").fill("zhengkai");
  await page.getByPlaceholder("请输入密码").fill("recovered-pass-1");
  await page.locator('button[type="submit"]').click();
  await expect(page.getByRole("heading", { name: "项目列表" })).toBeVisible();
  await admin.close();
});


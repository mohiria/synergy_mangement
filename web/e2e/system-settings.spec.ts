import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 系统设置入口与用户管理只读列表（#201，AC-71）：系统管理员在两套壳的侧栏底部看到「系统设置」，
// 普通用户看不到入口、直接访问得 403 页。种子里赵文琪是系统管理员（#200），林小雨不是。

test("系统管理员从项目列表壳进入系统设置并看到用户列表", async ({ page }) => {
  await login(page);
  const foot = page.locator(".sidebar-foot");
  await expect(foot.getByText("系统设置")).toBeVisible();
  await foot.getByText("系统设置").click();
  await expect(page).toHaveURL(/\/system\/users$/);
  await expect(page.locator(".page h1").first()).toHaveText("系统设置");
  // 四节顺序固定，当前在「用户管理」。
  const sections = await page.locator(".settings-nav button").allInnerTexts();
  expect(sections.map((s) => s.trim())).toEqual(["基本信息", "通知设置", "用户管理", "操作审计"]);
  await expect(page.locator(".settings-nav button.active")).toHaveText("用户管理");
  const row = page.locator(".settings-panel tr", { hasText: DEMO.admin.username });
  await expect(row).toContainText(DEMO.admin.displayName);
  await expect(row).toContainText(`${DEMO.admin.username}@example.com`); // #202：邮箱列
  await expect(row).toContainText("是");
});

test("系统管理员在项目壳侧栏底部也有系统设置，主导航七项含项目设置", async ({ page }) => {
  await login(page);
  await gotoPage(page, "");
  await expect(page.locator(".page h1").first()).toHaveText("项目总览");
  await expect(page.locator(".sidebar-foot").getByText("系统设置")).toBeVisible();
  const labels = await page.locator(".sidebar nav .nav-row span").allInnerTexts();
  expect(labels.map((l) => l.trim())).toEqual([
    "项目总览",
    "全部任务",
    "协作关系",
    "我的工作",
    "成果归档",
    "项目报告",
    "项目设置",
  ]);
});

test("普通用户没有入口，直接访问得 403 页", async ({ page }) => {
  await login(page, DEMO.outsider.username);
  await expect(page.locator(".sidebar-foot")).toHaveCount(0);
  await page.goto("/system/users");
  await expect(page.getByText("403 无权访问")).toBeVisible();
  await expect(page.locator(".settings-panel")).toHaveCount(0);
});

// #203：建号 → 首登 → 强制改密 → 进入系统（AC-73）。种子每次重建，用户名不会撞。
test("管理员建号后新用户首次登录被引导改密，改完进入系统", async ({ page }) => {
  await login(page);
  await page.goto("/system/users");
  await page.getByRole("button", { name: "新建用户" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("用户名").fill("e2e_newbie");
  await dialog.getByLabel("显示名").fill("新同事");
  await dialog.getByLabel("邮箱").fill("e2e_newbie@example.com");
  await dialog.getByLabel("初始密码").fill("init-pass-1");
  await dialog.getByRole("button", { name: "创 建" }).click();
  await expect(page.locator(".settings-panel tr", { hasText: "e2e_newbie" })).toContainText("待首次改密");

  // 登出，以新用户登录：只见首次改密页，业务路由也回到这里。
  await page.getByRole("button", { name: "当前身份" }).click();
  await page.getByRole("button", { name: "登 出" }).click();
  await page.getByPlaceholder("请输入用户名").fill("e2e_newbie");
  await page.getByPlaceholder("请输入密码").fill("init-pass-1");
  await page.locator('button[type="submit"]').click();
  await expect(page.getByRole("heading", { name: "首次登录请设置新密码" })).toBeVisible();
  await page.goto(`/projects/${DEMO.projectId}`);
  await expect(page.getByRole("heading", { name: "首次登录请设置新密码" })).toBeVisible();

  await page.getByPlaceholder("新密码（8～32 位）").fill("brand-new-pass-1");
  await page.getByPlaceholder("再次输入新密码").fill("brand-new-pass-1");
  await page.getByRole("button", { name: "设置新密码并进入" }).click();
  // 改完停留在原路由（1 号项目对新用户是 404），回项目列表应能正常进入、不再被引导改密。
  await expect(page.getByRole("heading", { name: "首次登录请设置新密码" })).toHaveCount(0);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "项目列表" })).toBeVisible();
});

// #204：停用 → 正确密码登录得「已停用」提示 → 启用后可登录（AC-74）。林小雨不是系统管理员，
// 只在 2 号项目里。
test("停用后正确密码登录提示已停用，启用后恢复", async ({ page }) => {
  await login(page);
  await page.goto("/system/users");
  const row = page.locator(".settings-panel tr", { hasText: DEMO.outsider.username });
  // antd 只给默认尺寸的两字按钮插空格，这里用正则同时兼容「停用」「停 用」。
  await row.getByRole("button", { name: /停\s*用/ }).click();
  await page.locator(".ant-popconfirm").getByRole("button", { name: /停\s*用/ }).click(); // Popconfirm 确认
  await expect(row).toContainText("已停用");
  // 自己那一行的停用按钮禁用。
  await expect(page.locator(".settings-panel tr", { hasText: DEMO.admin.username }).getByRole("button", { name: /停\s*用/ })).toBeDisabled();

  await page.getByRole("button", { name: "当前身份" }).click();
  await page.getByRole("button", { name: "登 出" }).click();
  await page.getByPlaceholder("请输入用户名").fill(DEMO.outsider.username);
  await page.getByPlaceholder("请输入密码").fill(DEMO.password);
  await page.locator('button[type="submit"]').click();
  await expect(page.getByText("账号已停用，请联系管理员")).toBeVisible();
  // 错误密码仍是统一文案。
  await page.getByPlaceholder("请输入密码").fill("definitely-wrong-1");
  await page.locator('button[type="submit"]').click();
  await expect(page.getByText("用户名或密码错误")).toBeVisible();

  await login(page);
  await page.goto("/system/users");
  await row.getByRole("button", { name: /启\s*用/ }).click();
  await expect(row).not.toContainText("已停用");
  await page.getByRole("button", { name: "当前身份" }).click();
  await page.getByRole("button", { name: "登 出" }).click();
  await login(page, DEMO.outsider.username);
});

// #205：重置密码 → 该用户新密码登录被引导改密；设／撤系统管理员，撤销自己被禁用（AC-75）。
test("重置密码后新密码登录被引导改密；设撤系统管理员且不能撤销自己", async ({ page }) => {
  await login(page);
  await page.goto("/system/users");
  const row = page.locator(".settings-panel tr", { hasText: DEMO.outsider.username });
  await row.getByRole("button", { name: `更多操作 ${DEMO.outsider.username}` }).click();
  await page.getByRole("menuitem", { name: "重置密码" }).click();
  await page.getByPlaceholder("新密码（8～32 位）").fill("reset-by-admin-1");
  await page.getByRole("button", { name: /重\s*置/ }).click();
  await expect(row).toContainText("待首次改密");

  // 设为系统管理员 → 行显示「是」；再撤销。
  await row.getByRole("button", { name: `更多操作 ${DEMO.outsider.username}` }).click();
  await page.getByRole("menuitem", { name: "设为系统管理员" }).click();
  await expect(row).toContainText("是");
  await row.getByRole("button", { name: `更多操作 ${DEMO.outsider.username}` }).click();
  await page.getByRole("menuitem", { name: "撤销系统管理员" }).click();
  await expect(row).not.toContainText("是");
  // 自己那一行的「撤销系统管理员」禁用。
  await page.locator(".settings-panel tr", { hasText: DEMO.admin.username }).getByRole("button", { name: `更多操作 ${DEMO.admin.username}` }).click();
  await expect(page.getByRole("menuitem", { name: "撤销系统管理员" })).toHaveAttribute("aria-disabled", "true");
  await page.keyboard.press("Escape");

  // 新密码登录 → 首次改密页。
  await page.getByRole("button", { name: "当前身份" }).click();
  await page.getByRole("button", { name: "登 出" }).click();
  await page.getByPlaceholder("请输入用户名").fill(DEMO.outsider.username);
  await page.getByPlaceholder("请输入密码").fill("reset-by-admin-1");
  await page.locator('button[type="submit"]').click();
  await expect(page.getByRole("heading", { name: "首次登录请设置新密码" })).toBeVisible();
});

// #206：系统设置「操作审计」列出系统级写操作（建号等），操作者为当前管理员（AC-76）。
test("操作审计节列出系统级写操作", async ({ page }) => {
  await login(page);
  await page.goto("/system/users");
  await page.getByRole("button", { name: "新建用户" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("用户名").fill("e2e_audited");
  await dialog.getByLabel("显示名").fill("审计样本");
  await dialog.getByLabel("邮箱").fill("e2e_audited@example.com");
  await dialog.getByLabel("初始密码").fill("init-pass-1");
  await dialog.getByRole("button", { name: "创 建" }).click();
  await expect(page.locator(".settings-panel tr", { hasText: "e2e_audited" })).toBeVisible();

  await page.locator(".settings-nav button", { hasText: "操作审计" }).click();
  await expect(page).toHaveURL(/\/system\/audit$/);
  const first = page.locator(".settings-panel tbody tr").first();
  await expect(first).toContainText("新建用户");
  await expect(first).toContainText(DEMO.admin.displayName);
});

// #210：改名称 → 侧栏与标签页标题同步 → 登出 → 登录页显示新名称与提示语；最后改回默认值（AC-80）。
test("改系统名称后侧栏、标签页与登录页同步，超长被拒", async ({ page }) => {
  await login(page);
  await page.goto("/system/basic");
  const name = page.getByLabel("系统名称");
  await expect(name).toHaveValue("协同管理工具");
  // 前端 maxLength 挡在 10 字，输入 12 字只留 10。
  await name.fill("一二三四五六七八九十一二");
  await expect(name).toHaveValue("一二三四五六七八九十");
  await name.fill("协同平台");
  await page.getByLabel("登录页提示语（可空）").fill("请用工号登录");
  await page.getByRole("button", { name: /保\s*存/ }).click();
  await expect(page.locator(".sidebar .brand-name b")).toHaveText("协同平台");
  await expect(page).toHaveTitle("协同平台");

  await page.getByRole("button", { name: "当前身份" }).click();
  await page.getByRole("button", { name: "登 出" }).click();
  await expect(page.locator(".login-brand .brand-name b")).toHaveText("协同平台");
  await expect(page.locator(".login-foot")).toContainText("请用工号登录");

  // 改回默认，避免影响其他用例。
  await login(page);
  await page.goto("/system/basic");
  await page.getByLabel("系统名称").fill("协同管理工具");
  await page.getByLabel("登录页提示语（可空）").fill("账号由管理员分配");
  await page.getByRole("button", { name: /保\s*存/ }).click();
  await expect(page.locator(".sidebar .brand-name b")).toHaveText("协同管理工具");
});

// #211：上传 PNG 后侧栏品牌位与 favicon 显示该图，SVG 被拒；删除后回退首字（AC-81）。
test("上传 logo 后品牌位与 favicon 显示图片，SVG 被拒，删除后回退首字", async ({ page }) => {
  await login(page);
  await page.goto("/system/basic");
  // 1×1 红色 PNG。
  const png = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg==",
    "base64",
  );
  const fileInput = page.locator('[data-testid="logo-block"] input[type="file"]');
  await fileInput.setInputFiles({ name: "bad.svg", mimeType: "image/svg+xml", buffer: Buffer.from("<svg xmlns='http://www.w3.org/2000/svg'/>") });
  await expect(page.locator('[data-testid="logo-error"]')).toContainText("仅支持 PNG、JPG、WebP");
  await fileInput.setInputFiles({ name: "logo.png", mimeType: "image/png", buffer: png });
  await expect(page.locator(".sidebar .brand-mark img")).toBeVisible();
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute("href", /\/api\/v1\/branding\/logo\?v=\d+/);
  await expect.poll(async () => {
    const res = await page.request.get("/api/v1/branding/logo");
    return `${res.status()} ${res.headers()["content-type"]}`;
  }).toBe("200 image/png");

  await page.getByRole("button", { name: "删除 logo" }).click();
  await page.getByRole("button", { name: /删\s*除/ }).last().click();
  await expect(page.locator(".sidebar .brand-mark img")).toHaveCount(0);
  await expect(page.locator(".sidebar .brand-mark")).toHaveText("协");
});

// #212：保存邮件通道（密码不回显）→ 发送测试邮件入队 → 发送记录可见（AC-82）。
// 本机没有 SMTP，后台发送会失败并显示原因；这里只断言入队与记录，不等待重试。
test("邮件通道保存后密码不回显，测试邮件入队并出现在发送记录", async ({ page }) => {
  await login(page);
  await page.goto("/system/notifications");
  await page.getByLabel("SMTP 主机").fill("smtp.invalid");
  await page.getByLabel("端口").fill("2525");
  await page.getByLabel("账号（可空）").fill("bot");
  await page.getByLabel("密码").fill("smtp-secret-1");
  await page.getByLabel("发件人地址").fill("bot@example.com");
  await page.getByRole("button", { name: "保存通道" }).click();
  await expect(page.getByText("通道已配置")).toBeVisible();
  await expect(page.getByLabel("密码（已设置，留空保持不变）")).toHaveValue("");
  await page.reload();
  await expect(page.getByLabel("密码（已设置，留空保持不变）")).toHaveValue("");

  await page.locator('[data-testid="test-mail"]').getByText("发到其他邮箱").click();
  await page.getByPlaceholder("收件地址").fill("ops@example.com");
  await page.getByRole("button", { name: "发送测试邮件" }).click();
  const row = page.locator(".settings-panel tbody tr", { hasText: "ops@example.com" }).first();
  await expect(row).toContainText("测试邮件");
  await expect(row).toContainText(/待发送|失败/);
});


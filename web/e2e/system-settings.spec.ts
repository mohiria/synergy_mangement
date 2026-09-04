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

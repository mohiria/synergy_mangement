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

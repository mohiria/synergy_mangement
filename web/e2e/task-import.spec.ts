import { expect, test } from "@playwright/test";
import { gotoPage, login } from "./fixtures";

// 任务批量导入（AC-02b、#107）：入口权限、所属 KR 按编号定位、编号不存在时在预览阶段报错。
// 不落库——真正的写入路径由服务端集成测试覆盖。

test("入口只对项目负责人／项目管理员开放", async ({ page }) => {
  await login(page); // 赵文琪：1 号项目的项目负责人兼管理员
  await gotoPage(page, "/tasks");
  await expect(page.getByRole("button", { name: "批量导入任务" })).toBeVisible();
});

test("项目成员看不到批量导入任务入口", async ({ page }) => {
  await login(page, "wanghaoran"); // 王浩然：项目成员
  await gotoPage(page, "/tasks");
  await expect(page.locator(".page h1").first()).toHaveText("全部任务");
  await expect(page.getByRole("button", { name: "批量导入任务" })).toHaveCount(0);
});

test("所属 KR 按编号定位，编号不存在的行在预览阶段报错", async ({ page }) => {
  await login(page);
  await gotoPage(page, "/tasks");
  await page.getByRole("button", { name: "批量导入任务" }).click();
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/import-tasks.csv");
  await page.getByRole("button", { name: /下一步：字段映射/ }).click();
  await page.getByRole("button", { name: /下一步：人员匹配/ }).click();
  await page.getByRole("button", { name: /下一步：结构预览/ }).click();

  // 编号存在的那条进了 KR1.1 分组
  await expect(page.getByText("KR1.1 · 1 项任务")).toBeVisible();
  // 编号不存在的那条明确报错，且导入按钮被拦住
  await expect(page.getByText(/所属 KR 编号「KR9.9」在本项目内不存在/)).toBeVisible();
  await expect(page.getByRole("button", { name: "确认导入" })).toBeDisabled();
});

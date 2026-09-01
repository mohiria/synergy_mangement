import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// #119：任务进度改为基础信息里的可拖进度条（刻度 1%），抽屉页脚不再有「更新进度」按钮。
// 种子坐标：T1.2.2 负责人王浩然、进行中 65%；T1.1.1 已完成（进度由后端派生 100，不可拖）。

test("负责人在基础信息里拖动进度条即保存（#119）", async ({ page }) => {
  await login(page, "wanghaoran");
  await gotoPage(page, "/tasks");
  await page.getByText(DEMO.activeTask.name).first().click();
  const drawer = page.locator(".ant-drawer-content");
  await expect(drawer).toBeVisible();
  // 页脚没有「更新进度」按钮（动作收进基础信息行内）
  await expect(drawer.getByRole("button", { name: "更新进度" })).toHaveCount(0);
  const slider = drawer.locator("[data-focus='basic'] .ant-slider");
  await expect(slider).toBeVisible();
  const handle = slider.locator(".ant-slider-handle");
  await expect(handle).toHaveAttribute("aria-valuenow", "65");
  // 键盘右移一格 = 1%（刻度 1），交互结束即落库
  await handle.focus();
  await page.keyboard.press("ArrowRight");
  await expect(handle).toHaveAttribute("aria-valuenow", "66");
  await expect(drawer.locator("[data-focus='basic']").getByText("66%")).toBeVisible();
  // 关掉重开抽屉，值已持久化
  await page.locator(".drawer-close").click();
  await expect(page.locator(".ant-drawer-open")).toHaveCount(0);
  await page.getByText(DEMO.activeTask.name).first().click();
  await expect(
    page.locator(".ant-drawer-content [data-focus='basic'] .ant-slider-handle"),
  ).toHaveAttribute("aria-valuenow", "66");
});

test("已完成任务进度条 100% 且不可拖（#119）", async ({ page }) => {
  await login(page);
  await gotoPage(page, "/tasks");
  await page.getByText(DEMO.completedTask.name).first().click();
  const drawer = page.locator(".ant-drawer-content");
  const slider = drawer.locator("[data-focus='basic'] .ant-slider");
  await expect(slider).toBeVisible();
  await expect(slider).toHaveClass(/ant-slider-disabled/);
  await expect(slider.locator(".ant-slider-handle")).toHaveAttribute("aria-valuenow", "100");
});

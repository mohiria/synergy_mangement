import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// #119 进度刻度 1%；#175：默认查看态只显数值，点击才出现进度条，交互结束即落库。
// 种子坐标：T1.2.2 负责人王浩然、进行中 65%；T1.1.1 已完成（进度由后端派生 100，纯只读）。

test("负责人点进度数值出现进度条，拖动即保存（#119、#175）", async ({ page }) => {
  await login(page, "wanghaoran");
  await gotoPage(page, "/tasks");
  await page.getByText(DEMO.activeTask.name).first().click();
  const drawer = page.locator(".ant-drawer-content");
  await expect(drawer).toBeVisible();
  // 页脚没有「更新进度」按钮（动作收进基础信息行内）
  await expect(drawer.getByRole("button", { name: "更新进度" })).toHaveCount(0);
  const basic = drawer.locator("[data-focus='basic']");
  // #175：默认查看态没有进度条，点数值才出现
  await expect(basic.locator(".ant-slider")).toHaveCount(0);
  await basic.getByText("65%").click();
  const slider = basic.locator(".ant-slider");
  await expect(slider).toBeVisible();
  const handle = slider.locator(".ant-slider-handle");
  await expect(handle).toHaveAttribute("aria-valuenow", "65");
  // 键盘右移一格 = 1%（刻度 1），交互结束即落库并收起进度条
  await handle.focus();
  await page.keyboard.press("ArrowRight");
  await expect(basic.getByText("66%")).toBeVisible();
  // 关掉重开抽屉，值已持久化
  await page.locator(".drawer-close").click();
  await expect(page.locator(".ant-drawer-open")).toHaveCount(0);
  await page.getByText(DEMO.activeTask.name).first().click();
  await expect(
    page.locator(".ant-drawer-content [data-focus='basic']").getByText("66%"),
  ).toBeVisible();
});

test("已完成任务进度纯只读显示 100%，点数值不出现进度条（#175）", async ({ page }) => {
  await login(page);
  await gotoPage(page, "/tasks");
  await page.getByText(DEMO.completedTask.name).first().click();
  const drawer = page.locator(".ant-drawer-content");
  const basic = drawer.locator("[data-focus='basic']");
  const value = basic.getByText("100%");
  await expect(value).toBeVisible();
  await value.click();
  await expect(basic.locator(".ant-slider")).toHaveCount(0);
});

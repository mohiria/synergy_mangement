import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// #121：协作关系页所有「打开任务详情／跳转任务」在本页打开任务抽屉，不再跳转全部任务页；
// 抽屉里「在关系图谱中查看」在本页内改为关闭抽屉并聚焦，不再跳页。
// 固定坐标（cmd/seed）：任务 6 = T1.2.2「完成资金模块驱动切换与连接池调优」，KR 层可选中。
const ACTIVE = { taskId: 6, name: DEMO.activeTask.name };

test("图谱面板在本页打开抽屉，「在关系图谱中查看」只回图谱不跳页（#121）", async ({ page }) => {
  await login(page);
  await gotoPage(page, `/graph?task=${ACTIVE.taskId}`);
  await expect(page.locator(".page h1").first()).toHaveText("协作关系");
  // ?task= 落位：任务已选中，检视面板出现
  await page.getByRole("button", { name: "打开任务详情" }).click();
  const drawer = page.locator(".ant-drawer-content");
  await expect(drawer).toBeVisible();
  expect(page.url()).toContain("/graph");
  expect(page.url()).not.toContain("/tasks");
  await expect(page.locator(".ant-drawer-title")).toContainText(ACTIVE.name);
  // 抽屉内「在关系图谱中查看」（协作关系 Tab）：关抽屉、留在图谱页并保持该任务选中
  await page.locator(".task-drawer-tabs .ant-tabs-tab", { hasText: "协作关系" }).click();
  await drawer.getByText("在关系图谱中查看").first().click();
  await expect(page.locator(".ant-drawer-open")).toHaveCount(0);
  expect(page.url()).toContain("/graph");
  await expect(page.locator(".gnode.selected", { hasText: ACTIVE.name })).toBeVisible();
});

test("关系列表「跳转任务」在本页打开抽屉（#121）", async ({ page }) => {
  await login(page);
  await gotoPage(page, "/graph");
  await expect(page.locator(".page h1").first()).toHaveText("协作关系");
  await page.getByRole("button", { name: "列表" }).click();
  await page.getByRole("button", { name: "跳转任务" }).first().click();
  await expect(page.locator(".ant-drawer-content")).toBeVisible();
  expect(page.url()).toContain("/graph");
  expect(page.url()).not.toContain("/tasks");
});

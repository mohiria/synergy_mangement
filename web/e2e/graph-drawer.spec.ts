import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// #121：协作关系页「跳转任务」在本页打开任务抽屉，不再跳转全部任务页；
// 抽屉里「在关系图谱中查看」在本页内改为关闭抽屉并聚焦，不再跳页。
// #177 裁决：节点详情面板重做为任务概况只读版，底部三按钮移除。
// 固定坐标（cmd/seed）：任务 6 = T1.2.2「完成资金模块驱动切换与连接池调优」，KR 层可选中。
const ACTIVE = { taskId: 6, name: DEMO.activeTask.name };

test("节点详情为只读概况且无底部按钮；抽屉「在关系图谱中查看」只回图谱不跳页（#121、#177）", async ({ page }) => {
  await login(page);
  await gotoPage(page, `/graph?task=${ACTIVE.taskId}`);
  await expect(page.locator(".page h1").first()).toHaveText("协作关系");
  // ?task= 落位：任务已选中，检视面板为只读概况——无「打开任务详情」等底部按钮
  const inspector = page.locator(".graph-inspector");
  await expect(inspector).toBeVisible();
  await expect(inspector.getByRole("button", { name: "打开任务详情" })).toHaveCount(0);
  await expect(inspector.getByRole("button", { name: "逐层展开" })).toHaveCount(0);
  await expect(inspector.getByRole("button", { name: "查看影响路径" })).toHaveCount(0);
  // 影响路径入口移到画布操作区（#177）
  await expect(page.locator(".graph-ops-right").getByRole("button", { name: "查看影响路径" })).toBeVisible();
  // 抽屉改从关系列表「跳转任务」进入（#121 口径不变）；
  // 按「目标任务」列（第 4 列）锁定 T1.2.2 → T1.2.4 的行（种子：任务 6 → 任务 8）。
  const downstream = "补齐适配后的回归用例并跑通两轮";
  await page.getByRole("button", { name: "列表" }).click();
  const targetRow = page.locator(`tr:has(td:nth-child(4)[title="${downstream}"])`).first();
  await expect(targetRow).toBeVisible();
  await targetRow.getByRole("button", { name: "跳转任务" }).click();
  const drawer = page.locator(".ant-drawer-content");
  await expect(drawer).toBeVisible();
  expect(page.url()).toContain("/graph");
  expect(page.url()).not.toContain("/tasks");
  await expect(page.locator(".ant-drawer-title")).toContainText(downstream);
  // 抽屉内「在关系图谱中查看」（协作关系 Tab）：关抽屉、切回图谱视图并保持该任务选中
  await page.locator(".task-drawer-tabs .ant-tabs-tab", { hasText: "协作关系" }).click();
  await drawer.getByText("在关系图谱中查看").first().click();
  await expect(page.locator(".ant-drawer-open")).toHaveCount(0);
  expect(page.url()).toContain("/graph");
  await expect(page.locator(".gnode.selected", { hasText: downstream })).toBeVisible();
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

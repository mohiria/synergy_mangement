import { expect, test, type Page } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 「显示已完成」开关在四处视图的一致性（AC-45、U1）。
// 可见任务口径在代码里只有一份 isTaskVisible，这条冒烟就是防它在某一层被绕开。
//
// 固定坐标（cmd/seed）：1.2.2「完成资金模块驱动切换与连接池调优」进行中，
// 它的上游 1.1.2「输出存储过程改造清单与工作量评估」已完成——上游只在开关打开时出现。
const ACTIVE = { krId: 2, taskId: 6, name: "完成资金模块驱动切换与连接池调优" };
const COMPLETED_UPSTREAM = "输出存储过程改造清单与工作量评估";

const toggle = (page: Page) => page.locator(".toolbar .ant-switch").first();

async function openGraph(page: Page, suffix = "") {
  await gotoPage(page, `/graph${suffix}`);
  await expect(page.locator(".page h1").first()).toHaveText("协作关系");
  await expect(toggle(page)).toBeVisible();
}

async function setShowCompleted(page: Page, on: boolean) {
  const sw = toggle(page);
  const checked = (await sw.getAttribute("class"))?.includes("ant-switch-checked");
  if (checked !== on) {
    await sw.click();
    await expect(sw).toHaveClass(on ? /ant-switch-checked/ : /^((?!ant-switch-checked).)*$/);
  }
}

// 图谱节点以任务名成行；列表视图里同一个任务名出现在来源／目标列。
const graphNode = (page: Page, name: string) => page.locator(".gnode", { hasText: name });
const listCell = (page: Page, name: string) =>
  page.locator(".data-table tbody td", { hasText: name });

test.describe("显示已完成开关", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test("KR 任务关系层：开关决定已完成上游是否成节点", async ({ page }) => {
    await openGraph(page, `?kr=${ACTIVE.krId}`);
    await expect(graphNode(page, ACTIVE.name)).toBeVisible();
    await setShowCompleted(page, false);
    await expect(graphNode(page, COMPLETED_UPSTREAM)).toHaveCount(0);
    await setShowCompleted(page, true);
    await expect(graphNode(page, COMPLETED_UPSTREAM)).toBeVisible();
  });

  test("任务聚焦层：开关口径与 KR 层一致", async ({ page }) => {
    await openGraph(page, `?task=${ACTIVE.taskId}`);
    await setShowCompleted(page, true);
    // #177 裁决：双击任务节点进入聚焦层（AC-27 逐层展开），面板不再有按钮。
    await graphNode(page, ACTIVE.name).dblclick();
    await expect(graphNode(page, ACTIVE.name)).toBeVisible();
    await expect(graphNode(page, COMPLETED_UPSTREAM)).toBeVisible();
    await setShowCompleted(page, false);
    await expect(graphNode(page, COMPLETED_UPSTREAM)).toHaveCount(0);
  });

  test("全局展开层：开关口径与 KR 层一致", async ({ page }) => {
    await openGraph(page);
    await page.getByRole("button", { name: "全局展开" }).click();
    await setShowCompleted(page, false);
    await expect(graphNode(page, COMPLETED_UPSTREAM)).toHaveCount(0);
    await setShowCompleted(page, true);
    await expect(graphNode(page, COMPLETED_UPSTREAM)).toBeVisible();
  });

  // Q-10（#90）：任务全部完成的 KR 点进来是零节点画布，要有空态指出开关在哪，
  // 而不是留白让人以为没加载出来（AC-45）。KR1.1 下四项任务在种子里全部已完成。
  test("KR 任务全完成且开关关闭：给空态而不是空白画布", async ({ page }) => {
    await openGraph(page, `?kr=${DEMO.completedKrId}`);
    await setShowCompleted(page, false);
    await expect(page.locator(".graph-empty")).toContainText("该 KR 下的任务已全部完成");
    await expect(page.locator(".gnode")).toHaveCount(0);
    await setShowCompleted(page, true);
    await expect(page.locator(".graph-empty")).toHaveCount(0);
    await expect(page.locator(".gnode").first()).toBeVisible();
  });

  test("关系列表：开关口径与图谱一致（AC-46 同一份数据）", async ({ page }) => {
    await openGraph(page);
    await page.getByRole("button", { name: "列表" }).click();
    await setShowCompleted(page, false);
    await expect(listCell(page, COMPLETED_UPSTREAM)).toHaveCount(0);
    await setShowCompleted(page, true);
    await expect(listCell(page, COMPLETED_UPSTREAM).first()).toBeVisible();
  });
});

// #159（裁决）：图谱节点只保留 O／KR／任务，不再生成成员（人名）节点及其连线；
// 成员提供的输入只在任务详情面板（输入源）与关系列表表达。
// 原 Q-07「成员节点参与淡化」用例随成员节点一起废弃。
// 固定坐标（cmd/seed）：KR1.2 下「完成第三方报表工具兼容性验证」的输入由成员郑凯提供，
// 该 KR 层此前必然渲染成员节点。
test.describe("图谱无成员节点（#159）", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test("KR 层与全局展开层都不渲染成员节点", async ({ page }) => {
    await openGraph(page, "?kr=2");
    await expect(page.locator(".gnode").first()).toBeVisible();
    await expect(page.locator(".gnode-member")).toHaveCount(0);

    await page.getByRole("button", { name: "全局展开" }).click();
    await expect(page.locator(".gnode-task").first()).toBeVisible();
    await expect(page.locator(".gnode-member")).toHaveCount(0);
  });
});

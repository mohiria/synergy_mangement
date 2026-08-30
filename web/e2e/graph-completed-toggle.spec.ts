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
    // 落位后任务已选中，从检视面板进入聚焦层（AC-27 逐层展开）。
    await page.getByRole("button", { name: "逐层展开" }).click();
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

// Q-07（#90）：AC-43 的「高亮关系两端」靠淡化其余节点反衬，成员节点必须一起参与，
// 否则选中一条关系时旁边照常亮着的成员节点会把对比削掉。
// 固定坐标（cmd/seed）：KR1.2 下「完成第三方报表工具兼容性验证」的输入由成员郑凯提供；
// 「完成资金模块驱动切换与连接池调优」与该关系无关（它的邻居是 1.1.x 与 1.2.4）。
test.describe("成员节点参与淡化", () => {
  const MEMBER = "郑凯";
  const UNRELATED_TASK = "完成资金模块驱动切换与连接池调优";

  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test("选中无关任务时成员节点淡化，选中它自己的关系时回到高亮", async ({ page }) => {
    await openGraph(page, "?kr=2");
    const member = page.locator(".gnode-member", { hasText: MEMBER });
    await expect(member).toBeVisible();
    await expect(member).not.toHaveClass(/dimmed/);

    await page.locator(".gnode", { hasText: UNRELATED_TASK }).first().click();
    await expect(member).toHaveClass(/dimmed/);

    await member.click();
    await expect(member).not.toHaveClass(/dimmed/);
  });
});

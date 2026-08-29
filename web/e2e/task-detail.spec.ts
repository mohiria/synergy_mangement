import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 任务详情抽屉的结构契约（AC-31 任务概况、AC-50／AC-51 协作关系与审核）。
// 只断言顺序与存在性：内容本身由后端派生字段决定，属于集成测试的地盘。

const TAB_ORDER = ["任务概况", "协作关系", "审核", "动态与讨论"];

// 概况五块（模块 PRD §5.1）：DOM 上以 data-focus 标记，抽屉按来源落位也靠它。
const OVERVIEW_BLOCKS = ["basic", "inputs", "deliverables", "receipts", "blockers"];

test.describe("任务详情抽屉", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await gotoPage(page, "/tasks");
    await expect(page.locator(".page h1").first()).toHaveText("全部任务");
    await page.getByText(DEMO.activeTask.name).first().click();
    await expect(page.locator(".ant-drawer-content")).toBeVisible();
  });

  test("Tab 顺序固定为 任务概况／协作关系／审核／动态与讨论", async ({ page }) => {
    const labels = await page.locator(".task-drawer-tabs .ant-tabs-tab").allInnerTexts();
    expect(labels.length).toBe(TAB_ORDER.length);
    // 后三个 Tab 带计数（如「审核 2」），只比对前缀。
    labels.forEach((label, i) => expect(label.trim()).toContain(TAB_ORDER[i]));
  });

  test("任务概况按 基础信息／任务输入／交付物／接收方／当前卡点 顺序排列", async ({ page }) => {
    const blocks = page.locator(".ant-drawer-content [data-focus]");
    const order = await blocks.evaluateAll((els) =>
      els.map((el) => el.getAttribute("data-focus")),
    );
    // 协作关系 Tab 的区块也带 data-focus，只取概况面板里的那几块。
    expect(order.filter((k) => OVERVIEW_BLOCKS.includes(k!))).toEqual(OVERVIEW_BLOCKS);
  });

  test("切 Tab 只更新内容区，抽屉标题不重挂载（AC-56）", async ({ page }) => {
    const title = page.locator(".ant-drawer-title");
    const before = await title.innerText();
    await page.locator(".task-drawer-tabs .ant-tabs-tab", { hasText: "协作关系" }).click();
    await expect(page.locator(".ant-drawer-content [data-focus='relations']")).toBeVisible();
    expect(await title.innerText()).toBe(before);
  });
});

import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 任务详情抽屉的结构契约（AC-31 任务概况、AC-50／AC-51 协作关系与审核）。
// 只断言顺序与存在性：内容本身由后端派生字段决定，属于集成测试的地盘。

const TAB_ORDER = ["任务概况", "协作关系", "审核", "动态与讨论"];

// 概况五块（模块 PRD §5.1）：DOM 上以 data-focus 标记，抽屉按来源落位也靠它。
const OVERVIEW_BLOCKS = ["basic", "inputs", "deliverables", "receipts", "blockers"];

// F-07：侧边栏七项此前有两项被缩写成「成果」「报告」，与 §6 和原型不一致。
test.describe("侧边栏导航", () => {
  // #125：「OKR 管理」并入项目总览，主导航剩六项。
  const NAV_LABELS = [
    "项目总览",
    "全部任务",
    "协作关系",
    "我的工作",
    "成果与归档",
    "项目报告",
  ];

  test("六项导航文案与 §6 一致", async ({ page }) => {
    await login(page);
    await gotoPage(page, "/tasks");
    await expect(page.locator(".page h1").first()).toHaveText("全部任务");
    // 主导航七项在 <nav> 里；项目设置与项目列表在 sidebar-foot，另算。
    const labels = await page.locator(".sidebar nav .nav-row span").allInnerTexts();
    expect(labels.map((l) => l.trim())).toEqual(NAV_LABELS);
  });
});

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

  test("任务概况按 基础信息／输入源／交付物／接收方／当前卡点 顺序排列", async ({ page }) => {
    const blocks = page.locator(".ant-drawer-content [data-focus]");
    const order = await blocks.evaluateAll((els) =>
      els.map((el) => el.getAttribute("data-focus")),
    );
    // 协作关系 Tab 的区块也带 data-focus，只取概况面板里的那几块。
    // #135 后接收方配置并入基础信息，「交付物接收方」区块只在有待接收项／接收记录时出现：
    // 断言改为「出现的区块按固定顺序排列」，且基础三块必在。
    const present = order.filter((k) => OVERVIEW_BLOCKS.includes(k!));
    expect(present).toEqual(OVERVIEW_BLOCKS.filter((k) => present.includes(k)));
    for (const must of ["basic", "inputs", "deliverables"]) {
      expect(present).toContain(must);
    }
  });

  // F-06：文案漂移过一次（实现写成「尚无当前内容」），冒烟只断言区块顺序没拦住，这里补上。
  test("交付物空态文案为「尚未提交交付物」（AC-51／MW-23）", async ({ page }) => {
    const block = page.locator(".ant-drawer-content [data-focus='deliverables']");
    await expect(block).toBeVisible();
    await expect(block.getByText("尚未提交交付物").first()).toBeVisible();
    await expect(block.getByText("尚无当前内容")).toHaveCount(0);
  });

  // 裁决 G1：新增交付物项不再先填名称，入口就是选文件，项名由后端按文件名派生。
  test("新增交付物项的入口是选文件，没有名称输入框（#113）", async ({ page }) => {
    const block = page.locator(".ant-drawer-content [data-focus='deliverables']");
    await expect(block.getByPlaceholder("新增交付物项名称")).toHaveCount(0);
    await block.getByRole("button", { name: "＋ 新增交付物项" }).click();
    const modal = page.locator(".ant-modal", { hasText: "新增交付物项" });
    await expect(modal.locator("input[type='file']")).toHaveCount(1);
    await expect(modal.getByRole("button", { name: "确认上传" })).toBeDisabled();
    // antd 会在两个汉字间插空格（「取 消」），按 name 匹配不稳，取页脚的非主按钮。
    await modal.locator(".ant-modal-footer .ant-btn:not(.ant-btn-primary)").click();
    await expect(modal).toBeHidden();
  });

  test("切 Tab 只更新内容区，抽屉标题不重挂载（AC-56）", async ({ page }) => {
    const title = page.locator(".ant-drawer-title");
    const before = await title.innerText();
    await page.locator(".task-drawer-tabs .ant-tabs-tab", { hasText: "协作关系" }).click();
    await expect(page.locator(".ant-drawer-content [data-focus='relations']")).toBeVisible();
    expect(await title.innerText()).toBe(before);
  });
});

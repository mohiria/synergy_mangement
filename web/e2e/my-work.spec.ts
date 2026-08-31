import { expect, test } from "@playwright/test";
import { gotoPage, login } from "./fixtures";

// 我的工作五分组顺序与计数徽标（AC-16、MW-16；#69）。

const GROUP_ORDER = ["待我处理", "待我审批", "待我接收", "等待他人", "与我相关的卡点"];

test.describe("我的工作", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await gotoPage(page, "/my-work");
    await expect(page.locator(".page h1").first()).toHaveText("我的工作");
    await expect(page.locator(".work-board")).toBeVisible();
  });

  test("五分组顺序固定", async ({ page }) => {
    const tabs = page.locator(".work-board .ant-tabs-tab");
    await expect(tabs).toHaveCount(GROUP_ORDER.length);
    const labels = await tabs.allInnerTexts();
    labels.forEach((label, i) => expect(label.trim()).toContain(GROUP_ORDER[i]));
  });

  test("计数是独立 pill 徽标，不拼进 tab 文本", async ({ page }) => {
    const pills = page.locator(".work-board .ant-tabs-tab .pill");
    await expect(pills).toHaveCount(GROUP_ORDER.length);
    for (const text of await pills.allInnerTexts()) {
      expect(text.trim()).toMatch(/^\d+$/);
    }
    // 徽标的计数要与该组实际渲染的事项数一致（空组渲染空态而非 0 行）。
    const firstCount = Number((await pills.first().innerText()).trim());
    const items = page.locator(".ant-tabs-tabpane-active .work-item");
    await expect(items).toHaveCount(firstCount);
  });

  test("身份卡给出姓名、系统权限与当前职责", async ({ page }) => {
    const card = page.locator(".work-identity");
    await expect(card).toBeVisible();
    await expect(card.locator("b")).toHaveText("赵文琪");
    // 权限与职责都是 API 派生字段，这里只断言非空且不是枚举原文。
    const role = await card.locator("b + span").innerText();
    expect(role).toMatch(/项目管理员|项目成员|访客|项目负责人/);
    const duties = await card.locator("p").innerText();
    expect(duties).toContain("当前职责");
    expect(duties.replace("当前职责", "").trim().length).toBeGreaterThan(0);
  });
});

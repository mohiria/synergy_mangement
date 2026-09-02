import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 输入源区块（#101）：区块名、单行事实、点行进来源任务、逐级返回回到原来的 Tab。
// 断言只用结构与文案，不复刻任何规则。

test.describe("输入源", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await gotoPage(page, "/tasks");
    await page.getByRole("button", { name: DEMO.activeTask.name }).click();
    await expect(page.locator(".ant-drawer-content")).toBeVisible();
  });

  test("区块名为「输入源」，每条输入是一行不折行的事实", async ({ page }) => {
    const block = page.locator("[data-focus='inputs']");
    await expect(block.locator("h3")).toContainText("输入源");
    await expect(block.locator("h3")).not.toContainText("任务输入");

    const rows = block.locator(".input-row-main");
    await expect(rows.first()).toBeVisible();
    // 单行：文本节点不换行，且带全称 title
    const facts = await rows.evaluateAll((els) =>
      els.map((el) => ({
        nowrap: getComputedStyle(el.querySelector(".cell-text")!).whiteSpace,
        title: el.getAttribute("title"),
        text: (el.textContent || "").trim(),
      })),
    );
    expect(facts.length).toBeGreaterThan(0);
    for (const f of facts) {
      expect(f.nowrap).toBe("nowrap");
      expect(f.title).toBeTruthy();
    }
    // 来源为已有任务的行读作「编号 · 标题 · 提供人」，编号带 T 前缀（#102；#173 去关系类型）
    const fromTask = facts.find((f) => /T\d+\.\d+\.\d+/.test(f.title ?? ""));
    expect(fromTask?.title).toMatch(/^T\d+\.\d+\.\d+ · .+ · 提供人 .+$/);
  });

  test("点输入源打开来源任务详情，关闭后逐级返回并回到原来的 Tab", async ({ page }) => {
    const title = page.locator(".ant-drawer-title");

    // 先切到「协作关系」，返回时应回到这个 Tab
    await page.locator(".task-drawer-tabs .ant-tabs-tab", { hasText: "协作关系" }).click();
    await expect(page.locator("[data-focus='relations']")).toBeVisible();
    await page.locator(".task-drawer-tabs .ant-tabs-tab", { hasText: "任务概况" }).click();
    await page.locator(".task-drawer-tabs .ant-tabs-tab", { hasText: "协作关系" }).click();

    // 回到概况点第一条可点的输入源
    await page.locator(".task-drawer-tabs .ant-tabs-tab", { hasText: "任务概况" }).click();
    const link = page.locator(".input-row-main.is-link").first();
    await expect(link).toBeVisible();
    await link.click();
    await expect(title).not.toContainText(DEMO.activeTask.name);

    // 关闭按钮此时读作「返回」，回到上一个任务详情
    const back = page.locator(".drawer-close");
    await expect(back).toHaveAttribute("aria-label", "返回上一个任务详情");
    await back.click();
    await expect(title).toContainText(DEMO.activeTask.name);
    await expect(page.locator(".task-drawer-tabs .ant-tabs-tab-active")).toContainText("任务概况");

    // 栈空后关闭按钮回到「关闭」，再点一次整个抽屉关掉
    await expect(page.locator(".drawer-close")).toHaveAttribute("aria-label", "关闭任务详情");
    await page.locator(".drawer-close").click();
    await expect(page.locator(".ant-drawer-open")).toHaveCount(0);
  });
});

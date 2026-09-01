import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 我的工作页内联打开任务详情（裁决 E1 第二步、#110）：不再跳转全部任务页，
// 抽屉按卡片的 drawerTab 落位，关闭即回到我的工作。

test("卡片在本页打开抽屉，URL 不切页且按卡片指定的 Tab 落位", async ({ page }) => {
  await login(page, "wanghaoran"); // 手上有事项的成员
  await gotoPage(page, "/my-work");
  await expect(page.locator(".page h1").first()).toHaveText("我的工作");

  const card = page.locator(".work-item").first();
  await expect(card).toBeVisible();
  const title = (await card.locator("h3").innerText()).trim();
  await card.click();

  // 抽屉在本页打开，地址还停在我的工作
  await expect(page.locator(".ant-drawer-content")).toBeVisible();
  expect(page.url()).toContain(`/projects/${DEMO.projectId}/my-work`);
  expect(page.url()).not.toContain("/tasks");
  // 抽屉页头就是卡片上的那条任务
  await expect(page.locator(".ant-drawer-title")).toContainText(title.split(" ")[0]);
  // 落位的 Tab 是被激活的那个（卡片的 drawerTab 由 API 派生）
  await expect(page.locator(".task-drawer-tabs .ant-tabs-tab-active")).toHaveCount(1);

  // 关闭回到我的工作，五组还在
  await page.locator(".drawer-close").click();
  await expect(page.locator(".ant-drawer-open")).toHaveCount(0);
  await expect(page.locator(".work-tabs .ant-tabs-tab")).toHaveCount(5);
});

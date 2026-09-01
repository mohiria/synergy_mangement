import { expect, test } from "@playwright/test";
import { gotoPage, login } from "./fixtures";

// 项目设置的成员分两区（裁决 C1、#108）：成员管理（管理员＋项目成员）与查看项目（访客）。
// 跨区动作会改角色，用例自己改回去，不给后面的用例留状态。

const openMembers = async (page: import("@playwright/test").Page, username?: string) => {
  await login(page, username);
  await gotoPage(page, "/settings");
  await page.getByRole("button", { name: "成员与职责" }).click();
  await expect(page.locator(".member-zone").first()).toBeVisible();
};

test("两区分开展示，角色不混排，各自给人数与空态口径", async ({ page }) => {
  await openMembers(page);
  const zones = page.locator(".member-zone");
  await expect(zones).toHaveCount(2);
  await expect(zones.nth(0).locator("h3")).toContainText("成员管理");
  await expect(zones.nth(1).locator("h3")).toContainText("查看项目");

  // 成员管理区没有访客，查看项目区只有访客
  const working = await zones.nth(0).locator(".member-card-text span").allInnerTexts();
  expect(working.every((t) => !t.startsWith("访客"))).toBe(true);
  const viewers = await zones.nth(1).locator(".member-card-text span").allInnerTexts();
  expect(viewers.length).toBeGreaterThan(0);
  expect(viewers.every((t) => t.startsWith("访客"))).toBe(true);
});

test("「转为访客」与「转为项目成员」把人在两区之间搬动", async ({ page }) => {
  await openMembers(page);
  const zones = page.locator(".member-zone");
  const before = await zones.nth(0).locator(".member-card").count();

  // 成员管理区最后一张卡转为访客
  const card = zones.nth(0).locator(".member-card").last();
  const name = (await card.locator("b").innerText()).trim();
  await card.locator(".member-card-more").click();
  await page.getByRole("menuitem", { name: "转为访客" }).click();
  await expect(zones.nth(0).locator(".member-card")).toHaveCount(before - 1);
  await expect(zones.nth(1).getByText(name)).toBeVisible();

  // 再从查看项目区转回项目成员，恢复原状
  const moved = zones.nth(1).locator(".member-card", { hasText: name });
  await moved.locator(".member-card-more").click();
  await page.getByRole("menuitem", { name: "转为项目成员" }).click();
  await expect(zones.nth(0).locator(".member-card")).toHaveCount(before);
  await expect(zones.nth(0).getByText(name)).toBeVisible();
});

test("没有成员管理权限时两区都是只读", async ({ page }) => {
  await openMembers(page, "wanghaoran"); // 项目成员
  await expect(page.locator(".member-zone")).toHaveCount(2);
  await expect(page.locator(".member-card-more")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "邀请成员" })).toHaveCount(0);
});

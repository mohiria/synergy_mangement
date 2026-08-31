import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// #125：OKR 管理并入项目总览——导航无「OKR 管理」，总览页头按权限显示「管理 O/KR」，
// /okr 变成全页管理模式（仅结构字段），无权限者直接访问被挡回总览。

test("有权限者从总览进入管理模式，结构表无风险／任务数列（#125）", async ({ page }) => {
  await login(page); // 赵文琪：项目负责人
  await gotoPage(page, "");
  await expect(page.locator(".page h1").first()).toHaveText("项目总览");
  await page.getByRole("button", { name: "管理 O/KR" }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${DEMO.projectId}/okr$`));
  await expect(page.locator(".page h1").first()).toHaveText("管理 O/KR");
  // 左侧仍高亮「项目总览」，且导航里没有「OKR 管理」
  const labels = await page.locator(".sidebar nav .nav-row span").allInnerTexts();
  expect(labels.map((l) => l.trim())).not.toContain("OKR 管理");
  await expect(page.locator(".sidebar nav .nav-row.active")).toContainText("项目总览");
  // 只维护结构字段：无 状态／任务 列
  const heads = await page.locator(".okr-structure-table thead th").allInnerTexts();
  expect(heads.join("|")).toContain("量化标准");
  expect(heads.join("|")).not.toContain("状态");
  expect(heads.join("|")).not.toContain("任务");
  // 返回项目总览
  await page.getByRole("button", { name: "返回项目总览" }).click();
  await expect(page.locator(".page h1").first()).toHaveText("项目总览");
});

test("无权限者总览不显示入口，直接访问 /okr 被挡回总览（#125）", async ({ page }) => {
  await login(page, "wanghaoran"); // 项目成员，无 O/KR 编辑权限
  await gotoPage(page, "");
  await expect(page.locator(".page h1").first()).toHaveText("项目总览");
  await expect(page.getByRole("button", { name: "管理 O/KR" })).toHaveCount(0);
  await gotoPage(page, "/okr");
  // 权限门：不渲染管理页，回到项目总览
  await expect(page.locator(".page h1").first()).toHaveText("项目总览");
  expect(page.url()).not.toMatch(/\/okr$/);
});

import { expect, test } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 项目可见性开关（裁决 D1、#111）：项目设置里能改，改成公开后其他登录用户在项目列表里
// 看得到这个项目并标出「只读」，进项目后顶部也说明身份。用例自己把开关改回私有，不留状态。
// 断言只用结构与派生文案：谁能读、谁能写由后端判定，属集成测试的地盘。

type Page = import("@playwright/test").Page;

// 可见性字段在「项目基础信息」面板里（#219 点击即编辑）：查看态是带 aria-label 的值块，点击后换成下拉。
// 只取查看态的值块：编辑态的 antd Select 会把同一个 aria-label 同时挂在外层与内层 input 上。
const visibilityField = (page: Page) => page.locator('.settings-value[aria-label="项目可见性"]');

const openBasic = async (page: Page) => {
  await gotoPage(page, "/settings");
  await page.getByRole("button", { name: "项目基础信息" }).click();
  await expect(visibilityField(page)).toBeVisible();
};

const setVisibility = async (page: Page, label: string) => {
  // 点击进入编辑态，下拉自动展开；选中即存，没有「保存」按钮。
  await visibilityField(page).click();
  await page.locator(".ant-select-dropdown:visible .ant-select-item-option", { hasText: label }).click();
  await expect(visibilityField(page)).toContainText(label);
};

const logout = async (page: Page) => {
  await page.locator(".identity").click();
  await page.locator(".identity-popover button").last().click();
  await expect(page.getByPlaceholder("请输入用户名")).toBeVisible();
};

test.describe("项目可见性", () => {
  test("项目基础信息里有可见性开关，默认私有", async ({ page }) => {
    await login(page);
    await openBasic(page);
    await expect(visibilityField(page)).toContainText("私有项目");
  });

  test("切公开后，非成员在项目列表看到它并标出只读，进项目后顶部同样标出", async ({ page }) => {
    // 项目负责人把 1 号项目切成公开
    await login(page);
    await openBasic(page);
    await setVisibility(page, "公开项目");

    // 换一个不是本项目成员的登录用户
    await logout(page);
    await login(page, DEMO.outsider.username);

    const row = page.locator(".project-name-cell", { hasText: DEMO.projectName });
    await expect(row).toBeVisible();
    await expect(row.locator(".status-pill")).toContainText("公开项目");

    // 「归属」筛选把它归到「公开可见的」
    await page.locator(".toolbar .ant-select").last().click();
    await page
      .locator(".ant-select-dropdown:visible .ant-select-item-option", { hasText: "公开可见的" })
      .click();
    await expect(page.locator(".project-name-cell", { hasText: DEMO.projectName })).toBeVisible();

    // 进项目：顶部标明只读浏览；项目设置里只有纯文本值，没有「点击编辑」提示、控件与保存入口（写动作按派生字段隐藏）
    await row.click();
    await expect(page.locator(".breadcrumbs .status-pill")).toContainText("只读浏览");
    await gotoPage(page, "/settings");
    await page.getByRole("button", { name: "项目基础信息" }).click();
    await expect(visibilityField(page)).toContainText("公开项目");
    await expect(visibilityField(page)).not.toHaveAttribute("title", "点击编辑");
    await expect(page.locator(".settings-panel .inline-editable")).toHaveCount(0);
    await expect(page.locator(".settings-panel .ant-select, .settings-panel input")).toHaveCount(0);
    await expect(page.locator(".settings-panel-head button")).toHaveCount(0);

    // 收尾：换回项目负责人改回私有
    await logout(page);
    await login(page);
    await openBasic(page);
    await setVisibility(page, "私有项目");
  });
});

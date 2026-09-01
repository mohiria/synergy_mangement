import { expect, test } from "@playwright/test";
import { gotoPage, login } from "./fixtures";

// #116：配置了成果审核人（或签）的任务，审核人要能在抽屉「审核」Tab 看到并处理
// 通过／退回；此前动作行漏掉了 intermediate_review 状态，按钮不渲染。
// 种子坐标：任务「补齐适配后的回归用例并跑通两轮」负责人孙鹏（sunpeng），进行中，
// 且留有被退回后未清的候选内容——正好走「配置审核人 → 提交 → 审核人处理」全链路。

const TASK_NAME = "补齐适配后的回归用例并跑通两轮";
const REVIEWER = { username: "xushuai", displayName: "徐帅" };

async function openAuditTab(page: import("@playwright/test").Page) {
  await gotoPage(page, "/tasks");
  await expect(page.locator(".page h1").first()).toHaveText("全部任务");
  await page.getByText(TASK_NAME).first().click();
  await expect(page.locator(".ant-drawer-content")).toBeVisible();
  await page.locator(".task-drawer-tabs .ant-tabs-tab", { hasText: "审核" }).click();
}

test("配置审核人 → 提交完成申请 → 审核人在抽屉或签通过（#116）", async ({ page }) => {
  // 1) 负责人在基础信息栏就地多选配置成果审核人（#135：无弹窗，下拉收起即保存）
  await login(page, "sunpeng");
  await gotoPage(page, "/tasks");
  await expect(page.locator(".page h1").first()).toHaveText("全部任务");
  await page.getByText(TASK_NAME).first().click();
  const drawer = page.locator(".ant-drawer-content");
  await expect(drawer).toBeVisible();
  // #166：人员选择组件——点击触发区弹出「搜索框 + 头像行」面板，收起时一次保存。
  const reviewerRow = drawer.locator(".task-info-row", { hasText: "成果审核人" });
  await reviewerRow.locator(".pp-trigger").click();
  await page.locator(".pp-panel .pp-row", { hasText: REVIEWER.displayName }).click();
  await drawer.locator(".task-info-row", { hasText: "周期" }).click(); // 点面板外收起即保存
  await expect(
    reviewerRow.locator(".pp-trigger-text", { hasText: REVIEWER.displayName }),
  ).toBeVisible();
  // #135：审核 Tab 不再有配置行
  await page.locator(".task-drawer-tabs .ant-tabs-tab", { hasText: "审核" }).click();
  await expect(drawer.getByRole("button", { name: /调\s*整/ })).toHaveCount(0);

  // 2) 负责人提交完成申请
  await page.getByRole("button", { name: "提交完成申请" }).click();
  const submitModal = page.locator(".ant-modal-content", { hasText: "本次全部候选交付物整体提交" });
  await submitModal.getByPlaceholder("提交说明（必填）").fill("两轮回归已跑完，e2e 冒烟提交。");
  await submitModal.getByRole("button", { name: "提交完成申请" }).click();
  await expect(submitModal).toBeHidden();
  // 提交人不是审核人：能看到或签中的申请卡片，但没有处理按钮
  const pendingCard = page.locator(".audit-card.pending", { hasText: "完成申请" });
  await expect(pendingCard).toBeVisible();
  await expect(pendingCard.getByText(`或签组：${REVIEWER.displayName}`)).toBeVisible();
  await expect(pendingCard.getByRole("button", { name: "通过（进入 KR 终审）" })).toHaveCount(0);

  // 3) 审核人在抽屉里或签通过
  await page.context().clearCookies();
  await login(page, REVIEWER.username);
  await openAuditTab(page);
  const card = page.locator(".audit-card.pending", { hasText: "完成申请" });
  await expect(card.getByRole("button", { name: /退\s*回/ })).toBeVisible();
  await card.getByRole("button", { name: "通过（进入 KR 终审）" }).click();

  // 或签通过：留痕显示处理人，进入待 KR 终审，审核人自己不再有处理按钮
  await expect(
    page.locator(".audit-card", { hasText: "完成申请" }).getByText(`或签通过 · ${REVIEWER.displayName}`),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "通过（进入 KR 终审）" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "通过 / 闭环" })).toHaveCount(0);
});

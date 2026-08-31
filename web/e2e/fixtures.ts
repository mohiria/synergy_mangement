import { expect, type Page } from "@playwright/test";

// 演示数据里的固定坐标（cmd/seed/sql）：种子按相对时间重建，编号与名称是稳定的。
export const DEMO = {
  password: process.env.SEED_PASSWORD ?? "e2e-demo-pass",
  // 赵文琪：1 号项目的项目负责人，五分组与图谱都有内容。
  admin: { username: "zhaowenqi", displayName: "赵文琪" },
  projectId: 1,
  // KR1.1 下四项任务全部已完成，用来验证「显示已完成」开关。
  completedKrId: 1,
  completedTask: { code: "T1.1.1", name: "盘点三套核心库对象与不兼容项" },
  // 进行中的任务，任务详情抽屉的结构断言用它（五块齐全）。
  activeTask: { code: "T1.2.2", name: "完成资金模块驱动切换与连接池调优" },
} as const;

export async function login(page: Page, username: string = DEMO.admin.username) {
  await page.goto("/");
  await page.getByPlaceholder("请输入用户名").fill(username);
  await page.getByPlaceholder("请输入口令").fill(DEMO.password);
  // antd 会在两个汉字间插空格，按 name 匹配不稳，直接点提交按钮。
  await page.locator('button[type="submit"]').click();
  await expect(page.getByRole("heading", { name: "项目列表" })).toBeVisible();
}

// 路由后缀写错时 App 的 * 兜底会重定向到项目列表；各处断言页面标题以免静默跑错页。
export async function gotoPage(page: Page, path: string) {
  await page.goto(`/projects/${DEMO.projectId}${path}`);
}

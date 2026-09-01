import { expect, test, type Page } from "@playwright/test";
import { gotoPage, login } from "./fixtures";

// 表格导入的读取契约（#97、#105）：三种常见来源都要读出中文，
// 引号包裹、正文里偶然出现的制表符与全分隔符空行都要按 CSV 规则处理。
// 三份 fixture 内容相同，只有编码不同（见 e2e/fixtures/）。

const FILES = [
  { name: "UTF-8", path: "e2e/fixtures/import-utf8.csv" },
  { name: "UTF-8 with BOM", path: "e2e/fixtures/import-utf8-bom.csv" },
  { name: "GB18030", path: "e2e/fixtures/import-gb18030.csv" },
] as const;

// 映射步骤的预览表：第 1 行是表头、后面是数据行，取前 4 列的原文。
const PREVIEW = `
Array.from(document.querySelectorAll('.ant-modal .data-table tbody tr')).map((tr) =>
  Array.from(tr.children).map((td) => td.textContent),
)`;

async function openPreview(page: Page, file: string) {
  await gotoPage(page, "/okr");
  await page.getByRole("button", { name: "导入 O / KR 表格" }).click();
  await page.locator('input[type="file"]').setInputFiles(file);
  await page.getByRole("button", { name: /下一步：字段映射/ }).click();
  await expect(page.locator(".ant-modal .data-table tbody tr").first()).toBeVisible();
  return (await page.evaluate(PREVIEW)) as string[][];
}

for (const f of FILES) {
  test(`${f.name} 编码的 CSV 读出中文且按 CSV 规则切分`, async ({ page }) => {
    await login(page);
    const rows = await openPreview(page, f.path);

    // 首格无 BOM 残留，中文不乱码
    expect(rows[0][0]).toBe("O 标题");
    expect(rows[0][1]).toBe("KR 描述");
    // 引号包裹且内含逗号的字段是一个单元格，引号本身不进内容
    expect(rows[1][3]).toBe("不兼容对象 100% 登记，含改造方案");
    expect(rows[1][4]).toBe("2026-03-09");
    // 分隔符按表头判定：正文单元格里的制表符不切列
    expect(rows[2][1]).toBe("应用侧 SQL 与驱动完成适配\t并通过回归");
    // 只有分隔符的空行在解析阶段就被剔除：表头 + 2 条数据行
    expect(rows).toHaveLength(3);
  });
}

// xlsx 走 SheetJS 在前端解析（#105，裁决 B-a）：第一张工作表、日期转 YYYY-MM-DD、全空行剔除。
test("xlsx 由前端解析出同一种二维表", async ({ page }) => {
  await login(page);
  const rows = await openPreview(page, "e2e/fixtures/import.xlsx");
  expect(rows[0][0]).toBe("O 标题");
  expect(rows[1][1]).toBe("完成兼容性评估");
  expect(rows[1][4]).toBe("2026-03-09");
  // 全空行不进预览：表头 + 2 条数据行
  expect(rows).toHaveLength(3);
});

// 内网离线部署（#105）：解析与模板生成都在前端本地完成，整个导入流程不发外链请求。
test("导入流程不发任何外部请求，模板由代码现生成", async ({ page }) => {
  const external: string[] = [];
  page.on("request", (req) => {
    const host = new URL(req.url()).host;
    if (host !== "127.0.0.1:5173" && host !== "127.0.0.1:8080" && !req.url().startsWith("data:")) {
      external.push(req.url());
    }
  });
  await login(page);
  await gotoPage(page, "/okr");
  await page.getByRole("button", { name: "导入 O / KR 表格" }).click();
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/import.xlsx");
  await page.getByRole("button", { name: /下一步：字段映射/ }).click();
  await expect(page.locator(".ant-modal .data-table tbody tr").first()).toBeVisible();
  await page.getByRole("button", { name: "上一步" }).click();

  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "下载 xlsx 模板" }).click();
  expect((await download).suggestedFilename()).toBe("OKR导入模板.xlsx");

  expect(external).toEqual([]);
});

// O／KR 导入器只导 O 与 KR（#106，裁决 B1）：字段里没有任务列，
// 模板表头能被表头猜测原样认出，未填负责人的行走统一指派。
test("字段只有 O/KR 六项，模板表头被原样认出", async ({ page }) => {
  await login(page);
  await openPreview(page, "e2e/fixtures/import.xlsx");
  const options = await page
    .locator(".ant-modal .data-table thead .ant-select-selection-item")
    .allInnerTexts();
  expect(options).toEqual([
    "O 标题",
    "KR 描述",
    "KR 负责人",
    "量化指标",
    "周期开始",
    "周期截止",
  ]);

  // 候选里没有任务列
  await page.locator(".ant-modal .data-table thead .ant-select-selector").first().click();
  const all = await page
    .locator(".ant-select-dropdown:visible .ant-select-item-option-content")
    .allInnerTexts();
  expect(all).toEqual(["忽略此列", "O 标题", "KR 负责人", "KR 描述", "量化指标", "周期开始", "周期截止"]);
});

test("未填负责人的 KR 走统一指派，指派前不能进结构预览", async ({ page }) => {
  await login(page);
  await openPreview(page, "e2e/fixtures/import-noowner.csv");
  await page.getByRole("button", { name: /下一步：人员匹配/ }).click();

  const slot = page.getByText("有 2 条 KR 没填负责人");
  await expect(slot).toBeVisible();
  const next = page.getByRole("button", { name: /下一步：结构预览/ });
  await expect(next).toBeDisabled();

  await page.locator(".ant-modal .fact-card .ant-select-selector").first().click();
  await page.locator(".ant-select-dropdown:visible .ant-select-item-option").first().click();
  await expect(slot).toHaveCount(0);
  await expect(next).toBeEnabled();
});

// 粘贴路径（剪贴板）保持制表符优先。
test("粘贴的制表符表格仍按制表符切列", async ({ page }) => {
  await login(page);
  await gotoPage(page, "/okr");
  await page.getByRole("button", { name: "导入 O / KR 表格" }).click();
  await page.locator("textarea").fill(
    "O 标题\tKR 描述\tKR 负责人\n核心库国产化替换\t完成兼容性评估\t陈牧阳",
  );
  await page.getByRole("button", { name: /下一步：字段映射/ }).click();
  const rows = (await page.evaluate(PREVIEW)) as string[][];
  expect(rows[0]).toEqual(["O 标题", "KR 描述", "KR 负责人"]);
  expect(rows[1]).toEqual(["核心库国产化替换", "完成兼容性评估", "陈牧阳"]);
});

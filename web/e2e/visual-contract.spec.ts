import { expect, test, type Page } from "@playwright/test";
import { DEMO, gotoPage, login } from "./fixtures";

// 字号与圆角契约（docs/原型设计风格基线.md §3、§4；AC-56）。
// 这两条是整份还原度清单里唯一能精确断言的部分——数值集合封闭，不依赖布局细节。

const ALLOWED_FONT_SIZES = new Set(["12px", "14px", "16px"]);
// 通用矩形一律 4px（基线 §4 的 !important 契约），0px 表示不设圆角。
const ALLOWED_RADII = new Set(["0px", "4px", "50%"]);
// 圆角例外（基线 §4）：圆形／胶囊的形状本身带语义，只此一份名单。
// 名单要按选择器写死——放宽成「够圆就算胶囊」会把真越界的大圆角一起放过去。
const RADIUS_EXEMPT = [
  ".avatar", // 头像：50%
  ".status-pill", // 状态徽章的前置圆点
  ".ant-switch", // 开关滑块：antd 给 100px，与原型的 999px 视觉等价
  ".ant-spin-dot", // antd 载入中的圆点
  ".ant-badge-dot",
];

// 采样时跳过的元素：
// - svg 内部（图谱节点、图标）不走字号契约，尺寸由画布布局定；
// - 不可见元素（antd 预挂载的弹层、收起的抽屉）不参与视觉契约。
const SAMPLE = `
((EXEMPT) => {
  const offenders = { font: [], radius: [] };
  const describe = (el) => {
    const cls = typeof el.className === 'string' ? el.className : '';
    return el.tagName.toLowerCase() + (el.id ? '#' + el.id : '') +
      (cls ? '.' + cls.trim().split(/\\s+/).slice(0, 3).join('.') : '') +
      ' 「' + (el.textContent || '').trim().slice(0, 24) + '」';
  };
  for (const el of document.querySelectorAll('body *')) {
    if (el.closest('svg')) continue;
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) continue;
    const cs = getComputedStyle(el);
    if (cs.visibility === 'hidden' || cs.display === 'none') continue;
    // 只看自己声明了文本的元素：纯容器继承父级字号，重复计数没有意义。
    if (el.textContent && el.textContent.trim() !== '') {
      offenders.font.push([cs.fontSize, describe(el)]);
    }
    if (EXEMPT.some((sel) => el.closest(sel))) continue;
    for (const corner of ['borderTopLeftRadius', 'borderTopRightRadius',
                          'borderBottomLeftRadius', 'borderBottomRightRadius']) {
      offenders.radius.push([cs[corner], describe(el)]);
    }
  }
  return offenders;
})(EXEMPT_PLACEHOLDER)
`;

async function collect(page: Page) {
  const script = SAMPLE.replace("EXEMPT_PLACEHOLDER", JSON.stringify(RADIUS_EXEMPT));
  return page.evaluate(script) as Promise<{
    font: [string, string][];
    radius: [string, string][];
  }>;
}

function violations(pairs: [string, string][], allowed: Set<string>) {
  const bad = new Map<string, string>();
  for (const [value, where] of pairs) {
    if (!allowed.has(value) && !bad.has(`${value} @ ${where}`)) {
      bad.set(`${value} @ ${where}`, where);
    }
  }
  return [...bad.keys()];
}

// [页面标题, 路由后缀]。标题同时用作落位断言：路由写错会被 App 的 * 兜底重定向到项目列表，
// 只等「有个 h1」会静默变成在项目列表上跑，必须比对标题本身。
const PAGES: [string, string][] = [
  ["项目总览", ""],
  ["OKR 管理", "/okr"],
  ["全部任务", "/tasks"],
  ["我的工作", "/my-work"],
  ["成果与归档", "/artifacts"],
  ["协作关系", "/graph"],
  ["项目报告", "/reports"],
  ["项目设置", "/settings"],
];

test.describe("视觉契约", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  for (const [label, path] of PAGES) {
    test(`${label}：字号 ⊆ {12,14,16}，圆角 ⊆ {4px, 50%}`, async ({ page }) => {
      await gotoPage(page, path);
      // 页面主标题出现即视为首屏渲染完成；数据区各页自带骨架/空态，不再单独等。
      await expect(page.locator(".page h1").first()).toHaveText(label);
      const sample = await collect(page);
      expect(sample.font.length, "采样为空说明选择器失效，不是通过").toBeGreaterThan(20);
      expect(violations(sample.font, ALLOWED_FONT_SIZES), "字号越界").toEqual([]);
      expect(violations(sample.radius, ALLOWED_RADII), "圆角越界").toEqual([]);
    });
  }

  test(`任务详情抽屉：字号与圆角契约`, async ({ page }) => {
    await gotoPage(page, "/tasks");
    await page.getByText(DEMO.activeTask.name).first().click();
    await expect(page.locator(".ant-drawer-content")).toBeVisible();
    const sample = await collect(page);
    expect(violations(sample.font, ALLOWED_FONT_SIZES), "字号越界").toEqual([]);
    expect(violations(sample.radius, ALLOWED_RADII), "圆角越界").toEqual([]);
  });
});

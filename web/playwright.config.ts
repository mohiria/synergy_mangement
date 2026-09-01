import { defineConfig, devices } from "@playwright/test";

// Playwright 冒烟（#71）：只覆盖可精确断言的视觉与结构契约，不复刻业务规则。
//
// 前置：postgres 与 minio 已起，DATABASE_URL 指向可写的开发库，SEED_PASSWORD 已设。
// global-setup.ts 会先跑一次 `go run ./cmd/seed` 重建演示数据——**该命令清空全部业务数据**，
// 所以只在开发库上跑；已经手工准备好数据时用 E2E_SKIP_SEED=1 跳过。
//
// 本机（macOS 12）只能用 Playwright 1.47 的 chromium：更新的版本不再提供 mac12 的浏览器包。
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  globalSetup: "./e2e/global-setup.ts",
  timeout: 45_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:5173",
    // 视觉契约按基线的桌面宽度断言；窄屏另有降级规则，不在冒烟范围内。
    viewport: { width: 1600, height: 1000 },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    locale: "zh-CN",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      // 后端：契约与派生字段的唯一来源，前端只消费。
      command: "go run ./cmd/server",
      cwd: "../server",
      url: "http://127.0.0.1:8080/api/v1/healthz",
      reuseExistingServer: true,
      timeout: 120_000,
      stdout: "pipe",
      stderr: "pipe",
    },
    {
      command: "npm run dev -- --host 127.0.0.1 --port 5173",
      url: "http://127.0.0.1:5173",
      reuseExistingServer: true,
      timeout: 120_000,
    },
  ],
});

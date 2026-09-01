import { defineConfig } from "vitest/config";

// 单测只跑 src 下的纯函数（#105 的解析层与模板生成）；
// e2e/ 是 Playwright 的地盘，两套 test runner 不能互相收编。
export default defineConfig({
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});

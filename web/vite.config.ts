import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // 字符串写法默认 changeOrigin: true，会把 Host 改成 8080；后端同源校验（#192）
      // 拿浏览器 Origin 与 Host 比对，必须透传浏览器的 Host。
      "/api": { target: "http://localhost:8080", changeOrigin: false },
    },
  },
});

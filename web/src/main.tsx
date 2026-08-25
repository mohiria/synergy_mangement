import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import dayjs from "dayjs";
import "dayjs/locale/zh-cn";
import App from "./App";
import "./ui.css";

dayjs.locale("zh-cn");

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: "#5267df",
          colorInfo: "#5267df",
          colorBgLayout: "#f5f7fa",
          colorText: "#1f2937",
          colorTextSecondary: "#6b778c",
          colorBorder: "#cbd4df",
          colorBorderSecondary: "#dfe5ec",
          borderRadius: 8,
          fontFamily:
            'system-ui, "PingFang SC", "Segoe UI", "Microsoft YaHei", sans-serif',
        },
        components: {
          Table: { headerBg: "#f7f9fa", headerColor: "#53616e" },
        },
      }}
    >
      <App />
    </ConfigProvider>
  </StrictMode>,
);

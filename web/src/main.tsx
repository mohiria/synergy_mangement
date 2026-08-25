import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
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
          colorLink: "#5267df",
          colorBgLayout: "#f5f7fa",
          colorText: "#1f2937",
          colorTextSecondary: "#6b778c",
          colorBorder: "#cbd4df",
          colorBorderSecondary: "#dfe5ec",
          borderRadius: 7,
          controlHeight: 34,
          fontFamily:
            'system-ui, "PingFang SC", "Segoe UI", "Microsoft YaHei", sans-serif',
        },
        components: {
          Button: { fontWeight: 500, defaultColor: "#344152" },
          Card: { borderRadiusLG: 10 },
          Modal: { borderRadiusLG: 9, titleFontSize: 16 },
          Table: {
            headerBg: "#f7f9fb",
            headerColor: "#657184",
            headerSplitColor: "transparent",
            rowHoverBg: "#fafbff",
            borderRadiusLG: 10,
          },
        },
      }}
    >
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </ConfigProvider>
  </StrictMode>,
);

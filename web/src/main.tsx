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
          colorPrimary: "#4f62d8",
          colorInfo: "#4f62d8",
          colorLink: "#4f62d8",
          colorBgLayout: "#f3f5f9",
          colorText: "#172033",
          colorTextSecondary: "#677287",
          colorBorder: "#ccd3de",
          colorBorderSecondary: "#e2e6ed",
          borderRadius: 4,
          // 4px 圆角契约对小尺寸组件也生效：通知徽标 .ant-badge-count-sm 默认吃
          // borderRadiusSM，不设会渲染成 7px（U9）。
          borderRadiusSM: 4,
          borderRadiusXS: 4,
          controlHeight: 36,
          fontSize: 14,
          fontFamily:
            'system-ui, "PingFang SC", "Segoe UI", "Microsoft YaHei", sans-serif',
        },
        components: {
          Button: { fontWeight: 600, defaultColor: "#334056" },
          Card: { borderRadiusLG: 4 },
          Modal: { borderRadiusLG: 4, titleFontSize: 16 },
          Table: {
            headerBg: "#f7f8fa",
            headerColor: "#6b7588",
            headerSplitColor: "transparent",
            rowHoverBg: "#f8f9fc",
            borderRadiusLG: 4,
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

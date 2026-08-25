import { useCallback, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { Badge, Button, Popover } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Notification = components["schemas"]["Notification"];

// 项目内页面共用壳层：浅色侧边栏 + 顶栏（原型 index.html 结构）。
// 导航项随功能落地逐步补齐（项目总览、协作关系等见后续 ticket）。
const NAV_ITEMS = [
  { key: "overview", label: "项目总览", path: "" },
  { key: "okr", label: "OKR 管理", path: "/okr" },
  { key: "tasks", label: "全部任务", path: "/tasks" },
  { key: "graph", label: "协作关系", path: "/graph" },
  { key: "mywork", label: "我的工作", path: "/my-work" },
  { key: "settings", label: "项目设置", path: "/settings" },
];

export default function ProjectShell({
  user,
  project,
  projectId,
  pageLabel,
  onLogout,
  children,
}: {
  user: CurrentUser;
  project: Project | null;
  projectId: number;
  pageLabel: string;
  onLogout: () => void;
  children: ReactNode;
}) {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const logout = async () => {
    await client.POST("/auth/logout");
    onLogout();
  };

  // 站内通知：铃铛 + 未读角标，点击条目直达对应任务讨论（AC-36）。
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const loadNotifications = useCallback(async () => {
    const res = await client.GET("/notifications");
    if (res.data) setNotifications(res.data);
  }, []);
  useEffect(() => {
    loadNotifications();
  }, [loadNotifications, pathname]);
  const unread = notifications.filter((n) => !n.readAt).length;

  const openNotification = async (n: Notification) => {
    if (n.projectId && n.taskId) {
      navigate(`/projects/${n.projectId}/tasks?task=${n.taskId}&tab=discussion`);
    }
  };

  const markAllRead = async () => {
    await client.POST("/notifications/read-all");
    loadNotifications();
  };

  const notificationPanel = (
    <div style={{ width: 320, maxHeight: 360, overflow: "auto" }}>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginBottom: 6,
        }}
      >
        <b style={{ fontSize: 13 }}>站内通知</b>
        <Button type="link" size="small" disabled={unread === 0} onClick={markAllRead}>
          全部已读
        </Button>
      </div>
      {notifications.length === 0 && (
        <div className="muted" style={{ padding: "18px 0", textAlign: "center", fontSize: 12 }}>
          暂无通知
        </div>
      )}
      {notifications.map((n) => (
        <div
          key={n.id}
          onClick={() => openNotification(n)}
          style={{
            padding: "8px 6px",
            borderBottom: "1px solid var(--line)",
            cursor: n.taskId ? "pointer" : "default",
            opacity: n.readAt ? 0.6 : 1,
            fontSize: 13,
          }}
        >
          {n.content}
          <div className="muted" style={{ fontSize: 12 }}>
            {n.createdAt.slice(0, 16).replace("T", " ")}
          </div>
        </div>
      ))}
    </div>
  );
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark">协</span>
          <div className="brand-name">
            <b>协同管理工具</b>
            <span>O／KR／任务协同推进</span>
          </div>
        </div>
        <nav>
          <Link className="nav-row" to="/">
            ← 项目列表
          </Link>
          {NAV_ITEMS.map((item) => {
            const to = `/projects/${projectId}${item.path}`;
            return (
              <Link
                key={item.key}
                className={`nav-row ${pathname === to ? "active" : ""}`}
                to={to}
              >
                {item.label}
              </Link>
            );
          })}
        </nav>
      </aside>
      <section className="workspace">
        <header className="topbar">
          <div className="breadcrumbs">
            <Link to="/">项目列表</Link>
            <span className="sep">/</span>
            <span>{project?.name ?? "…"}</span>
            <span className="sep">/</span>
            <b>{pageLabel}</b>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Popover content={notificationPanel} trigger="click" placement="bottomRight">
              <Badge count={unread} size="small">
                <Button size="small" shape="circle" aria-label="站内通知">
                  🔔
                </Button>
              </Badge>
            </Popover>
          <div className="identity">
            <span className="avatar">{user.displayName.slice(0, 1)}</span>
            <span>{user.displayName}</span>
            <Button size="small" onClick={logout}>
              登出
            </Button>
          </div>
          </div>
        </header>
        <main className="page">{children}</main>
      </section>
    </div>
  );
}

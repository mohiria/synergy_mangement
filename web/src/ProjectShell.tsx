import type { ReactNode } from "react";
import { Link, useLocation } from "react-router-dom";
import { Button } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];

// 项目内页面共用壳层：浅色侧边栏 + 顶栏（原型 index.html 结构）。
// 导航项随功能落地逐步补齐（项目总览、协作关系等见后续 ticket）。
const NAV_ITEMS = [
  { key: "okr", label: "OKR 管理", path: "" },
  { key: "tasks", label: "全部任务", path: "/tasks" },
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
  const logout = async () => {
    await client.POST("/auth/logout");
    onLogout();
  };
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
          <div className="identity">
            <span className="avatar">{user.displayName.slice(0, 1)}</span>
            <span>{user.displayName}</span>
            <Button size="small" onClick={logout}>
              登出
            </Button>
          </div>
        </header>
        <main className="page">{children}</main>
      </section>
    </div>
  );
}

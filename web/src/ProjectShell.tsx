import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { Button, Input, Popover } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";
import NotificationBell from "./NotificationBell";
import type { IconName } from "./icons";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type ProjectStatus = components["schemas"]["ProjectStatus"];

const STATUS_LABEL: Record<ProjectStatus, string> = {
  not_started: "未开始",
  in_progress: "进行中",
  completed: "已完成",
  archived: "已归档",
};

// 项目内页面共用壳层：浅色侧边栏 + 顶栏（原型 index.html 结构）。
// 侧边栏自上而下：brand → project-switch（项目切换）→ main-nav → sidebar-foot（项目设置）。
const NAV_ITEMS: { key: string; label: string; path: string; icon: IconName }[] = [
  { key: "overview", label: "项目总览", path: "", icon: "overview" },
  { key: "okr", label: "OKR 管理", path: "/okr", icon: "target" },
  { key: "tasks", label: "全部任务", path: "/tasks", icon: "list" },
  { key: "graph", label: "协作关系", path: "/graph", icon: "graph" },
  { key: "mywork", label: "我的工作", path: "/my-work", icon: "inbox" },
  { key: "artifacts", label: "成果", path: "/artifacts", icon: "archive" },
  { key: "reports", label: "报告", path: "/reports", icon: "report" },
];

// 项目切换浮层（原型 .project-switch 的 project-menu 动作；原型只是单项目占位，此处落成真实切换）。
// 切换后保留当前子页面路径：在 /tasks 切换即进入新项目的 /tasks。
function ProjectMenu({
  projects,
  projectId,
  subPath,
  onNavigate,
}: {
  projects: Project[];
  projectId: number;
  subPath: string;
  onNavigate: (to: string) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const needle = keyword.trim().toLowerCase();
  const matched = projects.filter((p) =>
    (p.name + (p.stage ?? "")).toLowerCase().includes(needle),
  );
  return (
    <div className="project-menu">
      {projects.length > 6 && (
        <Input
          className="project-menu-search"
          size="small"
          allowClear
          autoFocus
          prefix={<Icon name="search" size={15} />}
          placeholder="搜索项目"
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
        />
      )}
      <div className="project-menu-list">
        {matched.map((p) => (
          <button
            key={p.id}
            type="button"
            className={p.id === projectId ? "project-menu-item active" : "project-menu-item"}
            onClick={() => onNavigate("/projects/" + p.id + subPath)}
          >
            <span className={"project-dot " + p.status} />
            <span className="project-menu-text">
              <b>{p.name}</b>
              <small>
                {STATUS_LABEL[p.status]}
                {p.stage ? " · " + p.stage : ""}
              </small>
            </span>
            {p.id === projectId && <Icon name="check" size={15} />}
          </button>
        ))}
        {matched.length === 0 && <div className="project-menu-empty">没有匹配的项目</div>}
      </div>
      <button type="button" className="project-menu-foot" onClick={() => onNavigate("/")}>
        <Icon name="package" size={15} />
        查看全部项目
      </button>
    </div>
  );
}

export default function ProjectShell({
  user,
  project,
  projectId,
  pageLabel,
  pageWidth,
  onLogout,
  children,
}: {
  user: CurrentUser;
  project: Project | null;
  projectId: number;
  pageLabel: string;
  // 内容区最大宽度分档（基线 §5）：默认 1480，我的工作 1240，图谱 1680。
  pageWidth?: "narrow" | "wide";
  onLogout: () => void;
  children: ReactNode;
}) {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const logout = async () => {
    await client.POST("/auth/logout");
    onLogout();
  };

  // 项目切换器候选项：当前用户可见的全部项目。
  const [projects, setProjects] = useState<Project[]>([]);
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => {
    client.GET("/projects").then(({ data }) => setProjects(data ?? []));
  }, []);
  const subPath = useMemo(() => pathname.replace(/^\/projects\/\d+/, ""), [pathname]);
  const goProject = (to: string) => {
    setMenuOpen(false);
    navigate(to);
  };


  const settingsPath = `/projects/${projectId}/settings`;
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
        <Popover
          trigger="click"
          placement="bottomLeft"
          open={menuOpen}
          onOpenChange={setMenuOpen}
          content={
            <ProjectMenu
              projects={projects}
              projectId={projectId}
              subPath={subPath}
              onNavigate={goProject}
            />
          }
        >
          <button className="project-switch" type="button" aria-label="切换项目">
            <span className={"project-dot " + (project?.status ?? "not_started")} />
            <span className="project-switch-text">
              <b>{project?.name ?? "…"}</b>
              <small>{project?.stage || (project ? STATUS_LABEL[project.status] : "加载中")}</small>
            </span>
            <Icon name="down" size={15} />
          </button>
        </Popover>
        <nav>
          {NAV_ITEMS.map((item) => {
            const to = `/projects/${projectId}${item.path}`;
            return (
              <Link key={item.key} className={`nav-row ${pathname === to ? "active" : ""}`} to={to}>
                <Icon name={item.icon} />
                <span>{item.label}</span>
              </Link>
            );
          })}
        </nav>
        <div className="sidebar-foot">
          <Link className={`nav-row ${pathname === settingsPath ? "active" : ""}`} to={settingsPath}>
            <Icon name="settings" />
            <span>项目设置</span>
          </Link>
          <Link className="nav-row" to="/">
            <Icon name="package" />
            <span>项目列表</span>
          </Link>
        </div>
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
            <NotificationBell />
            <Popover
              trigger="click"
              placement="bottomRight"
              content={
                <div className="identity-popover">
                  <div className="identity-popover-head">
                    <span className="avatar">{user.displayName.slice(0, 1)}</span>
                    <span>
                      <b>{user.displayName}</b>
                      <small>{user.username}</small>
                    </span>
                  </div>
                  <Button block onClick={logout}>
                    登出
                  </Button>
                </div>
              }
            >
              <button className="identity" type="button" aria-label="当前身份">
                <span className="avatar">{user.displayName.slice(0, 1)}</span>
                <span className="who">
                  <b>{user.displayName}</b>
                  <small>{user.username}</small>
                </span>
                <Icon name="down" size={15} />
              </button>
            </Popover>
          </div>
        </header>
        <main className={`page${pageWidth ? ` page-${pageWidth}` : ""}`}>{children}</main>
      </section>
    </div>
  );
}

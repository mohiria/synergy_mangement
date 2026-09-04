import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { Alert, Button, Input, Modal, Popover, message } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";
import NotificationBell from "./NotificationBell";
import type { IconName } from "./icons";
import { Brand } from "./Brand";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];


// 项目内页面共用壳层：浅色侧边栏 + 顶栏（原型 index.html 结构）。
// 侧边栏自上而下：brand → project-switch（项目切换）→ main-nav → sidebar-foot（项目设置）。
const NAV_ITEMS: { key: string; label: string; path: string; icon: IconName }[] = [
  { key: "overview", label: "项目总览", path: "", icon: "overview" },
  // #125：「OKR 管理」并入项目总览——/okr 是总览页头进入的全页管理模式，不再单列导航。
  { key: "tasks", label: "全部任务", path: "/tasks", icon: "list" },
  { key: "graph", label: "协作关系", path: "/graph", icon: "graph" },
  { key: "mywork", label: "我的工作", path: "/my-work", icon: "inbox" },
  { key: "artifacts", label: "成果归档", path: "/artifacts", icon: "archive" },
  { key: "reports", label: "项目报告", path: "/reports", icon: "report" },
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
      {/* #132：搜索框常驻，任意项目数下打开即聚焦可过滤。 */}
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
                {p.statusLabel}
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
        全部项目
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
  // 修改密码入口挂在身份浮层里（S3）：改完后端会吊销本人其余会话。
  const [passwordOpen, setPasswordOpen] = useState(false);
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
        <Brand />
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
              <small>{project?.stage || project?.statusLabel || "加载中"}</small>
            </span>
            <Icon name="down" size={15} />
          </button>
        </Popover>
        <nav>
          {NAV_ITEMS.map((item) => {
            const to = `/projects/${projectId}${item.path}`;
            // #125：/okr 是总览页头进入的全页管理模式，侧栏仍高亮「项目总览」。
            const active =
              pathname === to ||
              (item.key === "overview" && pathname === `/projects/${projectId}/okr`);
            return (
              <Link key={item.key} className={`nav-row ${active ? "active" : ""}`} to={to}>
                <Icon name={item.icon} />
                <span>{item.label}</span>
              </Link>
            );
          })}
        </nav>
        {/* #131：侧栏只留「项目设置」；回项目列表走切换浮层底部的「全部项目」或面包屑。 */}
        <div className="sidebar-foot">
          <Link className={`nav-row ${pathname === settingsPath ? "active" : ""}`} to={settingsPath}>
            <Icon name="settings" />
            <span>项目设置</span>
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
            {/* 隐式访客：说清「为什么这里什么都点不了」，派生字段直接消费（#111）。 */}
            {project?.implicitViewer && (
              <span className="status-pill" style={{ marginLeft: 8 }} title="公开项目：系统内任何登录用户都可只读浏览与下载，但不能编辑、审批或讨论">
                {project.visibilityLabel} · 只读浏览
              </span>
            )}
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
                  <Button block style={{ marginBottom: 8 }} onClick={() => setPasswordOpen(true)}>
                    修改密码
                  </Button>
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
        <ChangePasswordModal open={passwordOpen} onClose={() => setPasswordOpen(false)} />
      </section>
    </div>
  );
}

// ChangePasswordModal 修改本人登录密码（S3）：成功后本人其余会话立即失效，当前会话保留。
function ChangePasswordModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) {
      setCurrent("");
      setNext("");
      setConfirm("");
      setError(null);
    }
  }, [open]);

  const submit = async () => {
    if (next !== confirm) {
      setError("两次输入的新密码不一致");
      return;
    }
    setSaving(true);
    setError(null);
    const res = await client.POST("/auth/change-password", {
      body: { currentPassword: current, newPassword: next },
    });
    setSaving(false);
    if (res.response.ok) {
      message.success("密码已修改，本人其余会话已失效");
      onClose();
    } else {
      setError(res.error?.message ?? "修改失败");
    }
  };

  return (
    <Modal
      title="修改登录密码"
      open={open}
      okText="确认修改"
      cancelText="取消"
      confirmLoading={saving}
      okButtonProps={{ disabled: !current || [...next].length < 8 || [...next].length > 32 || !confirm }}
      onCancel={onClose}
      onOk={submit}
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <p className="muted" style={{ marginTop: 0 }}>
        新密码 8～32 位；修改成功后除当前浏览器外，本人其余登录会话会立即失效。
      </p>
      <Input.Password
        placeholder="当前密码"
        value={current}
        onChange={(e) => setCurrent(e.target.value)}
        style={{ marginBottom: 8 }}
      />
      <Input.Password
        placeholder="新密码（8～32 位）"
        value={next}
        maxLength={32}
        onChange={(e) => setNext(e.target.value)}
        style={{ marginBottom: 8 }}
      />
      <Input.Password
        placeholder="再次输入新密码"
        value={confirm}
        maxLength={32}
        onChange={(e) => setConfirm(e.target.value)}
      />
    </Modal>
  );
}

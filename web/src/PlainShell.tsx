import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import type { components } from "./api/schema";
import { Brand } from "./Brand";
import Icon from "./icons";
import { IdentityMenu } from "./IdentityMenu";
import NotificationBell from "./NotificationBell";

type CurrentUser = components["schemas"]["CurrentUser"];

// 项目外页面共用壳层（项目列表、系统设置）：侧栏 brand → 主导航「项目列表」→ sidebar-foot「系统设置」；
// 顶栏面包屑 + 通知 + 身份浮层。「系统设置」入口只对系统管理员显示（#201），非管理员不渲染 foot。
export default function PlainShell({
  user,
  active,
  crumb,
  subNav,
  onLogout,
  children,
}: {
  user: CurrentUser;
  active: "projects" | "system" | "me";
  crumb: ReactNode;
  // 二级导航插槽（设置类页面）：作为主导航右侧紧贴的通高第二列，顶栏与内容区在其右。
  subNav?: ReactNode;
  onLogout: () => void;
  children: ReactNode;
}) {
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Brand />
        <nav>
          <Link className={`nav-row ${active === "projects" ? "active" : ""}`} to="/">
            <Icon name="package" />
            <span>项目列表</span>
          </Link>
        </nav>
        {user.isSystemAdmin && (
          <div className="sidebar-foot">
            <Link className={`nav-row ${active === "system" ? "active" : ""}`} to="/system/users">
              <Icon name="lock" />
              <span>系统设置</span>
            </Link>
          </div>
        )}
      </aside>
      <section className="workspace">
        <header className="topbar">
          <div className="breadcrumbs">{crumb}</div>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <NotificationBell />
            <IdentityMenu user={user} onLogout={onLogout} />
          </div>
        </header>
        {/* 顶栏下方分两列：设置类页面的二级导航列（可选）+ 内容区。 */}
        <div className="workspace-body">
          {subNav && <aside className="subnav">{subNav}</aside>}
          <main className="page">{children}</main>
        </div>
      </section>
    </div>
  );
}

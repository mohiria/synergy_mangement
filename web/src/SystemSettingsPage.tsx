import { useEffect, useState } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";
import { Alert, Spin, Table } from "antd";
import dayjs from "dayjs";
import { client } from "./api/client";
import type { components } from "./api/schema";
import PlainShell from "./PlainShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type SystemUser = components["schemas"]["SystemUser"];

// 系统设置四节（模块 PRD §7）。本版只落「用户管理」只读列表（#201），其余节由后续票填入：
// 基本信息 #210／#211，通知设置 #212／#213，操作审计 #206。
const SECTIONS = [
  { key: "basic", label: "基本信息", hint: "系统名称、副标题、登录页提示语、访问地址与 logo（#210、#211）" },
  { key: "notifications", label: "通知设置", hint: "邮件通道、测试邮件与事件开关（#212、#213）" },
  { key: "users", label: "用户管理", hint: "" },
  { key: "audit", label: "操作审计", hint: "系统级写操作的审计记录（#206）" },
] as const;
type SectionKey = (typeof SECTIONS)[number]["key"];

function isSection(v: string | undefined): v is SectionKey {
  return SECTIONS.some((s) => s.key === v);
}

export default function SystemSettingsPage({ user, onLogout }: { user: CurrentUser; onLogout: () => void }) {
  const { section } = useParams();
  const navigate = useNavigate();
  const logout = async () => {
    await client.POST("/auth/logout");
    onLogout();
  };

  if (!isSection(section)) {
    return <Navigate to="/system/users" replace />;
  }
  // 非系统管理员：壳照常渲染，内容区给 403 页；侧栏本就没有入口（PlainShell 按 isSystemAdmin 隐藏）。
  if (!user.isSystemAdmin) {
    return (
      <PlainShell user={user} onLogout={logout} active="system" crumb={<b>系统设置</b>}>
        <Alert
          type="error"
          showIcon
          message="403 无权访问"
          description={
            <>
              系统设置仅系统管理员可用。<Link to="/">返回项目列表</Link>
            </>
          }
        />
      </PlainShell>
    );
  }

  return (
    <PlainShell user={user} onLogout={logout} active="system" crumb={<b>系统设置</b>}>
      <div className="page-head">
        <div>
          <h1>系统设置</h1>
          <p>整个部署的品牌、通知渠道、账号与系统级审计；与项目设置是两个层面。</p>
        </div>
      </div>
      <div className="settings-layout">
        <aside className="settings-nav">
          {SECTIONS.map((s) => (
            <button
              key={s.key}
              type="button"
              className={section === s.key ? "active" : ""}
              onClick={() => navigate(`/system/${s.key}`)}
            >
              {s.label}
            </button>
          ))}
        </aside>
        <section className="settings-panel">
          {section === "users" ? (
            <UsersSection />
          ) : (
            <PlaceholderSection section={SECTIONS.find((s) => s.key === section)!} />
          )}
        </section>
      </div>
    </PlainShell>
  );
}

function PlaceholderSection({ section }: { section: (typeof SECTIONS)[number] }) {
  return (
    <>
      <div className="settings-panel-head">
        <div>
          <h2>{section.label}</h2>
          <span className="muted">{section.hint}</span>
        </div>
      </div>
      <div className="settings-panel-body">
        <p className="muted" style={{ margin: 0 }}>
          本节尚未开放。
        </p>
      </div>
    </>
  );
}

const fmtTime = (v?: string) => (v ? dayjs(v).format("YYYY-MM-DD HH:mm") : "—");

function UsersSection() {
  const [users, setUsers] = useState<SystemUser[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    client.GET("/system/users").then(({ data, error: err }) => {
      if (data) setUsers(data);
      else setError(err?.message ?? "加载失败");
    });
  }, []);
  return (
    <>
      <div className="settings-panel-head">
        <div>
          <h2>用户管理</h2>
          <span className="muted">全部账号；建号、停用、重置密码与设撤系统管理员由后续版本提供。</span>
        </div>
      </div>
      <div className="settings-panel-body">
        {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
        {users === null && !error ? (
          <Spin />
        ) : (
          <Table<SystemUser>
            size="small"
            rowKey="id"
            pagination={false}
            dataSource={users ?? []}
            columns={[
              { title: "用户名", dataIndex: "username", key: "username" },
              { title: "显示名", dataIndex: "displayName", key: "displayName" },
              { title: "邮箱", key: "email", render: (_, u) => u.email || "—" },
              {
                title: "系统管理员",
                key: "isSystemAdmin",
                render: (_, u) => (u.isSystemAdmin ? <span className="status-pill">是</span> : "—"),
              },
              { title: "状态", key: "disabled", render: (_, u) => (u.disabled ? "已停用" : "正常") },
              { title: "创建时间", key: "createdAt", render: (_, u) => fmtTime(u.createdAt) },
              { title: "最近登录", key: "lastLoginAt", render: (_, u) => fmtTime(u.lastLoginAt) },
            ]}
          />
        )}
      </div>
    </>
  );
}

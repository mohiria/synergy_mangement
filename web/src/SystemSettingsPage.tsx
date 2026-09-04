import { useEffect, useState } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Form, Input, Modal, Spin, Table, message } from "antd";
import dayjs from "dayjs";
import { client } from "./api/client";
import type { components } from "./api/schema";
import PlainShell from "./PlainShell";
import PasswordInput from "./PasswordInput";

type CurrentUser = components["schemas"]["CurrentUser"];
type SystemUser = components["schemas"]["SystemUser"];
type CreateSystemUserRequest = components["schemas"]["CreateSystemUserRequest"];

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
  const [createOpen, setCreateOpen] = useState(false);
  const load = () => {
    client.GET("/system/users").then(({ data, error: err }) => {
      if (data) setUsers(data);
      else setError(err?.message ?? "加载失败");
    });
  };
  useEffect(load, []);
  return (
    <>
      <div className="settings-panel-head">
        <div>
          <h2>用户管理</h2>
          <span className="muted">全部账号；新建用户由管理员设初始密码，首次登录强制改密（#203）。</span>
        </div>
        <Button type="primary" size="small" onClick={() => setCreateOpen(true)}>
          新建用户
        </Button>
      </div>
      <CreateUserModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={() => {
          setCreateOpen(false);
          load();
        }}
      />
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
              {
                title: "状态",
                key: "disabled",
                render: (_, u) => (u.disabled ? "已停用" : u.mustChangePassword ? "待首次改密" : "正常"),
              },
              { title: "创建时间", key: "createdAt", render: (_, u) => fmtTime(u.createdAt) },
              { title: "最近登录", key: "lastLoginAt", render: (_, u) => fmtTime(u.lastLoginAt) },
            ]}
          />
        )}
      </div>
    </>
  );
}

// CreateUserModal 管理员建号（#203）：用户名、显示名、邮箱、初始密码；规则以后端为准，
// 这里只做必填与长度提示。初始密码框同样禁复制／剪切（共享 PasswordInput）。
function CreateUserModal({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [form] = Form.useForm<CreateSystemUserRequest>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (open) {
      form.resetFields();
      setError(null);
    }
  }, [open, form]);
  const submit = async (values: CreateSystemUserRequest) => {
    setSaving(true);
    setError(null);
    const res = await client.POST("/system/users", { body: values });
    setSaving(false);
    if (res.data) {
      message.success(`已创建用户 ${res.data.username}，首次登录须设置新密码`);
      onCreated();
    } else {
      setError(res.error?.message ?? "创建失败");
    }
  };
  return (
    <Modal
      title="新建用户"
      open={open}
      okText="创建"
      cancelText="取消"
      confirmLoading={saving}
      onCancel={onClose}
      onOk={() => form.submit()}
      destroyOnClose
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <Form form={form} layout="vertical" onFinish={submit} requiredMark={false}>
        <Form.Item
          name="username"
          label="用户名"
          extra="小写字母、数字、点、下划线、连字符，3～32 位"
          rules={[{ required: true, message: "请输入用户名" }]}
        >
          <Input autoComplete="off" maxLength={32} placeholder="如 zhangsan" />
        </Form.Item>
        <Form.Item name="displayName" label="显示名" rules={[{ required: true, message: "请输入显示名" }]}>
          <Input maxLength={50} placeholder="如 张三" />
        </Form.Item>
        <Form.Item name="email" label="邮箱" rules={[{ required: true, message: "请输入邮箱" }]}>
          <Input autoComplete="off" maxLength={254} placeholder="如 zhangsan@example.com" />
        </Form.Item>
        <Form.Item
          name="password"
          label="初始密码"
          extra="8～32 位；用户首次登录时会被要求设置新密码"
          rules={[{ required: true, message: "请输入初始密码" }]}
        >
          <PasswordInput autoComplete="new-password" maxLength={32} placeholder="8～32 位" />
        </Form.Item>
      </Form>
    </Modal>
  );
}

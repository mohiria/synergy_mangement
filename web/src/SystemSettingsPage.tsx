import { useEffect, useState } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Dropdown, Form, Input, Modal, Popconfirm, Spin, Table, message } from "antd";
import type { MenuProps } from "antd";
import Icon from "./icons";
import dayjs from "dayjs";
import { client } from "./api/client";
import type { components } from "./api/schema";
import PlainShell from "./PlainShell";
import PasswordInput from "./PasswordInput";

type CurrentUser = components["schemas"]["CurrentUser"];
type SystemUser = components["schemas"]["SystemUser"];
type CreateSystemUserRequest = components["schemas"]["CreateSystemUserRequest"];
type UpdateUserProfileRequest = components["schemas"]["UpdateUserProfileRequest"];

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
            <UsersSection me={user} />
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

function UsersSection({ me }: { me: CurrentUser }) {
  const [users, setUsers] = useState<SystemUser[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  // #205：重置密码、改资料弹窗的目标用户；设／撤系统管理员走 Popconfirm。
  const [resetTarget, setResetTarget] = useState<SystemUser | null>(null);
  const [profileTarget, setProfileTarget] = useState<SystemUser | null>(null);
  const load = () => {
    client.GET("/system/users").then(({ data, error: err }) => {
      if (data) setUsers(data);
      else setError(err?.message ?? "加载失败");
    });
  };
  useEffect(load, []);
  // #204：停用／启用。停用后不能登录、现有会话立即失效；不能停用自己（按钮禁用，服务端同样拒绝）。
  const setDisabled = async (u: SystemUser, disabled: boolean) => {
    const path = disabled ? "/system/users/{userId}/disable" : "/system/users/{userId}/enable";
    const res = await client.POST(path, { params: { path: { userId: u.id } } });
    if (res.data) {
      message.success(disabled ? `已停用 ${u.displayName}` : `已启用 ${u.displayName}`);
      load();
    } else {
      message.error(res.error?.message ?? "操作失败");
    }
  };
  const setAdmin = async (u: SystemUser, isSystemAdmin: boolean) => {
    const res = await client.PUT("/system/users/{userId}/system-admin", {
      params: { path: { userId: u.id } },
      body: { isSystemAdmin },
    });
    if (res.data) {
      message.success(isSystemAdmin ? `已将 ${u.displayName} 设为系统管理员` : `已撤销 ${u.displayName} 的系统管理员`);
      load();
    } else {
      message.error(res.error?.message ?? "操作失败");
    }
  };
  const rowMenu = (u: SystemUser): MenuProps["items"] => [
    { key: "reset", label: "重置密码", onClick: () => setResetTarget(u) },
    { key: "profile", label: "修改显示名与邮箱", onClick: () => setProfileTarget(u) },
    { type: "divider" },
    u.isSystemAdmin
      ? {
          key: "revoke",
          label: "撤销系统管理员",
          danger: true,
          disabled: u.id === me.id,
          onClick: () => setAdmin(u, false),
        }
      : { key: "grant", label: "设为系统管理员", onClick: () => setAdmin(u, true) },
  ];
  return (
    <>
      <ResetPasswordModal target={resetTarget} onClose={() => setResetTarget(null)} onDone={load} />
      <EditProfileModal target={profileTarget} onClose={() => setProfileTarget(null)} onDone={load} />
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
              {
                title: "操作",
                key: "actions",
                render: (_, u) => (
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                    {u.disabled ? (
                      <Button type="link" size="small" onClick={() => setDisabled(u, false)}>
                        启用
                      </Button>
                    ) : (
                      <Popconfirm
                        title={`停用 ${u.displayName}？`}
                        description="停用后不能登录，现有会话立即失效；历史记录照常显示其名字。"
                        okText="停用"
                        cancelText="取消"
                        onConfirm={() => setDisabled(u, true)}
                        disabled={u.id === me.id}
                      >
                        <Button type="link" size="small" danger disabled={u.id === me.id} title={u.id === me.id ? "不能停用自己" : undefined}>
                          停用
                        </Button>
                      </Popconfirm>
                    )}
                    <Dropdown trigger={["click"]} placement="bottomRight" menu={{ items: rowMenu(u) }}>
                      <button className="icon-btn" type="button" aria-label={`更多操作 ${u.username}`}>
                        <Icon name="more" size={16} />
                      </button>
                    </Dropdown>
                  </span>
                ),
              },
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

// ResetPasswordModal 管理员重置密码（#205）：新密码 8～32；该用户全部会话失效并置「须改密码」。
function ResetPasswordModal({
  target,
  onClose,
  onDone,
}: {
  target: SystemUser | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const [password, setPassword] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    setPassword("");
    setError(null);
  }, [target]);
  const len = [...password].length;
  const submit = async () => {
    if (!target) return;
    setSaving(true);
    setError(null);
    const res = await client.POST("/system/users/{userId}/reset-password", {
      params: { path: { userId: target.id } },
      body: { password },
    });
    setSaving(false);
    if (res.data) {
      message.success(`已重置 ${target.displayName} 的密码，其全部会话已失效，下次登录须改密`);
      onDone();
      onClose();
    } else {
      setError(res.error?.message ?? "重置失败");
    }
  };
  return (
    <Modal
      title={target ? `重置 ${target.displayName}（${target.username}）的密码` : "重置密码"}
      open={target !== null}
      okText="重置"
      cancelText="取消"
      confirmLoading={saving}
      okButtonProps={{ disabled: len < 8 || len > 32 }}
      onCancel={onClose}
      onOk={submit}
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <p className="muted" style={{ marginTop: 0 }}>
        新密码 8～32 位；重置后该用户全部登录会话立即失效，下次登录须设置新密码。
      </p>
      <PasswordInput
        autoComplete="new-password"
        placeholder="新密码（8～32 位）"
        value={password}
        maxLength={32}
        onChange={(e) => setPassword(e.target.value)}
      />
    </Modal>
  );
}

// EditProfileModal 管理员改显示名与邮箱（#205）；邮箱仍必填、全局唯一。
function EditProfileModal({
  target,
  onClose,
  onDone,
}: {
  target: SystemUser | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const [form] = Form.useForm<UpdateUserProfileRequest>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (target) {
      form.setFieldsValue({ displayName: target.displayName, email: target.email });
      setError(null);
    }
  }, [target, form]);
  const submit = async (values: UpdateUserProfileRequest) => {
    if (!target) return;
    setSaving(true);
    setError(null);
    const res = await client.PUT("/system/users/{userId}/profile", {
      params: { path: { userId: target.id } },
      body: values,
    });
    setSaving(false);
    if (res.data) {
      message.success("已修改");
      onDone();
      onClose();
    } else {
      setError(res.error?.message ?? "修改失败");
    }
  };
  return (
    <Modal
      title={target ? `修改 ${target.username} 的显示名与邮箱` : "修改显示名与邮箱"}
      open={target !== null}
      okText="保存"
      cancelText="取消"
      confirmLoading={saving}
      onCancel={onClose}
      onOk={() => form.submit()}
      destroyOnClose
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <Form form={form} layout="vertical" onFinish={submit} requiredMark={false}>
        <Form.Item name="displayName" label="显示名" rules={[{ required: true, message: "请输入显示名" }]}>
          <Input maxLength={50} />
        </Form.Item>
        <Form.Item name="email" label="邮箱" rules={[{ required: true, message: "请输入邮箱" }]}>
          <Input autoComplete="off" maxLength={254} />
        </Form.Item>
      </Form>
    </Modal>
  );
}


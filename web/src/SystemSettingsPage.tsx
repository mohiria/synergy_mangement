import { useEffect, useState } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Dropdown, Form, Input, InputNumber, Modal, Popconfirm, Radio, Select, Spin, Switch, Table, Upload, message } from "antd";
import type { MenuProps } from "antd";
import Icon from "./icons";
import dayjs from "dayjs";
import { client } from "./api/client";
import type { components } from "./api/schema";
import PlainShell from "./PlainShell";
import PasswordInput from "./PasswordInput";
import { logoUrl, useBranding } from "./branding";

type CurrentUser = components["schemas"]["CurrentUser"];
type SystemUser = components["schemas"]["SystemUser"];
type CreateSystemUserRequest = components["schemas"]["CreateSystemUserRequest"];
type UpdateUserProfileRequest = components["schemas"]["UpdateUserProfileRequest"];
type AuditLog = components["schemas"]["AuditLog"];
type SystemSettingsInput = components["schemas"]["SystemSettingsInput"];
type MailSettings = components["schemas"]["MailSettings"];
type MailSettingsInput = components["schemas"]["MailSettingsInput"];
type MailOutboxItem = components["schemas"]["MailOutboxItem"];
type MailNotifySwitches = components["schemas"]["MailNotifySwitches"];

// 系统设置四节（模块 PRD §7）。本版只落「用户管理」只读列表（#201），其余节由后续票填入：
// 基本信息 #210／#211，通知设置 #212／#213，操作审计 #206。
const SECTIONS = [
  { key: "basic", label: "基本信息", hint: "" },
  { key: "notifications", label: "通知设置", hint: "" },
  { key: "users", label: "用户管理", hint: "" },
  { key: "audit", label: "操作审计", hint: "" },
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
          ) : section === "audit" ? (
            <AuditSection />
          ) : section === "basic" ? (
            <BasicSection />
          ) : section === "notifications" ? (
            <NotificationsSection me={user} />
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

// AuditSection 系统级操作审计（#206）：只列无项目作用域的记录，列与项目设置审计页一致。
function AuditSection() {
  const [logs, setLogs] = useState<AuditLog[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    client.GET("/system/audit-logs").then(({ data, error: err }) => {
      if (data) setLogs(data);
      else setError(err?.message ?? "加载失败");
    });
  }, []);
  return (
    <>
      <div className="settings-panel-head">
        <div>
          <h2>操作审计</h2>
          <span className="muted">
            用户管理与系统设置的每一次成功写操作都在这里留痕：谁、什么时候、对哪个对象做了什么。
            由后端写路径统一记录，后续系统设置写接口自动覆盖；个人中心的本人改密、改资料不记。
          </span>
        </div>
      </div>
      {error && (
        <div className="settings-panel-body">
          <Alert type="error" message={error} />
        </div>
      )}
      {logs === null && !error ? (
        <div className="settings-panel-body">
          <Spin />
        </div>
      ) : logs && logs.length === 0 ? (
        <div className="empty">暂无操作记录</div>
      ) : logs ? (
        <div className="data-table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th style={{ width: 170 }}>时间</th>
                <th style={{ width: 110 }}>操作人</th>
                <th>动作</th>
                <th style={{ width: 160 }}>对象</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((a) => (
                <tr key={a.id}>
                  <td className="mono">{new Date(a.occurredAt).toLocaleString("zh-CN")}</td>
                  <td title={a.actorName ?? "系统"}>{a.actorName ?? "系统"}</td>
                  <td title={a.action}>{a.action}</td>
                  <td className="muted">{a.objectType ? `${a.objectType}${a.objectId ? ` #${a.objectId}` : ""}` : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </>
  );
}

// BasicSection 基本信息（#210）：系统名称、副标题、登录页提示语、访问地址；输入框显示字数与上限，
// 规则以后端为准；保存后刷新品牌上下文，侧栏、登录页与标签页标题同步。logo 见 #211。
const LIMITS = { systemName: 10, subtitle: 16, loginHint: 60 } as const;

function BasicSection() {
  const { branding, reload } = useBranding();
  const [logoBusy, setLogoBusy] = useState(false);
  const [logoError, setLogoError] = useState<string | null>(null);
  // #211：文件在浏览器读成 base64 走 JSON 上传（入站 /api 无 multipart）；类型与大小以后端探测为准，前端只做提示。
  const uploadLogo = async (file: File) => {
    setLogoError(null);
    if (file.size > 512 * 1024) {
      setLogoError("logo 不能超过 512KB");
      return false;
    }
    setLogoBusy(true);
    const dataBase64 = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result).split(",")[1] ?? "");
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(file);
    });
    const res = await client.POST("/system/logo", { body: { fileName: file.name, dataBase64 } });
    setLogoBusy(false);
    if (res.data) {
      message.success("logo 已更新");
      await reload();
    } else {
      // 错误就地显示而不用瞬时 toast：类型／大小被拒时用户要能看清原因。
      setLogoError(res.error?.message ?? "上传失败");
    }
    return false;
  };
  const deleteLogo = async () => {
    const res = await client.DELETE("/system/logo");
    if (res.data) {
      message.success("已恢复默认标志");
      await reload();
    } else {
      message.error(res.error?.message ?? "删除失败");
    }
  };
  const [form] = Form.useForm<SystemSettingsInput>();
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    client.GET("/system/settings").then(({ data, error: err }) => {
      if (data) {
        form.setFieldsValue({ systemName: data.systemName, subtitle: data.subtitle, loginHint: data.loginHint, baseUrl: data.baseUrl });
        setLoaded(true);
      } else {
        setError(err?.message ?? "加载失败");
      }
    });
  }, [form]);
  const submit = async (values: SystemSettingsInput) => {
    setSaving(true);
    setError(null);
    const res = await client.PUT("/system/settings", { body: values });
    setSaving(false);
    if (res.data) {
      message.success("已保存");
      form.setFieldsValue({ systemName: res.data.systemName, subtitle: res.data.subtitle, loginHint: res.data.loginHint, baseUrl: res.data.baseUrl });
      await reload();
    } else {
      setError(res.error?.message ?? "保存失败");
    }
  };
  return (
    <>
      <div className="settings-panel-head">
        <div>
          <h2>基本信息</h2>
          <span className="muted">系统名称、副标题与登录页提示语在登录页与侧栏显示；访问地址用于找回密码邮件拼链接。</span>
        </div>
      </div>
      <div className="settings-panel-body">
        {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
        {loaded ? (
          <Form form={form} layout="vertical" onFinish={submit} requiredMark={false} style={{ maxWidth: 520 }}>
            <Form.Item name="systemName" label="系统名称" rules={[{ required: true, message: "请输入系统名称" }]}>
              <Input maxLength={LIMITS.systemName} showCount />
            </Form.Item>
            <Form.Item name="subtitle" label="副标题（可空）">
              <Input maxLength={LIMITS.subtitle} showCount />
            </Form.Item>
            <Form.Item name="loginHint" label="登录页提示语（可空）">
              <Input maxLength={LIMITS.loginHint} showCount />
            </Form.Item>
            <Form.Item name="baseUrl" label="访问地址（可空）" extra="http:// 或 https:// 开头的完整地址，如 http://203.0.113.10">
              <Input maxLength={254} placeholder="http://" />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={saving}>
              保存
            </Button>
            <div className="property" style={{ marginTop: 20, alignItems: "flex-start" }} data-testid="logo-block">
              <label>logo</label>
              <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                <span className="brand-mark" style={{ width: 40, height: 40 }}>
                  {logoUrl(branding) ? <img src={logoUrl(branding)!} alt="" /> : branding.systemName.slice(0, 1)}
                </span>
                <Upload accept="image/png,image/jpeg,image/webp" showUploadList={false} beforeUpload={(f) => uploadLogo(f)}>
                  <Button size="small" loading={logoBusy}>
                    上传 logo
                  </Button>
                </Upload>
                {logoUrl(branding) && (
                  <Popconfirm title="删除 logo，恢复系统名称首字？" okText="删除" cancelText="取消" onConfirm={deleteLogo}>
                    <Button size="small" danger>
                      删除 logo
                    </Button>
                  </Popconfirm>
                )}
                <span className="muted" style={{ fontSize: 12 }}>
                  仅 PNG／JPG／WebP，≤512KB，建议正方形；非正方形居中裁切；兼作浏览器标签页图标。不收 SVG。
                </span>
              </div>
            </div>
            {logoError && <Alert type="error" message={logoError} style={{ marginTop: 8, maxWidth: 520 }} data-testid="logo-error" />}
          </Form>
        ) : !error ? (
          <Spin />
        ) : null}
      </div>
    </>
  );
}

// NotificationsSection 通知设置（#212）：邮件通道配置（密码只显示「已设置」、留空保持原值）、
// 测试邮件两个选项、最近发送记录。事件开关见 #213。
function NotificationsSection({ me }: { me: CurrentUser }) {
  const [form] = Form.useForm<MailSettingsInput>();
  const [settings, setSettings] = useState<MailSettings | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [outbox, setOutbox] = useState<MailOutboxItem[]>([]);
  const [target, setTarget] = useState<"me" | "custom">("me");
  const [address, setAddress] = useState("");
  const [sending, setSending] = useState(false);
  // #213：系统级总开关 + 五个事件开关；改一个就保存。
  const saveNotify = async (next: MailNotifySwitches) => {
    const res = await client.PUT("/system/mail-notify", { body: next });
    if (res.data) {
      setSettings(res.data);
    } else {
      message.error(res.error?.message ?? "保存失败");
    }
  };
  const loadOutbox = () => {
    client.GET("/system/mail-outbox").then(({ data }) => {
      if (data) setOutbox(data);
    });
  };
  useEffect(() => {
    client.GET("/system/mail-settings").then(({ data, error: err }) => {
      if (data) {
        setSettings(data);
        form.setFieldsValue({
          host: data.host, port: data.port, encryption: data.encryption, username: data.username,
          fromName: data.fromName, fromAddress: data.fromAddress, password: "",
        });
      } else {
        setError(err?.message ?? "加载失败");
      }
    });
    loadOutbox();
  }, [form]);
  const submit = async (values: MailSettingsInput) => {
    setSaving(true);
    setError(null);
    const res = await client.PUT("/system/mail-settings", { body: { ...values, password: values.password || undefined } });
    setSaving(false);
    if (res.data) {
      message.success("邮件通道已保存");
      setSettings(res.data);
      form.setFieldValue("password", "");
    } else {
      setError(res.error?.message ?? "保存失败");
    }
  };
  const sendTest = async () => {
    setSending(true);
    const res = await client.POST("/system/mail-settings/test", {
      body: target === "me" ? { target: "me" } : { target: "custom", address },
    });
    setSending(false);
    if (res.data) {
      message.success(`测试邮件已加入发送队列：${res.data.toAddress}`);
      loadOutbox();
    } else {
      message.error(res.error?.message ?? "发送失败");
    }
  };
  return (
    <>
      <div className="settings-panel-head">
        <div>
          <h2>通知设置</h2>
          <span className="muted">
            邮件通道：SMTP 密码用应用密钥加密后落库，保存后不回显；所有邮件先入队由后台异步发送，失败自动重试三次。
          </span>
        </div>
      </div>
      <div className="settings-panel-body">
        {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
        {settings ? (
          <>
            <Form form={form} layout="vertical" onFinish={submit} requiredMark={false} style={{ maxWidth: 560 }}>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 140px 160px", gap: 12 }}>
                <Form.Item name="host" label="SMTP 主机" rules={[{ required: true, message: "请输入主机" }]}>
                  <Input placeholder="smtp.example.com" />
                </Form.Item>
                <Form.Item name="port" label="端口" rules={[{ required: true, message: "请输入端口" }]}>
                  <InputNumber min={1} max={65535} style={{ width: "100%" }} />
                </Form.Item>
                <Form.Item name="encryption" label="加密方式">
                  <Select
                    options={[
                      { value: "none", label: "无" },
                      { value: "starttls", label: "STARTTLS" },
                      { value: "ssl", label: "SSL" },
                    ]}
                  />
                </Form.Item>
              </div>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
                <Form.Item name="username" label="账号（可空）">
                  <Input autoComplete="off" />
                </Form.Item>
                <Form.Item name="password" label={settings.passwordSet ? "密码（已设置，留空保持不变）" : "密码"}>
                  <PasswordInput autoComplete="new-password" placeholder={settings.passwordSet ? "已设置" : ""} />
                </Form.Item>
                <Form.Item name="fromName" label="发件人显示名（可空）">
                  <Input maxLength={50} />
                </Form.Item>
                <Form.Item name="fromAddress" label="发件人地址" rules={[{ required: true, message: "请输入发件人地址" }]}>
                  <Input autoComplete="off" />
                </Form.Item>
              </div>
              <Button type="primary" htmlType="submit" loading={saving}>
                保存通道
              </Button>
              <span className="muted" style={{ marginLeft: 12, fontSize: 12 }}>
                {settings.configured ? "通道已配置" : "通道未配置：找回密码与邮件通知不可用"}
              </span>
            </Form>

            <div className="property" style={{ marginTop: 20, alignItems: "flex-start" }} data-testid="test-mail">
              <label>测试邮件</label>
              <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                <Radio.Group value={target} onChange={(e) => setTarget(e.target.value)}>
                  <Radio value="me">发到我绑定的邮箱（{me.email}）</Radio>
                  <Radio value="custom">发到其他邮箱</Radio>
                </Radio.Group>
                <div style={{ display: "flex", gap: 8 }}>
                  {target === "custom" && (
                    <Input style={{ width: 280 }} placeholder="收件地址" value={address} onChange={(e) => setAddress(e.target.value)} />
                  )}
                  <Button size="small" loading={sending} disabled={!settings.configured || (target === "custom" && !address)} onClick={sendTest}>
                    发送测试邮件
                  </Button>
                </div>
              </div>
            </div>

            <h3 style={{ fontSize: 14, margin: "20px 0 8px" }}>邮件通知</h3>
            <div data-testid="notify-switches" style={{ display: "grid", gap: 8, maxWidth: 560 }}>
              <label style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <Switch
                  checked={settings.notify.enabled}
                  onChange={(v) => saveNotify({ ...settings.notify, enabled: v })}
                  aria-label="启用邮件通知"
                />
                <b>启用邮件通知</b>
                <span className="muted" style={{ fontSize: 12 }}>站内通知产生时同步发邮件；找回密码邮件不受此控制</span>
              </label>
              {settings.notify.events.map((ev) => (
                <label key={ev.kind} style={{ display: "flex", alignItems: "center", gap: 10, paddingLeft: 12 }}>
                  <Switch
                    size="small"
                    checked={ev.enabled}
                    disabled={!settings.notify.enabled}
                    aria-label={ev.label}
                    onChange={(v) =>
                      saveNotify({
                        ...settings.notify,
                        events: settings.notify.events.map((x) => (x.kind === ev.kind ? { ...x, enabled: v } : x)),
                      })
                    }
                  />
                  <span>{ev.label}</span>
                </label>
              ))}
            </div>

            <h3 style={{ fontSize: 14, margin: "20px 0 8px" }}>最近发送记录</h3>
            <Table<MailOutboxItem>
              size="small"
              rowKey="id"
              pagination={false}
              dataSource={outbox}
              locale={{ emptyText: "暂无发送记录" }}
              columns={[
                { title: "收件人", dataIndex: "toAddress", key: "toAddress" },
                { title: "事件", dataIndex: "eventLabel", key: "event" },
                { title: "状态", key: "status", render: (_, x) => `${x.statusLabel}${x.attempts ? `（${x.attempts} 次）` : ""}` },
                { title: "失败原因", key: "lastError", render: (_, x) => x.lastError || "—", ellipsis: true },
                { title: "时间", key: "createdAt", render: (_, x) => fmtTime(x.sentAt ?? x.createdAt) },
              ]}
            />
            <Button size="small" type="link" onClick={loadOutbox} style={{ paddingLeft: 0 }}>
              刷新记录
            </Button>
          </>
        ) : !error ? (
          <Spin />
        ) : null}
      </div>
    </>
  );
}


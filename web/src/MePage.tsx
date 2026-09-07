import { useEffect, useState } from "react";
import { Navigate, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Form, Input, Popconfirm, Switch, Table, message } from "antd";
import dayjs from "dayjs";
import { client } from "./api/client";
import type { components } from "./api/schema";
import PlainShell from "./PlainShell";
import PasswordInput from "./PasswordInput";

type CurrentUser = components["schemas"]["CurrentUser"];
type UpdateUserProfileRequest = components["schemas"]["UpdateUserProfileRequest"];
type SessionInfo = components["schemas"]["SessionInfo"];
type MailPreferences = components["schemas"]["MailPreferences"];

// 个人中心（模块 PRD §6；#207）：/me，不挂项目，两套壳都能进；左侧分节导航复用项目设置的形态。
// 基本信息、修改密码（#207）、登录安全（#208）、通知偏好（#213）。
const SECTIONS = [
  { key: "profile", label: "基本信息" },
  { key: "password", label: "修改密码" },
  { key: "security", label: "登录安全" },
  { key: "notifications", label: "通知偏好" },
] as const;
type SectionKey = (typeof SECTIONS)[number]["key"];

function isSection(v: string | undefined): v is SectionKey {
  return SECTIONS.some((s) => s.key === v);
}

export default function MePage({
  user,
  onUserChange,
  onLogout,
}: {
  user: CurrentUser;
  // 改显示名后侧栏／顶栏即时更新：当前用户由 App 持有，这里回传最新值。
  onUserChange: (u: CurrentUser) => void;
  onLogout: () => void;
}) {
  const { section } = useParams();
  const navigate = useNavigate();
  const logout = async () => {
    await client.POST("/auth/logout");
    onLogout();
  };
  if (!isSection(section)) {
    return <Navigate to="/me/profile" replace />;
  }
  return (
    <PlainShell user={user} onLogout={logout} active="me" crumb={<b>个人中心</b>}>
      <div className="page-head">
        <div>
          <h1>个人中心</h1>
          <p>本人的资料、密码与登录安全；这里的修改不进操作审计。</p>
        </div>
      </div>
      <div className="settings-layout">
        <aside className="settings-nav">
          {SECTIONS.map((s) => (
            <button
              key={s.key}
              type="button"
              className={section === s.key ? "active" : ""}
              onClick={() => navigate(`/me/${s.key}`)}
            >
              {s.label}
            </button>
          ))}
        </aside>
        <section className="settings-panel">
          {section === "profile" ? (
            <ProfileSection user={user} onUserChange={onUserChange} />
          ) : section === "security" ? (
            <SecuritySection user={user} />
          ) : section === "notifications" ? (
            <NotificationPrefsSection />
          ) : (
            <PasswordSection />
          )}
        </section>
      </div>
    </PlainShell>
  );
}

function ProfileSection({ user, onUserChange }: { user: CurrentUser; onUserChange: (u: CurrentUser) => void }) {
  const [form] = Form.useForm<UpdateUserProfileRequest>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    form.setFieldsValue({ displayName: user.displayName, email: user.email });
  }, [user, form]);
  const submit = async (values: UpdateUserProfileRequest) => {
    setSaving(true);
    setError(null);
    const res = await client.PUT("/me/profile", { body: values });
    setSaving(false);
    if (res.data) {
      message.success("已保存");
      onUserChange(res.data);
    } else {
      setError(res.error?.message ?? "保存失败");
    }
  };
  return (
    <>
      <div className="settings-panel-head">
        <div>
          <h2>基本信息</h2>
          <span className="muted">用户名只读；显示名与邮箱可改，邮箱必填且全局唯一。</span>
        </div>
      </div>
      <div className="settings-panel-body">
        {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
        <Form form={form} layout="vertical" onFinish={submit} requiredMark={false} style={{ maxWidth: 420 }}>
          <Form.Item label="用户名">
            <Input value={user.username} disabled />
          </Form.Item>
          <Form.Item name="displayName" label="显示名" rules={[{ required: true, message: "请输入显示名" }]}>
            <Input maxLength={50} />
          </Form.Item>
          <Form.Item name="email" label="邮箱" rules={[{ required: true, message: "请输入邮箱" }]}>
            <Input autoComplete="email" maxLength={254} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={saving}>
            保存
          </Button>
        </Form>
      </div>
    </>
  );
}

// PasswordSection 修改密码（原顶栏弹窗内容搬入本节，S3）：成功后本人其余会话立即失效，当前会话保留。
function PasswordSection() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const len = [...next].length;
  const submit = async () => {
    if (next !== confirm) {
      setError("两次输入的新密码不一致");
      return;
    }
    setSaving(true);
    setError(null);
    const res = await client.POST("/auth/change-password", { body: { currentPassword: current, newPassword: next } });
    setSaving(false);
    if (res.response.ok) {
      message.success("密码已修改，本人其余会话已失效");
      setCurrent("");
      setNext("");
      setConfirm("");
    } else {
      setError(res.error?.message ?? "修改失败");
    }
  };
  return (
    <>
      <div className="settings-panel-head">
        <div>
          <h2>修改密码</h2>
          <span className="muted">新密码 8～32 位；修改成功后除当前浏览器外，本人其余登录会话会立即失效。</span>
        </div>
      </div>
      <div className="settings-panel-body" style={{ maxWidth: 420 }}>
        {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
        <PasswordInput
          autoComplete="current-password"
          placeholder="当前密码"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          style={{ marginBottom: 8 }}
        />
        <PasswordInput
          autoComplete="new-password"
          placeholder="新密码（8～32 位）"
          value={next}
          maxLength={32}
          onChange={(e) => setNext(e.target.value)}
          style={{ marginBottom: 8 }}
        />
        <PasswordInput
          autoComplete="new-password"
          placeholder="再次输入新密码"
          value={confirm}
          maxLength={32}
          onChange={(e) => setConfirm(e.target.value)}
          onPressEnter={submit}
          style={{ marginBottom: 12 }}
        />
        <Button type="primary" loading={saving} disabled={!current || len < 8 || len > 32 || !confirm} onClick={submit}>
          确认修改
        </Button>
      </div>
    </>
  );
}

const fmtTime = (v?: string) => (v ? dayjs(v).format("YYYY-MM-DD HH:mm") : "—");

// SecuritySection 登录安全（#208）：最近登录时间、活跃会话列表（当前会话标识）、一键退出其他设备。
function SecuritySection({ user }: { user: CurrentUser }) {
  const [sessions, setSessions] = useState<SessionInfo[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const load = () => {
    client.GET("/me/sessions").then(({ data, error: err }) => {
      if (data) setSessions(data);
      else setError(err?.message ?? "加载失败");
    });
  };
  useEffect(load, []);
  const logoutOthers = async () => {
    const res = await client.POST("/me/sessions/logout-others");
    if (res.response.ok) {
      message.success("已退出其他设备");
      load();
    } else {
      message.error(res.error?.message ?? "操作失败");
    }
  };
  const others = (sessions ?? []).filter((x) => !x.current).length;
  return (
    <>
      <div className="settings-panel-head">
        <div>
          <h2>登录安全</h2>
          <span className="muted">最近登录：{fmtTime(user.lastLoginAt)}。最近活动时间随会话续期更新，最多滞后 1 小时。</span>
        </div>
        <Popconfirm
          title="退出其他设备？"
          description="除当前浏览器外，本人其余登录会话立即失效。"
          okText="退出"
          cancelText="取消"
          onConfirm={logoutOthers}
          disabled={others === 0}
        >
          <Button size="small" danger disabled={others === 0}>
            退出其他设备{others > 0 ? `（${others}）` : ""}
          </Button>
        </Popconfirm>
      </div>
      <div className="settings-panel-body">
        {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
        <Table<SessionInfo>
          size="small"
          rowKey={(x) => `${x.createdAt}-${x.lastActiveAt}`}
          pagination={false}
          loading={sessions === null && !error}
          dataSource={sessions ?? []}
          columns={[
            { title: "登录时间", key: "createdAt", render: (_, x) => fmtTime(x.createdAt) },
            { title: "最近活动", key: "lastActiveAt", render: (_, x) => fmtTime(x.lastActiveAt) },
            { title: "过期时间", key: "expiresAt", render: (_, x) => fmtTime(x.expiresAt) },
            {
              title: "",
              key: "current",
              render: (_, x) => (x.current ? <span className="status-pill">当前会话</span> : null),
            },
          ]}
        />
      </div>
    </>
  );
}

// NotificationPrefsSection 通知偏好（#213）：本人总开关 + 五个事件开关，默认全开；
// 系统级已关的事件置灰不可用并注明「系统未启用」。只影响是否同步邮件，不影响站内通知。
function NotificationPrefsSection() {
  const [prefs, setPrefs] = useState<MailPreferences | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    client.GET("/me/mail-preferences").then(({ data, error: err }) => {
      if (data) setPrefs(data);
      else setError(err?.message ?? "加载失败");
    });
  }, []);
  const save = async (next: MailPreferences) => {
    const res = await client.PUT("/me/mail-preferences", { body: { enabled: next.enabled, events: next.events } });
    if (res.data) setPrefs(res.data);
    else message.error(res.error?.message ?? "保存失败");
  };
  return (
    <>
      <div className="settings-panel-head">
        <div>
          <h2>通知偏好</h2>
          <span className="muted">站内通知产生时是否同步发到我的邮箱；不影响站内通知本身。</span>
        </div>
      </div>
      <div className="settings-panel-body">
        {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
        {prefs && (
          <div data-testid="notify-prefs" style={{ display: "grid", gap: 8, maxWidth: 560 }}>
            {!prefs.systemEnabled && <Alert type="info" showIcon message="系统未启用邮件通知，以下偏好暂不生效" />}
            <label style={{ display: "flex", alignItems: "center", gap: 10 }}>
              <Switch
                checked={prefs.enabled}
                disabled={!prefs.systemEnabled}
                aria-label="邮件通知"
                onChange={(v) => save({ ...prefs, enabled: v })}
              />
              <b>邮件通知</b>
            </label>
            {prefs.events.map((ev) => {
              const systemOff = ev.systemEnabled === false;
              return (
                <label key={ev.kind} style={{ display: "flex", alignItems: "center", gap: 10, paddingLeft: 12 }}>
                  <Switch
                    size="small"
                    checked={ev.enabled}
                    disabled={systemOff || !prefs.enabled}
                    aria-label={ev.label}
                    onChange={(v) => save({ ...prefs, events: prefs.events.map((x) => (x.kind === ev.kind ? { ...x, enabled: v } : x)) })}
                  />
                  <span className={systemOff ? "muted" : undefined}>{ev.label}</span>
                  {systemOff && (
                    <span className="muted" style={{ fontSize: 12 }}>
                      系统未启用
                    </span>
                  )}
                </label>
              );
            })}
          </div>
        )}
      </div>
    </>
  );
}


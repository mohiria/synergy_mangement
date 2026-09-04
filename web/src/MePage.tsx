import { useEffect, useState } from "react";
import { Navigate, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Form, Input, message } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import PlainShell from "./PlainShell";
import PasswordInput from "./PasswordInput";

type CurrentUser = components["schemas"]["CurrentUser"];
type UpdateUserProfileRequest = components["schemas"]["UpdateUserProfileRequest"];

// 个人中心（模块 PRD §6；#207）：/me，不挂项目，两套壳都能进；左侧分节导航复用项目设置的形态。
// 本版落基本信息与修改密码两节；登录安全 #208、通知偏好 #213 后续填入。
const SECTIONS = [
  { key: "profile", label: "基本信息" },
  { key: "password", label: "修改密码" },
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

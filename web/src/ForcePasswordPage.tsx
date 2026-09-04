import { useState } from "react";
import { Alert, Button } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import { Brand } from "./Brand";
import PasswordInput from "./PasswordInput";

type CurrentUser = components["schemas"]["CurrentUser"];

// 首次登录强制改密（#203，模块 PRD §5.3）：「须改密码」为真时整页只有本页，任何路由都回到这里；
// 不要求旧密码（初始密码是管理员设的，用户刚用它登录过）。改完重新读当前用户进入系统。
export default function ForcePasswordPage({
  user,
  onDone,
  onLogout,
}: {
  user: CurrentUser;
  onDone: (u: CurrentUser) => void;
  onLogout: () => void;
}) {
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
    const res = await client.POST("/auth/change-password", { body: { newPassword: next } });
    if (!res.response.ok) {
      setSaving(false);
      setError(res.error?.message ?? "修改失败");
      return;
    }
    const me = await client.GET("/auth/me");
    setSaving(false);
    if (me.data) onDone(me.data);
  };
  const logout = async () => {
    await client.POST("/auth/logout");
    onLogout();
  };

  return (
    <div className="login-shell">
      <div className="login-panel">
        <Brand className="login-brand" />
        <div className="login-card">
          <div className="login-card-head">
            <h1>首次登录请设置新密码</h1>
            <p>
              {user.displayName}（{user.username}），当前使用的是管理员分配的初始密码，设置新密码后才能进入系统。
            </p>
          </div>
          {error && <Alert type="error" message={error} style={{ marginBottom: 14 }} />}
          <p className="muted" style={{ marginTop: 0 }}>
            新密码 8～32 位，不能与初始密码相同。
          </p>
          <PasswordInput
            autoFocus
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
            style={{ marginBottom: 16 }}
          />
          <Button type="primary" block loading={saving} disabled={len < 8 || len > 32 || !confirm} onClick={submit}>
            设置新密码并进入
          </Button>
          <Button type="link" block onClick={logout} style={{ marginTop: 8 }}>
            退出登录
          </Button>
        </div>
      </div>
    </div>
  );
}

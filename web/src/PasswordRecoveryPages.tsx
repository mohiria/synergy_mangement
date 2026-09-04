import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Alert, Button, Input } from "antd";
import { client } from "./api/client";
import { Brand } from "./Brand";
import PasswordInput from "./PasswordInput";

// 找回密码两页（模块 PRD §4；#214）：都在未登录态渲染，复用登录页的壳。
// 请求页：输入用户名或邮箱，无论结果如何都显示统一文案；重置页：从链接取 token 设置新密码。

export function ForgotPasswordPage() {
  const [identifier, setIdentifier] = useState("");
  const [done, setDone] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const submit = async () => {
    setSubmitting(true);
    setError(null);
    const { data, error: err, response } = await client.POST("/auth/password-reset/request", { body: { identifier } });
    setSubmitting(false);
    if (data) {
      setDone(data.message);
      return;
    }
    if (response.status === 429) {
      const secs = (err as { retryAfterSeconds?: number } | undefined)?.retryAfterSeconds ?? 0;
      setError(`尝试过多，请 ${secs} 秒后再试`);
      return;
    }
    setError(err?.message ?? "请求失败");
  };
  return (
    <div className="login-shell">
      <div className="login-panel">
        <Brand className="login-brand" />
        <div className="login-card">
          <div className="login-card-head">
            <h1>找回密码</h1>
            <p>输入用户名或邮箱，我们会把重置链接发到你绑定的邮箱；链接 30 分钟内有效、只能用一次。</p>
          </div>
          {error && <Alert type="error" message={error} style={{ marginBottom: 14 }} />}
          {done ? (
            <Alert type="success" showIcon message={done} description="请查收邮件；若一直未收到，请联系管理员重置。" />
          ) : (
            <>
              <Input
                autoFocus
                placeholder="用户名或邮箱"
                value={identifier}
                onChange={(e) => setIdentifier(e.target.value)}
                onPressEnter={submit}
                style={{ marginBottom: 16 }}
              />
              <Button type="primary" block loading={submitting} disabled={!identifier.trim()} onClick={submit}>
                发送重置邮件
              </Button>
            </>
          )}
        </div>
        <p className="login-foot">
          <Link to="/">返回登录</Link>
        </p>
      </div>
    </div>
  );
}

export function ResetPasswordPage() {
  const [params] = useSearchParams();
  const token = params.get("token") ?? "";
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [saving, setSaving] = useState(false);
  const len = [...next].length;
  const submit = async () => {
    if (next !== confirm) {
      setError("两次输入的新密码不一致");
      return;
    }
    setSaving(true);
    setError(null);
    const res = await client.POST("/auth/password-reset/confirm", { body: { token, password: next } });
    setSaving(false);
    if (res.response.ok) setDone(true);
    else setError(res.error?.message ?? "重置失败");
  };
  return (
    <div className="login-shell">
      <div className="login-panel">
        <Brand className="login-brand" />
        <div className="login-card">
          <div className="login-card-head">
            <h1>设置新密码</h1>
            <p>新密码 8～32 位；设置成功后原有登录会话全部失效。</p>
          </div>
          {error && <Alert type="error" message={error} style={{ marginBottom: 14 }} />}
          {done ? (
            <Alert
              type="success"
              showIcon
              message="密码已重置"
              description={
                <>
                  请用新密码<Link to="/">登录</Link>。
                </>
              }
            />
          ) : !token ? (
            <Alert type="error" message="链接无效或已过期" />
          ) : (
            <>
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
                设置新密码
              </Button>
            </>
          )}
        </div>
        <p className="login-foot">
          <Link to="/">返回登录</Link>
        </p>
      </div>
    </div>
  );
}

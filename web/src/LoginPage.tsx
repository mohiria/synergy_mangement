import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Alert, Button, Form, Input } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";
import { Brand } from "./Brand";
import PasswordInput from "./PasswordInput";
import { useBranding } from "./branding";

type CurrentUser = components["schemas"]["CurrentUser"];

export default function LoginPage({ onLogin }: { onLogin: (u: CurrentUser) => void }) {
  const { branding } = useBranding();
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  // #209：被限速（429）时按接口返回的剩余秒数倒计时，归零后可再试。
  const [retryAfter, setRetryAfter] = useState(0);
  useEffect(() => {
    if (retryAfter <= 0) return;
    const id = window.setTimeout(() => setRetryAfter((s) => s - 1), 1000);
    return () => window.clearTimeout(id);
  }, [retryAfter]);

  const submit = async (values: { username: string; password: string }) => {
    setSubmitting(true);
    setError(null);
    const { data, error: err, response } = await client.POST("/auth/login", { body: values });
    setSubmitting(false);
    if (data) {
      onLogin(data);
      return;
    }
    if (response.status === 429) {
      const secs = (err as { retryAfterSeconds?: number } | undefined)?.retryAfterSeconds ?? 0;
      setRetryAfter(secs);
      return;
    }
    setError(err?.message ?? "登录失败");
  };

  const locked = retryAfter > 0;
  return (
    <div className="login-shell">
      <div className="login-panel">
        <Brand className="login-brand" />
        <div className="login-card">
          <div className="login-card-head">
            <h1>登录</h1>
            <p>使用管理员分配的账号进入协作空间</p>
          </div>
          {error && <Alert type="error" message={error} style={{ marginBottom: 14 }} />}
          {locked && (
            <Alert type="warning" message={`尝试过多，请 ${retryAfter} 秒后再试`} style={{ marginBottom: 14 }} />
          )}
          <Form layout="vertical" onFinish={submit} requiredMark={false}>
            <Form.Item
              name="username"
              label="用户名"
              rules={[{ required: true, message: "请输入用户名" }]}
            >
              <Input autoFocus autoComplete="username" placeholder="请输入用户名" />
            </Form.Item>
            <Form.Item
              name="password"
              label="密码"
              rules={[{ required: true, message: "请输入密码" }]}
            >
              <PasswordInput autoComplete="current-password" placeholder="请输入密码" />
            </Form.Item>
            <Button type="primary" htmlType="submit" block loading={submitting} disabled={locked}>
              {locked ? `${retryAfter} 秒后可再试` : "登录"}
            </Button>
          </Form>
          {/* #214：邮件通道已配置时才有找回密码入口。 */}
          {branding.canRecoverPassword && (
            <p style={{ margin: "12px 0 0", textAlign: "right" }}>
              <Link to="/forgot-password">忘记密码？</Link>
            </p>
          )}
        </div>
        {/* #210：登录页提示语读系统设置，为空则不显示该行。 */}
        {branding.loginHint && (
          <p className="login-foot">
            <Icon name="lock" size={13} />
            {branding.loginHint}
          </p>
        )}
      </div>
    </div>
  );
}

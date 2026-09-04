import { useState } from "react";
import { Alert, Button, Form, Input } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";
import { Brand } from "./Brand";
import PasswordInput from "./PasswordInput";

type CurrentUser = components["schemas"]["CurrentUser"];

export default function LoginPage({ onLogin }: { onLogin: (u: CurrentUser) => void }) {
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const submit = async (values: { username: string; password: string }) => {
    setSubmitting(true);
    setError(null);
    const { data, error: err } = await client.POST("/auth/login", { body: values });
    setSubmitting(false);
    if (data) {
      onLogin(data);
    } else {
      setError(err?.message ?? "登录失败");
    }
  };

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
            <Button type="primary" htmlType="submit" block loading={submitting}>
              登录
            </Button>
          </Form>
        </div>
        <p className="login-foot">
          <Icon name="lock" size={13} />
          内网部署 · 账号由管理员分配
        </p>
      </div>
    </div>
  );
}

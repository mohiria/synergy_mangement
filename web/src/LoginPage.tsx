import { useState } from "react";
import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";

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
      <div style={{ width: 360 }}>
        <div className="login-brand">
          <span className="brand-mark">协</span>
          <div className="brand-name">
            <b>协同管理工具</b>
            <span>O／KR／任务协同推进</span>
          </div>
        </div>
        <Card>
          {error && <Alert type="error" message={error} style={{ marginBottom: 16 }} />}
          <Form layout="vertical" onFinish={submit} requiredMark={false}>
            <Form.Item name="username" label="用户名" rules={[{ required: true, message: "请输入用户名" }]}>
              <Input autoFocus autoComplete="username" />
            </Form.Item>
            <Form.Item name="password" label="口令" rules={[{ required: true, message: "请输入口令" }]}>
              <Input.Password autoComplete="current-password" />
            </Form.Item>
            <Button type="primary" htmlType="submit" block loading={submitting}>
              登录
            </Button>
          </Form>
        </Card>
        <Typography.Paragraph className="muted" style={{ textAlign: "center", marginTop: 14 }}>
          内网部署 · 账号由管理员分配
        </Typography.Paragraph>
      </div>
    </div>
  );
}

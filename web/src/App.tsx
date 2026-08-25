import { useEffect, useState } from "react";
import { Layout, Tag, Typography } from "antd";
import { client } from "./api/client";

export default function App() {
  const [healthy, setHealthy] = useState<boolean | null>(null);

  useEffect(() => {
    client
      .GET("/healthz")
      .then(({ data }) => setHealthy(data?.status === "ok"))
      .catch(() => setHealthy(false));
  }, []);

  return (
    <Layout style={{ minHeight: "100vh" }}>
      <Layout.Content style={{ padding: 24 }}>
        <Typography.Title level={3}>协同管理工具</Typography.Title>
        <Typography.Paragraph>
          后端状态：
          {healthy === null ? (
            <Tag>检测中</Tag>
          ) : healthy ? (
            <Tag color="green">正常</Tag>
          ) : (
            <Tag color="red">不可用</Tag>
          )}
        </Typography.Paragraph>
      </Layout.Content>
    </Layout>
  );
}

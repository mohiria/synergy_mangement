import { useCallback, useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { Badge, Button, Popover } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";

type Notification = components["schemas"]["Notification"];

// 站内通知：铃铛 + 未读角标，点击条目直达对应任务讨论（AC-36）。
// 顶栏在项目内页与项目列表页共用同一个组件，两处形态保持一致（基线 §5）。
export default function NotificationBell() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const [notifications, setNotifications] = useState<Notification[]>([]);

  const loadNotifications = useCallback(async () => {
    const res = await client.GET("/notifications");
    if (res.data) setNotifications(res.data);
  }, []);
  useEffect(() => {
    loadNotifications();
  }, [loadNotifications, pathname]);

  const unread = notifications.filter((n) => !n.readAt).length;

  const openNotification = (n: Notification) => {
    if (n.projectId && n.taskId) {
      navigate(`/projects/${n.projectId}/tasks?task=${n.taskId}&tab=discussion`);
    }
  };

  const markAllRead = async () => {
    await client.POST("/notifications/read-all");
    loadNotifications();
  };

  const panel = (
    <div style={{ width: 320, maxHeight: 360, overflow: "auto" }}>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginBottom: 6,
        }}
      >
        <b style={{ fontSize: 14 }}>站内通知</b>
        <Button type="link" size="small" disabled={unread === 0} onClick={markAllRead}>
          全部已读
        </Button>
      </div>
      {notifications.length === 0 && (
        <div className="muted" style={{ padding: "18px 0", textAlign: "center", fontSize: 12 }}>
          暂无通知
        </div>
      )}
      {notifications.map((n) => (
        <div
          key={n.id}
          onClick={() => openNotification(n)}
          style={{
            padding: "8px 6px",
            borderBottom: "1px solid var(--line)",
            cursor: n.taskId ? "pointer" : "default",
            opacity: n.readAt ? 0.6 : 1,
            fontSize: 14,
          }}
        >
          {n.content}
          <div className="muted" style={{ fontSize: 12 }}>
            {n.createdAt.slice(0, 16).replace("T", " ")}
          </div>
        </div>
      ))}
    </div>
  );

  return (
    <Popover content={panel} trigger="click" placement="bottomRight">
      <Badge count={unread} size="small" offset={[-2, 2]}>
        <button className="icon-btn" type="button" aria-label="站内通知">
          <Icon name="bell" />
        </button>
      </Badge>
    </Popover>
  );
}

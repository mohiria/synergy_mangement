import { useEffect, useState } from "react";
import { Alert, Modal, Select, message } from "antd";
import { client } from "../api/client";
import type { components } from "../api/schema";
import { structureMessage } from "./shared";

type Task = components["schemas"]["Task"];
type ProjectMember = components["schemas"]["ProjectMember"];

// 接收方配置（模块 PRD §8.6、MW-09）：按需字段，与输入／输出配置同口径直接生效；
// 一至多名具体成员，或「所有项目成员」——后者在终审通过时按当时项目成员逐人生成待接收项。
export default function ReceiversModal({
  projectId,
  task,
  members,
  onClose,
  onSaved,
}: {
  projectId: number;
  task: Task | null;
  members: ProjectMember[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [scope, setScope] = useState<"none" | "members" | "all">("none");
  const [userIds, setUserIds] = useState<number[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (task) {
      setError(null);
      setScope(task.receiverScope);
      setUserIds((task.receivers ?? []).map((r) => r.userId));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task?.id]);

  if (!task) return null;
  // 接收方只查看、下载与确认接收，不拥有审核权，因此访客也可以是接收方。
  const options = members.map((m) => ({
    value: m.userId,
    label: `${m.displayName}（${m.username}）`,
  }));

  const save = async () => {
    setSaving(true);
    setError(null);
    const res = await client.PUT("/projects/{projectId}/tasks/{taskId}/receivers", {
      params: { path: { projectId, taskId: task.id } },
      body: { scope, userIds: scope === "members" ? userIds : [] },
    });
    setSaving(false);
    if (res.data) {
      message.success(structureMessage(res.data, "接收方配置已保存"));
      onSaved();
    } else {
      setError(res.error?.message ?? "保存失败");
    }
  };

  return (
    <Modal
      title={
        <div>
          接收方
          <span className="modal-sub">终审通过后，每位接收方在「待我接收」收到待确认的成果</span>
        </div>
      }
      open={!!task}
      width={520}
      confirmLoading={saving}
      onOk={save}
      onCancel={onClose}
      okText="保存配置"
      cancelText="取消"
      destroyOnHidden
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <Select
        style={{ width: "100%" }}
        value={scope}
        onChange={(v) => setScope(v)}
        options={[
          { value: "none", label: "不指定接收方" },
          { value: "members", label: "指定成员" },
          { value: "all", label: "所有项目成员" },
        ]}
      />
      {scope === "members" && (
        <Select
          mode="multiple"
          style={{ width: "100%", marginTop: 8 }}
          placeholder="选择接收方（至少一人）"
          value={userIds}
          onChange={setUserIds}
          options={options}
          optionFilterProp="label"
        />
      )}
      <div className="notice" style={{ marginTop: 12 }}>
        接收方只查看、下载和确认接收，不拥有审核权，不能退回。
        「所有项目成员」按终审通过当时的项目成员逐人生成待接收项。
      </div>
    </Modal>
  );
}

import { useEffect, useState } from "react";
import { Alert, Modal, Select, message } from "antd";
import { client } from "../api/client";
import type { components } from "../api/schema";

type Task = components["schemas"]["Task"];
type ProjectMember = components["schemas"]["ProjectMember"];

// 中间审核人配置（§5.4）：非关键字段，可直接调整；0～多名，或签。
export default function ReviewersModal({
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
  const [userIds, setUserIds] = useState<number[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (task) {
      setError(null);
      client
        .GET("/projects/{projectId}/tasks/{taskId}", {
          params: { path: { projectId, taskId: task.id } },
        })
        .then((res) => {
          if (res.data) setUserIds(res.data.reviewers.map((r) => r.userId));
        });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task?.id]);

  if (!task) return null;
  const options = members
    .filter((m) => m.role !== "viewer")
    .map((m) => ({ value: m.userId, label: `${m.displayName}（${m.username}）` }));

  const save = async () => {
    setSaving(true);
    setError(null);
    const res = await client.PUT("/projects/{projectId}/tasks/{taskId}/reviewers", {
      params: { path: { projectId, taskId: task.id } },
      body: { userIds },
    });
    setSaving(false);
    if (res.data) {
      message.success("中间审核人配置已调整");
      onSaved();
    } else {
      setError(res.error?.message ?? "保存失败");
    }
  };

  return (
    <Modal
      title={
        <div>
          中间审核（或签）
          <span className="modal-sub">0～多名；任一人通过即进入待 KR 终审，任一人退回则整体退回</span>
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
        mode="multiple"
        style={{ width: "100%" }}
        placeholder="选择中间审核人（可为空表示不设置）"
        value={userIds}
        onChange={setUserIds}
        options={options}
        optionFilterProp="label"
      />
      <div className="notice" style={{ marginTop: 12 }}>
        中间审核人配置不属于关键字段，可直接调整；提交完成申请后随申请快照锁定。
      </div>
    </Modal>
  );
}

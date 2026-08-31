import { useEffect, useState } from "react";
import { Alert, Modal, Select, message } from "antd";
import { client } from "../api/client";
import type { components } from "../api/schema";

type Task = components["schemas"]["Task"];
type ProjectMember = components["schemas"]["ProjectMember"];

// 参与人配置（词汇表「参与人」；主 PRD §9.2）：按需字段，且不属关键字段——
// 保存直接生效，不产生变更单、不进审批，因此这里不做 structureMessage 那套「已提交审批」分支。
export default function ParticipantsModal({
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
      setUserIds((task.participants ?? []).map((p) => p.userId));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task?.id]);

  if (!task) return null;
  // 负责人已单列在基础信息里，不再作为可选参与人；访客可以是参与人——参与人不带写权限。
  const options = members
    .filter((m) => m.userId !== task.ownerId)
    .map((m) => ({ value: m.userId, label: `${m.displayName}（${m.username}）` }));

  const save = async () => {
    setSaving(true);
    setError(null);
    const res = await client.PUT("/projects/{projectId}/tasks/{taskId}/participants", {
      params: { path: { projectId, taskId: task.id } },
      body: { userIds },
    });
    setSaving(false);
    if (res.data) {
      message.success("参与人已保存");
      onSaved();
    } else {
      setError(res.error?.message ?? "保存失败");
    }
  };

  return (
    <Modal
      title={
        <div>
          参与人
          <span className="modal-sub">任务上除负责人以外的协作者，只作展示与检索</span>
        </div>
      }
      open={!!task}
      width={520}
      confirmLoading={saving}
      onOk={save}
      onCancel={onClose}
      okText="保存"
      cancelText="取消"
      destroyOnHidden
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <Select
        mode="multiple"
        style={{ width: "100%" }}
        placeholder="选择参与人（可留空）"
        value={userIds}
        onChange={setUserIds}
        options={options}
        optionFilterProp="label"
      />
      <div className="notice" style={{ marginTop: 12 }}>
        参与人不会收到待办、不进入审批链，也不因此获得任何编辑权限；修改保存后直接生效。
      </div>
    </Modal>
  );
}

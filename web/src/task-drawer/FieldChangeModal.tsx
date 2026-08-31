import { useEffect, useState } from "react";
import { Alert, DatePicker, Input, Modal, Select, message } from "antd";
import type { Dayjs } from "dayjs";
import { client } from "../api/client";
import type { components } from "../api/schema";

type Task = components["schemas"]["Task"];
type ProjectMember = components["schemas"]["ProjectMember"];

// 编辑任务／提交关键字段修改（AC-23）：草稿直接生效；已入池任务进入审批（KR 负责人本人免审）。
export default function FieldChangeModal({
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
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [completionCriteria, setCompletionCriteria] = useState("");
  const [ownerId, setOwnerId] = useState<number | undefined>(undefined);
  const [endDate, setEndDate] = useState<Dayjs | null>(null);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (task) {
      setName(task.name);
      setDescription(task.description ?? "");
      setCompletionCriteria(task.completionCriteria ?? "");
      setOwnerId(task.ownerId);
      setEndDate(null);
      setReason("");
      setError(null);
    }
  }, [task]);

  if (!task) return null;
  const isDraft = task.status === "draft";
  const ownerOptions = members
    .filter((m) => m.role !== "viewer")
    .map((m) => ({ value: m.userId, label: `${m.displayName}（${m.username}）` }));

  const save = async () => {
    const changes: Record<string, unknown> = {};
    if (name.trim() !== task.name) changes.name = name.trim();
    if (description.trim() !== (task.description ?? "")) changes.description = description.trim();
    if (completionCriteria.trim() !== (task.completionCriteria ?? ""))
      changes.completionCriteria = completionCriteria.trim();
    if (ownerId !== undefined && ownerId !== task.ownerId) changes.ownerId = ownerId;
    if (endDate) changes.endDate = endDate.format("YYYY-MM-DD");
    if (Object.keys(changes).length === 0) {
      setError("没有任何修改");
      return;
    }
    setSaving(true);
    setError(null);
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/field-changes", {
      params: { path: { projectId, taskId: task.id } },
      body: { changes, reason: reason.trim() || undefined },
    });
    setSaving(false);
    if (res.data) {
      message.success(
        isDraft
          ? "草稿已更新"
          : res.data.fieldChange?.state === "pending"
            ? "已提交所属 KR 负责人审批，审批期间旧值继续生效"
            : "修改已生效",
      );
      onSaved();
    } else {
      setError(res.error?.message ?? "保存失败");
    }
  };

  return (
    <Modal
      title={
        <div>
          编辑任务
          <span className="modal-sub">
            {isDraft
              ? "草稿阶段可直接完善，保存后立即生效"
              : "名称、说明、完成标准、负责人与截止时间为关键字段，提交后由所属 KR 负责人审批"}
          </span>
        </div>
      }
      open={!!task}
      width={640}
      confirmLoading={saving}
      onOk={save}
      onCancel={onClose}
      okText={isDraft ? "保存" : "提交变更审批"}
      cancelText="取消"
      destroyOnHidden
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <div style={{ display: "grid", gap: 12 }}>
        <div>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>任务名称</div>
          <Input maxLength={200} value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>任务说明</div>
          <Input.TextArea rows={2} maxLength={2000} value={description} onChange={(e) => setDescription(e.target.value)} />
        </div>
        <div>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>完成标准</div>
          <Input.TextArea rows={2} maxLength={2000} value={completionCriteria} onChange={(e) => setCompletionCriteria(e.target.value)} />
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
          <div>
            <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>任务负责人</div>
            <Select
              style={{ width: "100%" }}
              options={ownerOptions}
              value={ownerId}
              onChange={setOwnerId}
              showSearch
              optionFilterProp="label"
            />
          </div>
          <div>
            <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
              截止时间（当前 {task.endDate}，不改可留空）
            </div>
            <DatePicker style={{ width: "100%" }} value={endDate} onChange={setEndDate} />
          </div>
        </div>
        {!isDraft && (
          <div>
            <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>修改原因（必填）</div>
            <Input.TextArea rows={2} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
          </div>
        )}
        {!isDraft && (
          <div className="notice">提交后由所属 KR 负责人审批；审批期间旧值继续生效，任务不暂停执行。</div>
        )}
      </div>
    </Modal>
  );
}

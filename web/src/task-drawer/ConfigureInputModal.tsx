import { useEffect, useState } from "react";
import { Alert, DatePicker, Input, Modal, Select, message } from "antd";
import type { Dayjs } from "dayjs";
import { client } from "../api/client";
import TreeTransfer from "../TreeTransfer";
import type { TreeTransferItem } from "../TreeTransfer";
import type { components } from "../api/schema";
import PersonPicker from "../PersonPicker";
import { structureMessage, type KrOption } from "./shared";

type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type ProjectMember = components["schemas"]["ProjectMember"];

// 配置输入（AC-28）：默认搜索系统内已有任务，选择来源任务及其交付物建立交付物边。
export default function ConfigureInputModal({
  projectId,
  task,
  tasks,
  taskCode,
  objectives,
  krList,
  members,
  onClose,
  onSaved,
}: {
  projectId: number;
  task: Task | null;
  tasks: Task[];
  taskCode: Map<number, string>;
  objectives: Objective[];
  krList: KrOption[];
  members: ProjectMember[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [mode, setMode] = useState<"task" | "member">("task");
  const [providerIds, setProviderIds] = useState<number[]>([]);
  const [contentNote, setContentNote] = useState("");
  const [sourceTaskIds, setSourceTaskIds] = useState<number[]>([]);
  const [necessity, setNecessity] = useState<"required" | "reference">("required");
  const [expectedDate, setExpectedDate] = useState<Dayjs | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (task) {
      setMode("task");
      setProviderIds([]);
      setContentNote("");
      setSourceTaskIds([]);
      setNecessity("required");
      setExpectedDate(null);
      setError(null);
    }
  }, [task]);

  if (!task) return null;
  const candidates = tasks.filter((t) => t.id !== task.id && t.status !== "cancelled");

  // 已有任务来源：O → KR → 任务三级树（AC-53）；分组顺序沿用 OKR 页派生的 O／KR 顺序。
  const objectiveTitle = new Map(objectives.map((o) => [o.id, o.title]));
  const krById = new Map(krList.map((k) => [k.id, k]));
  const krOrder = new Map(krList.map((k, i) => [k.id, i]));
  const taskItems: TreeTransferItem[] = [...candidates]
    .sort(
      (a, b) =>
        (krOrder.get(a.keyResultId) ?? Number.MAX_SAFE_INTEGER) -
          (krOrder.get(b.keyResultId) ?? Number.MAX_SAFE_INTEGER) || a.id - b.id,
    )
    .map((t) => {
      const kr = krById.get(t.keyResultId);
      const oTitle = kr ? objectiveTitle.get(kr.objectiveId) ?? "" : "";
      const code = taskCode.get(t.id) ?? "";
      const deliverables = t.deliverableNames ?? [];
      return {
        key: String(t.id),
        groups: kr
          ? [
              { key: `o${kr.objectiveId}`, label: oTitle },
              { key: `k${kr.id}`, label: `${kr.code} · ${kr.description}` },
            ]
          : [
              { key: "o0", label: "未归属 O" },
              { key: "k0", label: "未归属 KR" },
            ],
        label: (
          <span className="tree-transfer-row">
            <span className="tree-transfer-code">{code}</span>
            <span className="tree-transfer-text">
              <b title={t.name}>{t.name}</b>
              <small
                title={
                  t.ownerName + (deliverables.length > 0 ? ` · ${deliverables.join("、")}` : "")
                }
              >
                {t.ownerName}
                {deliverables.length > 0 && ` · ${deliverables.join("、")}`}
              </small>
            </span>
          </span>
        ),
        // 支持按任务名称、O、KR、负责人和交付物名称搜索（PRD §7.3）。
        search: `${code} ${t.name} ${t.ownerName} ${oTitle} ${kr?.code ?? ""} ${kr?.description ?? ""} ${deliverables.join(" ")}`.toLowerCase(),
      };
    });
  // #166：对接人选择统一为人员选择组件（搜索框 + 头像行）。
  const providerPeople = members
    .filter((m) => m.role !== "viewer")
    .map((m) => ({ userId: m.userId, displayName: m.displayName, username: m.username }));

  const save = async () => {
    if (mode === "member") {
      if (providerIds.length === 0) {
        setError("请至少选择一名对接人");
        return;
      }
      if (!contentNote.trim()) {
        setError("请填写所需内容");
        return;
      }
      if (!expectedDate) {
        setError("请填写期望时间");
        return;
      }
      setSaving(true);
      setError(null);
      const res = await client.POST("/projects/{projectId}/tasks/{taskId}/member-inputs", {
        params: { path: { projectId, taskId: task.id } },
        body: {
          necessity,
          providerIds,
          contentNote: contentNote.trim(),
          expectedDate: expectedDate.format("YYYY-MM-DD"),
        },
      });
      setSaving(false);
      if (res.data) {
        message.success(
          structureMessage(res.data, `已为 ${providerIds.length} 名对接人建立输入请求；对接人会收到站内通知`),
        );
        onSaved();
      } else {
        setError(res.error?.message ?? "保存失败");
      }
      return;
    }
    if (sourceTaskIds.length === 0) {
      setError("请至少选择一个来源任务");
      return;
    }
    setSaving(true);
    setError(null);
    // #174 裁决：任务来源不再单独填期望时间，展示与超期判断统一取上游任务截止日期。
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/inputs", {
      params: { path: { projectId, taskId: task.id } },
      body: {
        necessity,
        sourceTaskIds,
      },
    });
    setSaving(false);
    if (res.data) {
      message.success(
        structureMessage(res.data, `已建立 ${sourceTaskIds.length} 条「来源任务 → 本任务」的交付物边`),
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
          配置输入
          <span className="modal-sub">来源为系统内已有任务；来源任务完成后输入自动就绪（裁决 #163）</span>
        </div>
      }
      open={!!task}
      width={900}
      confirmLoading={saving}
      onOk={save}
      onCancel={onClose}
      okText="建立输入关系"
      cancelText="取消"
      destroyOnHidden
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      {/* 同上（#100）：来源任务树在这个网格里，网格项必须允许收缩。 */}
      <div style={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr)", gap: 12 }}>
        <div style={{ minWidth: 0 }}>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>来源模式（二选一）</div>
          <Select
            style={{ width: "100%" }}
            value={mode}
            onChange={setMode}
            options={[
              { value: "task", label: "已有任务（默认搜索系统内任务）" },
              { value: "member", label: "指定项目成员提供" },
            ]}
          />
        </div>
        {mode === "member" && (
          <>
            <div>
              <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
                对接人（可多选；非只读项目成员）
              </div>
              <PersonPicker
                people={providerPeople}
                value={providerIds}
                placeholder="选择对接人"
                size="middle"
                onSave={setProviderIds}
              />
            </div>
            <div>
              <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>所需内容（必填）</div>
              <Input.TextArea
                rows={2}
                maxLength={500}
                value={contentNote}
                onChange={(e) => setContentNote(e.target.value)}
              />
            </div>
          </>
        )}
        {mode === "task" && (
        <div>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
            来源任务（按 O／KR 整组选择或多选）
          </div>
          <TreeTransfer
            items={taskItems}
            targetKeys={sourceTaskIds.map(String)}
            onChange={(keys) => setSourceTaskIds(keys.map(Number))}
            titles={["可选任务", "已选任务"]}
            unit="项"
            searchPlaceholder="搜索 O、KR、任务、负责人或交付物"
          />
        </div>
        )}
        {/* #173 裁决：关系类型删除，只填必要性。
            #174 裁决：任务来源无期望时间（统一取上游任务截止日期）；成员来源保留必填期望时间。 */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
          <div>
            <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>必要性</div>
            <Select
              style={{ width: "100%" }}
              value={necessity}
              onChange={setNecessity}
              options={[
                { value: "required", label: "必要" },
                { value: "reference", label: "参考" },
              ]}
            />
          </div>
          {mode === "member" && (
            <div>
              <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>期望时间</div>
              <DatePicker style={{ width: "100%" }} value={expectedDate} onChange={setExpectedDate} />
            </div>
          )}
        </div>
        <div className="notice">
          缺了它下游就做不了时选「必要」，仅供参考时选「参考」；必要输入未就绪的未开始任务显示“等待输入”，
          但不阻断开始、上传文件或提交完成申请，任务开始后该状态与“上游未就绪”卡点自动消失。
          偶发的外部材料不产生外部账号：由内部协调人（项目成员）作为对接人收集后代为提交。
        </div>
      </div>
    </Modal>
  );
}

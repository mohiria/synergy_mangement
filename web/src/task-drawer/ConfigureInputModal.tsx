import { useEffect, useMemo, useState } from "react";
import { Alert, DatePicker, Input, Modal, Select, Table, message } from "antd";
import type { Dayjs } from "dayjs";
import { client } from "../api/client";
import type { components } from "../api/schema";
import PersonPicker from "../PersonPicker";
import Icon from "../icons";
import { STATUS_CLASS, structureMessage, type KrOption } from "./shared";

type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type ProjectMember = components["schemas"]["ProjectMember"];

// 选择输入源（AC-28；#176 重做）：已有任务在弹窗表格中直接多选
// （列含编号、名称、负责人、所属 KR、状态），支持搜索与按 O/KR 定位；
// 字段集为裁决精简后的「来源任务（多选）＋必要性」（#173/#174）。
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
  const [searchText, setSearchText] = useState("");
  const [krFilter, setKrFilter] = useState<number | "all">("all");
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
      setSearchText("");
      setKrFilter("all");
      setError(null);
    }
  }, [task]);

  const objectiveTitle = useMemo(() => new Map(objectives.map((o) => [o.id, o.title])), [objectives]);
  const krById = useMemo(() => new Map(krList.map((k) => [k.id, k])), [krList]);
  const krOrder = useMemo(() => new Map(krList.map((k, i) => [k.id, i])), [krList]);

  // #176：已有任务直接在表格里多选；按 O/KR 定位与搜索在表格上方（PRD §7.3 搜索口径不变）。
  const candidateRows = useMemo(() => {
    if (!task) return [];
    const kw = searchText.trim().toLowerCase();
    return tasks
      .filter((t) => t.id !== task.id && t.status !== "cancelled")
      .filter((t) => krFilter === "all" || t.keyResultId === krFilter)
      .filter((t) => {
        if (!kw) return true;
        const kr = krById.get(t.keyResultId);
        const oTitle = kr ? objectiveTitle.get(kr.objectiveId) ?? "" : "";
        const hay = `${taskCode.get(t.id) ?? ""} ${t.name} ${t.ownerName} ${oTitle} ${
          kr?.code ?? ""
        } ${kr?.description ?? ""} ${(t.deliverableNames ?? []).join(" ")}`.toLowerCase();
        return hay.includes(kw);
      })
      .sort(
        (a, b) =>
          (krOrder.get(a.keyResultId) ?? Number.MAX_SAFE_INTEGER) -
            (krOrder.get(b.keyResultId) ?? Number.MAX_SAFE_INTEGER) || a.id - b.id,
      );
  }, [tasks, task, searchText, krFilter, krById, krOrder, objectiveTitle, taskCode]);
  if (!task) return null;

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
          选择输入源
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
        <div style={{ minWidth: 0 }}>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
            来源任务（表格中直接勾选，可多选）
          </div>
          <div style={{ display: "flex", gap: 8, marginBottom: 8 }}>
            <Input
              allowClear
              prefix={<Icon name="search" size={15} />}
              style={{ flex: 1 }}
              placeholder="搜索 O、KR、任务、负责人或交付物"
              value={searchText}
              onChange={(e) => setSearchText(e.target.value)}
            />
            <Select
              style={{ width: 220 }}
              value={krFilter}
              onChange={setKrFilter}
              options={[
                { value: "all" as const, label: "全部 O / KR" },
                ...krList.map((k) => ({ value: k.id, label: `${k.code} · ${k.description}` })),
              ]}
              optionLabelProp="label"
              popupMatchSelectWidth={360}
            />
          </div>
          <Table
            className="input-source-table"
            size="small"
            rowKey="id"
            dataSource={candidateRows}
            pagination={false}
            scroll={{ y: 320 }}
            rowSelection={{
              selectedRowKeys: sourceTaskIds,
              onChange: (keys) => setSourceTaskIds(keys.map(Number)),
            }}
            onRow={(t) => ({
              // 点行即勾选；勾选框本身的点击交给 rowSelection，避免二次切换。
              onClick: (e) => {
                if ((e.target as HTMLElement).closest(".ant-table-selection-column")) return;
                setSourceTaskIds((prev) =>
                  prev.includes(t.id) ? prev.filter((id) => id !== t.id) : [...prev, t.id],
                );
              },
              style: { cursor: "pointer" },
            })}
            locale={{ emptyText: "没有匹配的任务" }}
            columns={[
              {
                title: "编号",
                width: 84,
                render: (_, t) => <span className="mono">{taskCode.get(t.id) ?? ""}</span>,
              },
              {
                title: "任务",
                ellipsis: { showTitle: false },
                render: (_, t) => <span title={t.name}>{t.name}</span>,
              },
              { title: "负责人", width: 96, dataIndex: "ownerName", ellipsis: true },
              {
                title: "所属 KR",
                width: 200,
                ellipsis: { showTitle: false },
                render: (_, t) => {
                  const kr = krById.get(t.keyResultId);
                  const label = kr ? `${kr.code} · ${kr.description}` : "—";
                  return <span title={label}>{label}</span>;
                },
              },
              {
                title: "状态",
                width: 104,
                render: (_, t) => (
                  <span className={`status-pill ${STATUS_CLASS[t.status]}`}>{t.statusLabel}</span>
                ),
              },
            ]}
          />
          <div className="muted" style={{ fontSize: 12, marginTop: 6 }}>
            已选 {sourceTaskIds.length} 项
          </div>
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

import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Button, Drawer, Input, Modal, Select, Spin, message } from "antd";
import type { Dayjs } from "dayjs";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";
import ImportModal from "./ImportModal";
import DateRangeField from "./DateRangeField";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Objective = components["schemas"]["Objective"];
type KeyResult = components["schemas"]["KeyResult"];
type ProjectMember = components["schemas"]["ProjectMember"];
type RiskLevel = components["schemas"]["RiskLevel"];
type CreateOkrBatchItem = components["schemas"]["CreateOkrBatchItem"];

const RISK_LABEL: Record<RiskLevel, string> = {
  normal: "正常",
  warning: "预警",
  high_risk: "高风险",
};

// 周期展示沿用原型的紧凑格式（08.20—09.18）。
const fmtDate = (d?: string | null) => (d ? d.slice(5).replace("-", ".") : "…");

export default function ProjectOkrPage({
  user,
  onLogout,
}: {
  user: CurrentUser;
  onLogout: () => void;
}) {
  const { projectId: projectIdParam } = useParams();
  const projectId = Number(projectIdParam);

  const [project, setProject] = useState<Project | null>(null);
  const [objectives, setObjectives] = useState<Objective[]>([]);
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  // §7.2：取消行内编辑图标，点整行打开右侧编辑抽屉。
  const [editing, setEditing] = useState<{ kind: "O"; o: Objective } | { kind: "KR"; k: KeyResult } | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, objectivesRes, membersRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/objectives", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/members", { params: { path: { projectId } } }),
    ]);
    if (projectRes.response.status === 401) {
      onLogout();
      return;
    }
    if (projectRes.response.status === 404 || !projectRes.data) {
      setNotFound(true);
      setLoading(false);
      return;
    }
    setProject(projectRes.data);
    setObjectives(objectivesRes.data ?? []);
    setMembers(membersRes.data ?? []);
    setLoading(false);
  }, [projectId, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  // 展示编号按列表顺序派生（O1…、KR 全局连续），仅用于界面呈现。
  let krSeq = 0;
  const rows = objectives.flatMap((o, oIndex) => [
    <tr
      key={`o-${o.id}`}
      className={`table-group${o.canEdit ? " row-clickable" : ""}`}
      onClick={o.canEdit ? () => setEditing({ kind: "O", o }) : undefined}
    >
      <td colSpan={7}>
        O{oIndex + 1}　{o.title}
        {o.description && <span className="muted">　{o.description}</span>}
      </td>
    </tr>,
    ...o.keyResults.map((k: KeyResult) => {
      krSeq += 1;
      return (
        <tr
          key={`kr-${k.id}`}
          className={k.canEdit ? "row-clickable" : undefined}
          onClick={k.canEdit ? () => setEditing({ kind: "KR", k }) : undefined}
        >
          <td className="mono">KR{krSeq}</td>
          <td>{k.description}</td>
          <td>
            {k.ownerName ? (
              <span className="owner-cell">
                <span className="avatar">{k.ownerName.slice(0, 1)}</span>
                {k.ownerName}
              </span>
            ) : (
              <span className="muted">未指定</span>
            )}
          </td>
          <td>
            {k.startDate || k.endDate ? (
              <span>
                {fmtDate(k.startDate)}—{fmtDate(k.endDate)}
              </span>
            ) : (
              <span className="muted">—</span>
            )}
          </td>
          <td>{k.metric ?? <span className="muted">待补充量化指标</span>}</td>
          <td>
            <span className={`status-pill risk-${k.riskLevel}`}>{RISK_LABEL[k.riskLevel]}</span>
          </td>
          <td>{k.taskCount ? `${k.taskCount} 项` : <span className="muted">—</span>}</td>
        </tr>
      );
    }),
  ]);

  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="OKR 管理"
      onLogout={onLogout}
    >
      {notFound ? (
            <Alert
              type="error"
              message="项目不存在"
              description={<Link to="/">返回项目列表</Link>}
            />
          ) : loading || !project ? (
            <Spin />
          ) : (
            <>
              <div className="page-head">
                <div>
                  <h1>OKR 管理</h1>
                  <p>O、KR 在线下确定后在此连续录入；系统不承载 OKR 讨论审批。</p>
                </div>
                {project.canEdit && (
                  <div style={{ display: "flex", gap: 8 }}>
                    <Button onClick={() => setImportOpen(true)}>导入已有表格</Button>
                    <Button type="primary" onClick={() => setModalOpen(true)}>
                      ＋ 新增 O / KR
                    </Button>
                  </div>
                )}
              </div>
              <div className="data-table-wrap">
                <table className="data-table">
                  <thead>
                    <tr>
                      <th style={{ width: 70 }}>KR</th>
                      <th>目标描述</th>
                      <th style={{ width: 140 }}>负责人</th>
                      <th style={{ width: 210 }}>周期</th>
                      <th style={{ width: 180 }}>量化指标</th>
                      <th style={{ width: 100 }}>状态</th>
                      <th style={{ width: 90 }}>任务</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.length > 0 ? (
                      rows
                    ) : (
                      <tr>
                        <td colSpan={7}>
                          <div className="empty">暂无 O／KR</div>
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
        </>
      )}
      <ImportModal
        open={importOpen}
        projectId={projectId}
        members={members}
        onClose={() => setImportOpen(false)}
        onImported={() => {
          setImportOpen(false);
          load();
        }}
      />
      <OkrEditDrawer
        target={editing}
        projectId={projectId}
        members={members}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null);
          load();
        }}
      />
      {project && (
        <OkrBatchModal
          open={modalOpen}
          projectId={projectId}
          objectives={objectives}
          members={members}
          onClose={() => setModalOpen(false)}
          onSaved={(latest) => {
            setObjectives(latest);
            setModalOpen(false);
          }}
        />
      )}
    </ProjectShell>
  );
}

// OkrEditDrawer 编辑 O／KR 的右侧抽屉（§7.2、AC-61、AC-65）：
// O 只有项目管理员能打开；KR 由管理员或本人负责的 KR 打开。
// 更换 KR 负责人且该 KR 下有未决审批时先弹确认框，默认转交继任者。
function OkrEditDrawer({
  target,
  projectId,
  members,
  onClose,
  onSaved,
}: {
  target: { kind: "O"; o: Objective } | { kind: "KR"; k: KeyResult } | null;
  projectId: number;
  members: ProjectMember[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [metric, setMetric] = useState("");
  const [ownerId, setOwnerId] = useState<number | undefined>(undefined);
  const [period, setPeriod] = useState<[Dayjs | null, Dayjs | null] | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!target) return;
    setError(null);
    if (target.kind === "O") {
      setTitle(target.o.title);
      setDescription(target.o.description ?? "");
      return;
    }
    setDescription(target.k.description);
    setMetric(target.k.metric ?? "");
    setOwnerId(target.k.ownerId ?? undefined);
    setPeriod(null);
  }, [target]);

  if (!target) return null;

  const eligible = members.filter((m) => m.role !== "viewer");

  const saveObjective = async () => {
    if (target.kind !== "O") return;
    setSaving(true);
    setError(null);
    const res = await client.PATCH("/projects/{projectId}/objectives/{objectiveId}", {
      params: { path: { projectId, objectiveId: target.o.id } },
      body: { title: title.trim(), description: description.trim() },
    });
    setSaving(false);
    if (res.data) {
      message.success("O 已更新");
      onSaved();
    } else {
      setError(res.error?.message ?? "保存失败");
    }
  };

  const patchKeyResult = async (transfer: boolean) => {
    if (target.kind !== "KR") return;
    setSaving(true);
    setError(null);
    const res = await client.PATCH("/projects/{projectId}/key-results/{keyResultId}", {
      params: { path: { projectId, keyResultId: target.k.id } },
      body: {
        description: description.trim(),
        metric: metric.trim(),
        ownerId,
        startDate: period?.[0] ? period[0]!.format("YYYY-MM-DD") : undefined,
        endDate: period?.[1] ? period[1]!.format("YYYY-MM-DD") : undefined,
        transferPendingApprovals: transfer,
      },
    });
    setSaving(false);
    if (res.data) {
      message.success("KR 已更新");
      onSaved();
    } else {
      setError(res.error?.message ?? "保存失败");
    }
  };

  const saveKeyResult = async () => {
    if (target.kind !== "KR") return;
    if (!ownerId) {
      setError("KR 负责人不可为空，请直接指定继任者");
      return;
    }
    if (ownerId === target.k.ownerId) {
      await patchKeyResult(true);
      return;
    }
    // AC-61：换人前先看该 KR 下有多少未决审批，有才弹确认框。
    const preview = await client.GET("/projects/{projectId}/key-results/{keyResultId}", {
      params: { path: { projectId, keyResultId: target.k.id } },
    });
    const pending = preview.data?.pendingApprovals ?? 0;
    if (pending === 0) {
      await patchKeyResult(true);
      return;
    }
    let transfer = true;
    Modal.confirm({
      title: "该 KR 下有未决审批",
      content: (
        <div>
          <p style={{ marginTop: 0 }}>
            共 {pending} 件未决审批。默认转交继任者处理，继任者会收到站内通知；
            取消勾选则保留给原负责人处理。
          </p>
          <label>
            <input
              type="checkbox"
              defaultChecked
              onChange={(e) => {
                transfer = e.target.checked;
              }}
            />
            　把未决审批转交继任者
          </label>
        </div>
      ),
      okText: "确认更换",
      cancelText: "返回",
      onOk: () => patchKeyResult(transfer),
    });
  };

  const remove = async () => {
    const path =
      target.kind === "O"
        ? client.DELETE("/projects/{projectId}/objectives/{objectiveId}", {
            params: { path: { projectId, objectiveId: target.o.id } },
          })
        : client.DELETE("/projects/{projectId}/key-results/{keyResultId}", {
            params: { path: { projectId, keyResultId: target.k.id } },
          });
    const res = await path;
    if (res.response.ok) {
      message.success("已删除");
      onSaved();
    } else {
      setError(res.error?.message ?? "删除失败");
    }
  };

  const canDelete = target.kind === "O" ? target.o.canDelete : target.k.canDelete;
  const deleteHint =
    target.kind === "O" ? "O 下还有 KR 时不能删除" : "KR 下还有任务（含已完成、已取消）时不能删除";

  return (
    <Drawer
      open
      width={420}
      title={target.kind === "O" ? "编辑 O" : "编辑 KR"}
      onClose={onClose}
      footer={
        <div style={{ display: "flex", justifyContent: "space-between" }}>
          <Button danger disabled={!canDelete} title={canDelete ? undefined : deleteHint} onClick={remove}>
            删除
          </Button>
          <div style={{ display: "flex", gap: 8 }}>
            <Button onClick={onClose}>取消</Button>
            <Button
              type="primary"
              loading={saving}
              onClick={target.kind === "O" ? saveObjective : saveKeyResult}
            >
              保存
            </Button>
          </div>
        </div>
      }
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      {target.kind === "O" ? (
        <>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>O 标题</div>
          <Input value={title} maxLength={100} onChange={(e) => setTitle(e.target.value)} />
          <div className="muted" style={{ fontSize: 12, margin: "12px 0 4px" }}>O 说明（选填）</div>
          <Input.TextArea
            rows={3}
            maxLength={500}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </>
      ) : (
        <>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>KR 描述</div>
          <Input value={description} maxLength={200} onChange={(e) => setDescription(e.target.value)} />
          <div className="muted" style={{ fontSize: 12, margin: "12px 0 4px" }}>量化指标（选填）</div>
          <Input value={metric} maxLength={100} onChange={(e) => setMetric(e.target.value)} />
          <div className="muted" style={{ fontSize: 12, margin: "12px 0 4px" }}>负责人（不可为空）</div>
          <Select
            style={{ width: "100%" }}
            value={ownerId}
            placeholder="选择负责人"
            showSearch
            optionFilterProp="label"
            onChange={setOwnerId}
            options={eligible.map((m) => ({ value: m.userId, label: m.displayName }))}
          />
          <div className="muted" style={{ fontSize: 12, margin: "12px 0 4px" }}>周期（不改可留空）</div>
          <DateRangeField value={period} onChange={setPeriod} />
        </>
      )}
    </Drawer>
  );
}

type OkrRow = {
  key: number;
  kind: "O" | "KR";
  title: string;
  objRef?: string; // KR 所属 O："new:<rowKey>" 或 "existing:<objectiveId>"
  description: string;
  ownerId?: number;
  period?: [Dayjs | null, Dayjs | null] | null;
};

let rowSeq = 0;

function OkrBatchModal({
  open,
  projectId,
  objectives,
  members,
  onClose,
  onSaved,
}: {
  open: boolean;
  projectId: number;
  objectives: Objective[];
  members: ProjectMember[];
  onClose: () => void;
  onSaved: (latest: Objective[]) => void;
}) {
  const [rows, setRows] = useState<OkrRow[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) {
      // 与原型一致：打开即预置一行 O 和一行归属该 O 的 KR。
      const oRow: OkrRow = { key: ++rowSeq, kind: "O", title: "", description: "" };
      const krRow: OkrRow = {
        key: ++rowSeq,
        kind: "KR",
        title: "",
        objRef: `new:${oRow.key}`,
        description: "",
      };
      setRows([oRow, krRow]);
      setError(null);
    }
  }, [open]);

  const newRow = (kind: "O" | "KR"): OkrRow => {
    const oRows = rows.filter((r) => r.kind === "O");
    const defaultRef =
      kind === "KR"
        ? oRows.length > 0
          ? `new:${oRows[oRows.length - 1].key}`
          : objectives.length > 0
            ? `existing:${objectives[objectives.length - 1].id}`
            : undefined
        : undefined;
    return { key: ++rowSeq, kind, title: "", objRef: defaultRef, description: "" };
  };

  const patch = (key: number, p: Partial<OkrRow>) =>
    setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...p } : r)));

  const removeRow = (key: number) =>
    setRows((rs) =>
      rs
        .filter((r) => r.key !== key)
        .map((r) => (r.objRef === `new:${key}` ? { ...r, objRef: undefined } : r)),
    );

  // 弹窗内展示编号：新 O 接在已有 O 之后，KR 接在已有 KR 之后。
  const existingKrCount = objectives.reduce((n, o) => n + o.keyResults.length, 0);
  const oCodeByKey = new Map<number, string>();
  const krCodeByKey = new Map<number, string>();
  {
    let oN = objectives.length;
    let krN = existingKrCount;
    for (const r of rows) {
      if (r.kind === "O") oCodeByKey.set(r.key, `O${++oN}`);
      else krCodeByKey.set(r.key, `KR${++krN}`);
    }
  }

  const objRefOptions = [
    ...rows
      .filter((r) => r.kind === "O")
      .map((r) => ({
        value: `new:${r.key}`,
        label: `${oCodeByKey.get(r.key)}：${r.title.trim() || "（未命名）"}`,
      })),
    ...objectives.map((o, i) => ({ value: `existing:${o.id}`, label: `O${i + 1}：${o.title}` })),
  ];

  const ownerOptions = members.map((m) => ({
    value: m.userId,
    label: `${m.displayName}（${m.username}）`,
  }));

  const save = async () => {
    const oRows = rows.filter((r) => r.kind === "O");
    const krRows = rows.filter((r) => r.kind === "KR");
    if (rows.length === 0) {
      setError("请至少添加一个 O 或 KR 事项");
      return;
    }
    if (oRows.some((r) => !r.title.trim())) {
      setError("O 目标描述不能为空");
      return;
    }
    if (krRows.some((r) => !r.description.trim())) {
      setError("KR 目标描述不能为空");
      return;
    }
    const newIndexByKey = new Map<number, number>();
    const items: CreateOkrBatchItem[] = [];
    for (const r of oRows) {
      newIndexByKey.set(r.key, items.length);
      items.push({ title: r.title.trim(), keyResults: [] });
    }
    const existingIndexById = new Map<number, number>();
    for (const r of krRows) {
      if (!r.objRef) {
        setError("每条 KR 都要选择所属 O");
        return;
      }
      const kr = {
        description: r.description.trim(),
        ownerId: r.ownerId,
        startDate: r.period?.[0]?.format("YYYY-MM-DD"),
        endDate: r.period?.[1]?.format("YYYY-MM-DD"),
      };
      if (r.objRef.startsWith("new:")) {
        const idx = newIndexByKey.get(Number(r.objRef.slice(4)));
        if (idx === undefined) {
          setError("每条 KR 都要选择所属 O");
          return;
        }
        items[idx].keyResults!.push(kr);
      } else {
        const oid = Number(r.objRef.slice("existing:".length));
        let idx = existingIndexById.get(oid);
        if (idx === undefined) {
          idx = items.length;
          existingIndexById.set(oid, idx);
          items.push({ objectiveId: oid, keyResults: [] });
        }
        items[idx].keyResults!.push(kr);
      }
    }
    setSaving(true);
    setError(null);
    const res = await client.POST("/projects/{projectId}/objectives", {
      params: { path: { projectId } },
      body: { items },
    });
    setSaving(false);
    if (res.data) {
      onSaved(res.data);
    } else {
      setError(res.error?.message ?? "保存失败");
    }
  };

  return (
    <Modal
      title={
        <div>
          新增 O / KR
          <span className="modal-sub">横向区分 O 与 KR，向下连续增加事项并指定负责人</span>
        </div>
      }
      open={open}
      width={1080}
      confirmLoading={saving}
      onOk={save}
      onCancel={onClose}
      okText="保存 O / KR"
      cancelText="取消"
      destroyOnHidden
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <div className="notice">O、KR 仍由线下确定；这里仅用于一次性连续录入结构和负责人。</div>
      <div className="okr-sheet">
        <div className="okr-sheet-head">
          <span>目标 O</span>
          <span>关键结果 KR</span>
          <span>所属 O</span>
          <span>负责人</span>
          <span>周期</span>
          <span />
        </div>
        {rows.map((r) =>
          r.kind === "O" ? (
            <div key={r.key} className="okr-sheet-row">
              <div className="okr-sheet-cell">
                <div className="sheet-input-pair">
                  <span className="sheet-code">{oCodeByKey.get(r.key)}</span>
                  <Input
                    maxLength={100}
                    placeholder="O 目标描述"
                    value={r.title}
                    onChange={(e) => patch(r.key, { title: e.target.value })}
                  />
                </div>
              </div>
              <div className="okr-sheet-cell">
                <span className="sheet-placeholder">该行用于创建 O</span>
              </div>
              <div className="okr-sheet-cell">
                <span className="sheet-placeholder">项目目标</span>
              </div>
              <div className="okr-sheet-cell">
                <span className="sheet-placeholder">—</span>
              </div>
              <div className="okr-sheet-cell">
                <span className="sheet-placeholder">沿用项目周期</span>
              </div>
              <Button type="text" size="small" onClick={() => removeRow(r.key)} aria-label="删除该行">
                ✕
              </Button>
            </div>
          ) : (
            <div key={r.key} className="okr-sheet-row">
              <div className="okr-sheet-cell">
                <span className="sheet-placeholder">—</span>
              </div>
              <div className="okr-sheet-cell">
                <div className="sheet-input-pair">
                  <span className="sheet-code">{krCodeByKey.get(r.key)}</span>
                  <Input
                    maxLength={200}
                    placeholder="KR 目标描述"
                    value={r.description}
                    onChange={(e) => patch(r.key, { description: e.target.value })}
                  />
                </div>
              </div>
              <div className="okr-sheet-cell">
                <Select
                  style={{ width: "100%" }}
                  options={objRefOptions}
                  value={r.objRef}
                  onChange={(v) => patch(r.key, { objRef: v })}
                  placeholder="所属 O"
                />
              </div>
              <div className="okr-sheet-cell">
                <Select
                  style={{ width: "100%" }}
                  options={ownerOptions}
                  value={r.ownerId}
                  onChange={(v) => patch(r.key, { ownerId: v })}
                  allowClear
                  showSearch
                  optionFilterProp="label"
                  placeholder="负责人"
                />
              </div>
              <div className="okr-sheet-cell">
                <DateRangeField
                  allowEmpty
                  value={r.period}
                  onChange={(v) => patch(r.key, { period: v })}
                />
              </div>
              <Button type="text" size="small" onClick={() => removeRow(r.key)} aria-label="删除该行">
                ✕
              </Button>
            </div>
          ),
        )}
      </div>
      <div className="okr-sheet-actions">
        <Button size="small" onClick={() => setRows((rs) => [...rs, newRow("O")])}>
          ＋ 添加 O 事项
        </Button>
        <Button
          size="small"
          disabled={rows.every((r) => r.kind !== "O") && objectives.length === 0}
          onClick={() => setRows((rs) => [...rs, newRow("KR")])}
        >
          ＋ 添加 KR 事项
        </Button>
      </div>
    </Modal>
  );
}

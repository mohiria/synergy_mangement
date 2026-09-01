import { useCallback, useEffect, useState } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";
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
type CreateOkrBatchItem = components["schemas"]["CreateOkrBatchItem"];


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
  const navigate = useNavigate();

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

  // #125：管理模式只维护结构字段（编号／名称／所属 O／负责人／周期／量化标准），
  // 风险、卡点、任务数与进度一律留在项目总览；点行开右侧编辑抽屉（§7.2）。
  const rows = objectives.flatMap((o) => [
    <tr
      key={`o-${o.id}`}
      className={`okr-structure-o${o.canEdit ? " row-clickable okr-editable-row" : ""}`}
      onClick={o.canEdit ? () => setEditing({ kind: "O", o }) : undefined}
    >
      <td>
        <span className="okr-level-tag">O</span>
        <span className="mono">{o.code}</span>
      </td>
      <td title={`${o.title}${o.description ? `　${o.description}` : ""}`}>
        <strong>{o.title}</strong>
        {o.description && <span className="muted">　{o.description}</span>}
      </td>
      <td className="muted">—</td>
      <td className="muted">—</td>
      <td className="muted">—</td>
      <td className="muted">—</td>
      <td>{o.canEdit && <span className="okr-row-action">编辑</span>}</td>
    </tr>,
    ...o.keyResults.map((k: KeyResult) => {
      return (
        <tr
          key={`kr-${k.id}`}
          className={`okr-structure-kr${k.canEdit ? " row-clickable okr-editable-row" : ""}`}
          onClick={k.canEdit ? () => setEditing({ kind: "KR", k }) : undefined}
        >
          <td className="okr-kr-code-cell">
            <span className="okr-level-branch" aria-hidden="true" />
            <span className="okr-level-tag kr">KR</span>
            <span className="mono">{k.code}</span>
          </td>
          <td title={k.description}>{k.description}</td>
          <td className="mono">{o.code}</td>
          <td title={k.ownerName ?? "未指定"}>
            {k.ownerName ? (
              <span className="owner-cell">
                <span className="avatar">{k.ownerName.slice(0, 1)}</span>
                <span className="cell-text">{k.ownerName}</span>
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
          <td title={k.metric ?? "待补充量化指标"}>
            {k.metric ?? <span className="muted">待补充量化指标</span>}
          </td>
          <td>{k.canEdit && <span className="okr-row-action">编辑</span>}</td>
        </tr>
      );
    }),
  ]);
  const krTotal = objectives.reduce((n, o) => n + o.keyResults.length, 0);

  // #125：/okr 是总览页头进入的全页管理模式，仅项目负责人／项目管理员可用；
  // 无权限直接访问按权限挡回项目总览。
  if (!loading && project && !project.canEdit) {
    return <Navigate to={`/projects/${projectId}`} replace />;
  }
  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="项目总览"
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
                  <h1>管理 O/KR</h1>
                  <p>集中维护目标结构和责任信息；项目态势、风险与任务进度仍在项目总览查看。</p>
                </div>
                <div style={{ display: "flex", gap: 8 }}>
                  <Button onClick={() => navigate(`/projects/${projectId}`)}>返回项目总览</Button>
                  <Button onClick={() => setImportOpen(true)}>导入 O / KR 表格</Button>
                  <Button type="primary" onClick={() => setModalOpen(true)}>
                    ＋ 新增 O / KR
                  </Button>
                </div>
              </div>
              <section className="okr-management-note">
                <div>
                  <b>结构维护模式</b>
                  <span>点击任一 O 或 KR 行，在右侧编辑对应结构字段。</span>
                </div>
                <span>
                  {objectives.length} 个 O · {krTotal} 个 KR
                </span>
              </section>
              <div className="data-table-wrap okr-structure-wrap">
                <table className="data-table okr-structure-table">
                  <thead>
                    <tr>
                      <th style={{ width: 130 }}>编号</th>
                      <th>名称</th>
                      <th style={{ width: 80 }}>所属 O</th>
                      <th style={{ width: 150 }}>负责人</th>
                      <th style={{ width: 180 }}>周期</th>
                      <th style={{ width: 240 }}>量化标准</th>
                      <th style={{ width: 64 }} />
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
    target.kind === "O" ? "O 下还有 KR 时不能删除" : "KR 下还有任务（含已完成、已关闭）时不能删除";

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
  metric: string; // 量化指标（选填，#94）；O 行不用
  ownerId?: number;
  period?: [Dayjs | null, Dayjs | null] | null;
};

let rowSeq = 0;

// 已有 O 在弹窗里的编号预览：与 OKR 列表同序，列表本身消费的是后端的 code。
const oCodeOf = (objectives: Objective[], id: number) =>
  `O${objectives.findIndex((o) => o.id === id) + 1}`;

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
      const oRow: OkrRow = { key: ++rowSeq, kind: "O", title: "", description: "", metric: "" };
      const krRow: OkrRow = {
        key: ++rowSeq,
        kind: "KR",
        title: "",
        objRef: `new:${oRow.key}`,
        description: "",
        metric: "",
      };
      setRows([oRow, krRow]);
      setError(null);
    }
  }, [open]);

  // 新 O 行；新建后它自成一组，KR 由组末尾的按钮就地添加（#104）。
  const addObjective = () =>
    setRows((rs) => [...rs, { key: ++rowSeq, kind: "O", title: "", objRef: undefined, description: "", metric: "" }]);

  // 点哪一组就加到哪一组：归属由按钮所在的组给出，不再默认落到最后一个 O。
  const addKeyResult = (objRef: string) =>
    setRows((rs) => [...rs, { key: ++rowSeq, kind: "KR", title: "", objRef, description: "", metric: "" }]);

  const patch = (key: number, p: Partial<OkrRow>) =>
    setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...p } : r)));

  const removeRow = (key: number) =>
    setRows((rs) =>
      rs
        .filter((r) => r.key !== key)
        .map((r) => (r.objRef === `new:${key}` ? { ...r, objRef: undefined } : r)),
    );

  // 分组（#104）：本次新建的每个 O 一组、已有的每个 O 一组，最后是归属待定的孤儿行。
  // 组内顺序即 rows 里的顺序，也就是保存顺序。
  const oRows = rows.filter((r) => r.kind === "O");
  const krRows = rows.filter((r) => r.kind === "KR");
  const oSeqByRef = new Map<string, number>();
  objectives.forEach((o, i) => oSeqByRef.set(`existing:${o.id}`, i + 1));
  oRows.forEach((r, i) => oSeqByRef.set(`new:${r.key}`, objectives.length + i + 1));

  // 编号预览：O 接在已有 O 之后，KR 在所属 O 的现有 KR 之后接着排（AC-64 的展示口径）。
  const oCodeByKey = new Map<number, string>();
  oRows.forEach((r) => oCodeByKey.set(r.key, `O${oSeqByRef.get(`new:${r.key}`)}`));
  const krCodeByKey = new Map<number, string>();
  const krSeqByRef = new Map<string, number>();
  objectives.forEach((o) => krSeqByRef.set(`existing:${o.id}`, o.keyResults.length));
  for (const r of krRows) {
    if (!r.objRef) continue;
    const oSeq = oSeqByRef.get(r.objRef);
    if (oSeq === undefined) continue;
    const next = (krSeqByRef.get(r.objRef) ?? 0) + 1;
    krSeqByRef.set(r.objRef, next);
    krCodeByKey.set(r.key, `KR${oSeq}.${next}`);
  }

  const groups = [
    ...oRows.map((r) => ({ ref: `new:${r.key}`, oRow: r, title: "" })),
    ...objectives.map((o) => ({
      ref: `existing:${o.id}`,
      oRow: undefined,
      title: `${oCodeOf(objectives, o.id)} ${o.title}`,
    })),
  ];
  const orphanKrRows = krRows.filter((r) => !r.objRef || !oSeqByRef.has(r.objRef));

  const objRefOptions = [
    ...oRows.map((r) => ({
      value: `new:${r.key}`,
      label: `${oCodeByKey.get(r.key)}：${r.title.trim() || "（未命名）"}`,
    })),
    ...objectives.map((o, i) => ({ value: `existing:${o.id}`, label: `O${i + 1}：${o.title}` })),
  ];

  // KR 负责人承担关键字段变更与完成终审，访客担任会让审批链无人可推进（#95、§3.4）；
  // 与导入、编辑抽屉、任务各处的负责人选择同一口径，规则本身由域层兜底。
  const ownerOptions = members
    .filter((m) => m.role !== "viewer")
    .map((m) => ({
      value: m.userId,
      label: `${m.displayName}（${m.username}）`,
    }));

  const renderObjectiveRow = (r: OkrRow) => (
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
        <span className="sheet-placeholder">在 KR 上填</span>
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
  );

  const renderKeyResultRow = (r: OkrRow) => (
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
        <Input
          maxLength={100}
          placeholder="量化指标（选填）"
          value={r.metric}
          onChange={(e) => patch(r.key, { metric: e.target.value })}
        />
      </div>
      {/* 「所属 O」下拉保留做纠错：改归属后该行移动到目标组（#104）。 */}
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
  );

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
        metric: r.metric.trim() || undefined,
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
      // #125：保存后按本次涉及的 KR 负责人发站内通知（本人不收），提示含人数。
      message.success(
        res.data.notifiedCount > 0
          ? `O / KR 已保存，已通知 ${res.data.notifiedCount} 名负责人`
          : "O / KR 已保存；本次负责人均为你本人",
      );
      onSaved(res.data.objectives);
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
      width={1160}
      confirmLoading={saving}
      onOk={save}
      onCancel={onClose}
      okText="保存并通知负责人"
      cancelText="取消"
      destroyOnHidden
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <div className="notice">O、KR 仍由线下确定；这里仅用于一次性连续录入结构和负责人。</div>
      <div className="okr-sheet">
        <div className="okr-sheet-head">
          <span>目标 O</span>
          <span>关键结果 KR</span>
          <span>量化指标</span>
          <span>所属 O</span>
          <span>负责人</span>
          <span>周期</span>
          <span />
        </div>
        {/* 一个 O 一组（#104）：组头是 O 行（新建的可编辑、已有的只读），
            组末尾的「＋ 添加 KR」把新行加进本组，不再统一落到最后一个 O。 */}
        {groups.map((g) => (
          <div key={g.ref} className="okr-sheet-group">
            {g.oRow ? (
              renderObjectiveRow(g.oRow)
            ) : (
              <div className="okr-sheet-grouphead" title={g.title}>
                <b className="cell-text">{g.title}</b>
                <span className="muted">已有 O</span>
              </div>
            )}
            {krRows.filter((r) => r.objRef === g.ref).map(renderKeyResultRow)}
            <div className="okr-sheet-groupfoot">
              <Button size="small" type="text" onClick={() => addKeyResult(g.ref)}>
                ＋ 添加 KR
              </Button>
            </div>
          </div>
        ))}
        {/* 删掉一个新建 O 行后，它下面的 KR 不静默丢失，落到这里等重新指定归属。 */}
        {orphanKrRows.length > 0 && (
          <div className="okr-sheet-group">
            <div className="okr-sheet-grouphead">
              <b>待指定所属 O</b>
              <span className="muted">保存前需为每行选择所属 O</span>
            </div>
            {orphanKrRows.map(renderKeyResultRow)}
          </div>
        )}
      </div>
      <div className="okr-sheet-actions">
        <Button size="small" onClick={addObjective}>
          ＋ 添加 O 事项
        </Button>
      </div>
    </Modal>
  );
}

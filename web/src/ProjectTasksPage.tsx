import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import {
  Alert,
  Button,
  Input,
  Modal,
  Select,
  Spin,
  message,
} from "antd";
import type { Dayjs } from "dayjs";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";
import ProjectShell from "./ProjectShell";
import TaskImportModal from "./TaskImportModal";
import TaskDrawerHost from "./task-drawer";
import {
  STATUS_CLASS,
  fmtDate,
  type KrOption,
} from "./task-drawer/shared";
import DateRangeField from "./DateRangeField";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type TaskStatus = components["schemas"]["TaskStatus"];
type CreateTaskItem = components["schemas"]["CreateTaskItem"];
type ProjectMember = components["schemas"]["ProjectMember"];
type TaskInvite = components["schemas"]["TaskInvite"];

// 风险等级词表（与 OKR、总览、图谱、报告各页一致）；卡点类型名一律消费 API 的 kindLabel。

// 状态筛选下拉的选项词表（需要枚举全集，非行级显示；文案对齐原型 taskStatusOptions）。
// 行级状态显示一律消费 API 派生的 statusLabel（AC-04），不在前端推导。
const STATUS_FILTER_LABEL: Record<TaskStatus, string> = {
  draft: "草稿",
  pending_pool_review: "待负责人审批",
  not_started: "未开始",
  waiting_input: "等待输入",
  in_progress: "进行中",
  pending_intermediate_review: "待成果审核",
  pending_final_review: "待 KR 终审",
  completed: "已完成",
  cancelled: "已关闭",
};


// 全部任务列表的状态备注（#91）：退回理由、关闭原因、卡点与变更审批合成一行文本，
// alert 决定用红色还是弱化色；没有备注时返回 null，状态列只剩状态胶囊。
function statusNote(t: Task): { text: string; alert: boolean } | null {
  const parts: string[] = [];
  let alert = false;
  if (t.status === "draft" && t.poolReview?.status === "rejected" && t.poolReview.opinion) {
    parts.push(`退回：${t.poolReview.opinion}`);
  }
  if (t.status === "cancelled" && t.cancelReason) parts.push(`原因：${t.cancelReason}`);
  if (t.openBlockerCount != null && t.openBlockerCount > 0) {
    parts.push(`⚠ ${t.openBlockerCount} 个卡点`);
    alert = true;
  }
  if (t.fieldChange?.state === "pending") {
    const detail = t.fieldChange.changes
      .map((c) => `${c.label} ${c.oldValue || "—"}→${c.newValue}`)
      .join("；");
    parts.push((t.fieldChange.changeType === "cancel" ? "关闭审批中：" : "变更审批中：") + detail);
  }
  if (t.fieldChange?.state === "rejected" && !t.fieldChange.resolved) {
    parts.push(
      (t.fieldChange.changeType === "cancel" ? "关闭申请已退回" : "变更已退回") +
        (t.fieldChange.opinion ? `：${t.fieldChange.opinion}` : ""),
    );
    alert = true;
  }
  return parts.length > 0 ? { text: parts.join("　·　"), alert } : null;
}




export default function ProjectTasksPage({
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
  const [tasks, setTasks] = useState<Task[]>([]);
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [invites, setInvites] = useState<TaskInvite[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [inviteModalOpen, setInviteModalOpen] = useState(false);
  const [respondingInvite, setRespondingInvite] = useState<TaskInvite | null>(null);
  const [search, setSearch] = useState("");
  const [krFilter, setKrFilter] = useState<number | "all">("all");
  const [statusFilter, setStatusFilter] = useState<TaskStatus | "all">("all");

  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, objectivesRes, tasksRes, membersRes, invitesRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/objectives", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/tasks", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/members", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/task-invites", { params: { path: { projectId } } }),
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
    setTasks(tasksRes.data ?? []);
    setMembers(membersRes.data ?? []);
    setInvites(invitesRes.data ?? []);
    setLoading(false);
  }, [projectId, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  // 站内通知直达：/projects/:id/tasks?task=<id>&tab=discussion
  const [searchParams, setSearchParams] = useSearchParams();
  const focusTaskId = searchParams.get("task");
  const focusTab = searchParams.get("tab") ?? "overview";
  // 来源分组（我的工作卡片带入）：决定抽屉落位区块与进度是否可在概况内编辑（MW-22、§6.2）。
  const focusSource = searchParams.get("from") ?? "";
  useEffect(() => {
    if (focusTaskId && !loading) {
      setDrawerTaskId(Number(focusTaskId));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusTaskId, focusTab, loading]);

  // O／KR／任务编号都是持久字段（AC-64），前端只取不算。
  const krList = useMemo(() => objectives.flatMap((o) => o.keyResults.map((k) => ({ ...k }))), [objectives]);
  // 编号是持久字段（AC-64）：跨页一致、增删任务后不位移，前端只取不算。
  const taskCode = useMemo(() => new Map(tasks.map((t) => [t.id, t.code])), [tasks]);
  const filtered = tasks.filter((t) => {
    if (krFilter !== "all" && t.keyResultId !== krFilter) return false;
    if (statusFilter !== "all" && t.status !== statusFilter) return false;
    const q = search.trim().toLowerCase();
    if (!q) return true;
    return `${taskCode.get(t.id)}${t.name}${t.ownerName}`.toLowerCase().includes(q);
  });

  // #118：行级动作全部收进任务抽屉，列表只负责打开抽屉；退回／进度／取消弹窗由抽屉持有（#109）。
  const [importOpen, setImportOpen] = useState(false);
  const [drawerTaskId, setDrawerTaskId] = useState<number | null>(null);

  // 裁决 A2（#134）：批量入池／批量通过／批量退回彻底移除，逐条处理走任务抽屉。
  const groups = krList
    .map((kr) => ({ kr, list: filtered.filter((t) => t.keyResultId === kr.id) }))
    .filter((g) => g.list.length > 0);

  const rows = groups.flatMap(({ kr, list }) => [
    <tr key={`kr-${kr.id}`} className="table-group">
      <td colSpan={8}>
        <div className="task-group-label">
          <span title={`${kr.code} ${kr.description}`}>
            <b>{kr.code}</b>
            <span className="cell-text">{kr.description}</span>
            <span className="muted">{list.length} 项</span>
          </span>
          {kr.progressSummary && kr.progressSummary.totalTasks > 0 && (
            <span className="muted" style={{ fontWeight: 400 }}>
              {kr.progressSummary.averageProgress != null &&
                `平均 ${kr.progressSummary.averageProgress}%　·　`}
              其中 {kr.progressSummary.filledTasks}／{kr.progressSummary.totalTasks} 个任务由负责人填写，未填按 0 计入
            </span>
          )}
        </div>
      </td>
    </tr>,
    ...list.map((t) => (
      <tr key={t.id} className="task-table-row">
        <td className="mono">{taskCode.get(t.id)}</td>
        <td title={t.name}>
          <Button
            type="link"
            size="small"
            className="task-title-link"
            style={{ padding: 0 }}
            onClick={() => setDrawerTaskId(t.id)}
          >
            {t.name}
          </Button>
        </td>
        <td title={t.ownerName}>
          <span className="owner-cell">
            <span className="avatar">{t.ownerName.slice(0, 1)}</span>
            <span className="cell-text">{t.ownerName}</span>
          </span>
        </td>
        {/* 状态备注（退回理由、关闭原因、卡点、变更审批）与状态胶囊同排一行（#91）：
            行高恒定，超长在列宽处省略，完整内容悬停看 title，明细仍在任务抽屉里。 */}
        <td title={statusNote(t)?.text}>
          <span className={`status-pill ${STATUS_CLASS[t.status]}`}>{t.statusLabel}</span>
          {(() => {
            const note = statusNote(t);
            return note ? (
              <span
                className={`cell-note${note.alert ? "" : " muted"}`}
                style={note.alert ? { color: "var(--red)" } : undefined}
              >
                {note.text}
              </span>
            ) : null;
          })()}
        </td>
        <td>
          {t.progress != null ? (
            <div className="task-progress-cell">
              <div className="progress">
                <i style={{ width: `${t.progress}%` }} />
              </div>
              <span className="muted" style={{ fontSize: 12 }}>
                {t.progress}%
              </span>
            </div>
          ) : (
            <span className="muted">—</span>
          )}
        </td>
        <td className="task-date">{fmtDate(t.startDate)}</td>
        <td className="task-date">{fmtDate(t.endDate)}</td>
        <td
          className="task-output"
          title={t.deliverableNames && t.deliverableNames.length > 0 ? t.deliverableNames.join("、") : undefined}
        >
          {t.deliverableNames && t.deliverableNames.length > 0 ? (
            t.deliverableNames.join("、")
          ) : (
            <span className="muted">—</span>
          )}
        </td>
      </tr>
    )),
  ]);

  const canCreate = members.some((m) => m.userId === user.id && m.role !== "viewer") || !!project?.canEdit;
  // 邀请入口：项目管理员/负责人，或本人负责任一 KR（domain CanInviteForKr 的前端投影，仅控制入口显隐）。
  const canInvite = !!project?.canEdit || krList.some((k) => k.ownerId === user.id);
  const myPendingInvites = invites.filter((iv) => iv.canHandle);

  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="全部任务"
      onLogout={onLogout}
    >
      {notFound ? (
        <Alert type="error" message="项目不存在" description={<Link to="/">返回项目列表</Link>} />
      ) : loading || !project ? (
        <Spin />
      ) : (
        <>
          <div className="page-head">
            <div>
              <h1>全部任务</h1>
              <p>按 O / KR 组织三级任务；任务创建后先提交入池审批，通过后进入执行池。</p>
            </div>
            <div style={{ display: "flex", gap: 8 }}>
              {/* 任务批量导入（AC-02b、#107）：入口只对项目负责人与项目管理员开放，
                  显隐取 project.canEdit 这个派生字段，规则本身在域层 CanImportTasks。 */}
              {project?.canEdit && (
                <Button onClick={() => setImportOpen(true)}>批量导入任务</Button>
              )}
              {canInvite && (
                <Button onClick={() => setInviteModalOpen(true)}>邀请负责人完善</Button>
              )}
              {canCreate && (
                <Button type="primary" onClick={() => setModalOpen(true)}>
                  ＋ 创建任务
                </Button>
              )}
            </div>
          </div>
          {myPendingInvites.length > 0 && (
            <div className="notice" style={{ marginBottom: 12 }}>
              {myPendingInvites.map((iv) => {
                const kr = krList.find((k) => k.id === iv.keyResultId);
                return (
                  <div
                    key={iv.id}
                    style={{ display: "flex", alignItems: "center", gap: 8, minHeight: 28 }}
                  >
                    <b>
                      {kr?.code ?? "KR"} 任务创建邀请
                    </b>
                    <span>
                      {iv.inviterName} 邀请你在「{kr?.description ?? "该 KR"}」下创建任务
                      {iv.note ? `：${iv.note}` : ""}
                    </span>
                    <Button size="small" onClick={() => setRespondingInvite(iv)}>
                      响应邀请
                    </Button>
                  </div>
                );
              })}
            </div>
          )}
          <div className="toolbar">
            <div className="toolbar-group">
              <Input
                allowClear
                prefix={<Icon name="search" size={15} />}
                style={{ width: 240 }}
                placeholder="搜索任务、编号或负责人"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              <Select
                style={{ width: 160 }}
                value={krFilter}
                onChange={setKrFilter}
                options={[
                  { value: "all" as const, label: "全部 KR" },
                  ...krList.map((k) => ({ value: k.id, label: k.code })),
                ]}
              />
              <Select
                style={{ width: 150 }}
                value={statusFilter}
                onChange={setStatusFilter}
                options={[
                  { value: "all" as const, label: "全部状态" },
                  ...(Object.keys(STATUS_FILTER_LABEL) as TaskStatus[]).map((s) => ({
                    value: s,
                    label: STATUS_FILTER_LABEL[s],
                  })),
                ]}
              />
            </div>
          </div>
          <div className="data-table-wrap task-table-wrap">
            <table className="data-table task-table">
              <thead>
                <tr>
                  {/* 列宽按 1920×1080 校准（#91）：固定列吃满各自内容，剩余宽度全给任务名。 */}
                  <th style={{ width: 56 }}>编号</th>
                  <th>任务</th>
                  <th style={{ width: 110 }}>负责人</th>
                  <th style={{ width: 200 }}>状态</th>
                  <th style={{ width: 140 }}>进度</th>
                  <th style={{ width: 70 }}>开始</th>
                  <th style={{ width: 70 }}>截止</th>
                  <th style={{ width: 190 }}>预期交付物</th>
                </tr>
              </thead>
              <tbody>
                {rows.length > 0 ? (
                  rows
                ) : (
                  <tr>
                    <td colSpan={8}>
                      <div className="empty">没有匹配任务</div>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </>
      )}
      <CreateTaskModal
        open={modalOpen || !!respondingInvite}
        projectId={projectId}
        krList={krList}
        members={members}
        currentUserId={user.id}
        invite={respondingInvite}
        onClose={() => {
          setModalOpen(false);
          setRespondingInvite(null);
        }}
        onSaved={() => {
          setModalOpen(false);
          setRespondingInvite(null);
          load();
        }}
      />
      <TaskImportModal
        open={importOpen}
        projectId={projectId}
        members={members}
        krList={krList}
        onClose={() => setImportOpen(false)}
        onImported={() => {
          setImportOpen(false);
          load();
        }}
      />
      <InviteOwnersModal
        open={inviteModalOpen}
        projectId={projectId}
        krList={krList.filter((k) => !!project?.canEdit || k.ownerId === user.id)}
        members={members}
        currentUserId={user.id}
        onClose={() => setInviteModalOpen(false)}
        onSent={(latest) => {
          setInvites(latest);
          setInviteModalOpen(false);
          message.success("邀请已发出，受邀成员将在其任务页看到该邀请");
        }}
      />
      {/* 任务详情抽屉抽成独立模块（#109）：宿主只给 projectId、要打开的 taskId、
          初始 Tab 与关闭回调；抽屉自己取数、自己刷新，动作落库后回调宿主刷新列表。 */}
      <TaskDrawerHost
        projectId={projectId}
        taskId={drawerTaskId}
        initialTab={drawerTaskId === Number(focusTaskId) ? focusTab : "overview"}
        source={drawerTaskId === Number(focusTaskId) ? focusSource : ""}
        onClose={() => {
          setDrawerTaskId(null);
          if (focusTaskId) setSearchParams({}, { replace: true });
        }}
        onChanged={load}
      />
    </ProjectShell>
  );
}



type TaskRow = {
  key: number;
  keyResultId?: number;
  name: string;
  ownerId?: number;
  period?: [Dayjs | null, Dayjs | null] | null;
  outputName: string;
};

let taskRowSeq = 0;

function CreateTaskModal({
  open,
  projectId,
  krList,
  members,
  currentUserId,
  invite,
  onClose,
  onSaved,
}: {
  open: boolean;
  projectId: number;
  krList: KrOption[];
  members: ProjectMember[];
  currentUserId: number;
  invite?: TaskInvite | null;
  onClose: () => void;
  onSaved: (latest: Task[]) => void;
}) {
  const [rows, setRows] = useState<TaskRow[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const newRow = (): TaskRow => ({
    key: ++taskRowSeq,
    keyResultId: invite ? invite.keyResultId : krList[0]?.id,
    name: "",
    outputName: "",
    ownerId: members.some((m) => m.userId === currentUserId && m.role !== "viewer")
      ? currentUserId
      : undefined,
  });

  useEffect(() => {
    if (open) {
      setRows([newRow()]);
      setError(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const patch = (key: number, p: Partial<TaskRow>) =>
    setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...p } : r)));

  const krOptions = krList.map((k) => ({ value: k.id, label: `${k.code} · ${k.description}` }));
  // 负责人列按约 4 个汉字的基础输入宽度（PRD §7.3、AC-54），因此选项只显示姓名；
  // 账号名不进标签但仍参与姓名匹配（同名成员靠账号名区分）。
  const ownerOptions = members
    .filter((m) => m.role !== "viewer")
    .map((m) => ({ value: m.userId, label: m.displayName, username: m.username }));

  const save = async () => {
    if (rows.length === 0) {
      setError("请至少录入一项任务");
      return;
    }
    const items: CreateTaskItem[] = [];
    for (const r of rows) {
      if (!r.keyResultId) {
        setError("每项任务都要选择所属 KR");
        return;
      }
      if (!r.name.trim()) {
        setError("任务名称不能为空");
        return;
      }
      if (!r.ownerId) {
        setError("每项任务都要指定负责人");
        return;
      }
      if (!r.period?.[0] || !r.period?.[1]) {
        setError("每项任务都要填写开始与截止时间");
        return;
      }
      items.push({
        keyResultId: r.keyResultId,
        name: r.name.trim(),
        ownerId: r.ownerId,
        startDate: r.period[0].format("YYYY-MM-DD"),
        endDate: r.period[1].format("YYYY-MM-DD"),
        expectedDeliverable: r.outputName.trim() || undefined,
      });
    }
    setSaving(true);
    setError(null);
    const res = await client.POST("/projects/{projectId}/tasks", {
      params: { path: { projectId } },
      body: { items, submitForReview: true, taskInviteId: invite?.id },
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
          {invite ? "响应任务创建邀请" : "创建任务"}
          <span className="modal-sub">
            {invite
              ? `${invite.inviterName} 发起 · 通过本邀请提交关联任务后邀请退出`
              : "按 KR 连续录入任务骨架并指定负责人"}
          </span>
        </div>
      }
      open={open}
      width={1080}
      confirmLoading={saving}
      onOk={save}
      onCancel={onClose}
      okText="提交入池审批"
      cancelText="取消"
      destroyOnHidden
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      {invite && (
        <div className="notice" style={{ marginBottom: 12 }}>
          <b>任务创建邀请</b>
          {invite.note ? `：${invite.note}` : ""}
          <div className="muted" style={{ fontSize: 12 }}>
            只有通过本邀请提交关联任务，邀请才会退出。
          </div>
        </div>
      )}
      <div className="task-sheet">
        <div className="task-sheet-head">
          <span>所属 KR</span>
          <span>任务名称</span>
          <span>负责人</span>
          <span>任务周期</span>
          <span>预期交付物</span>
          <span />
        </div>
        {rows.map((r) => (
          <div key={r.key} className="task-sheet-row">
            <div className="task-sheet-cell">
              <Select
                style={{ width: "100%" }}
                options={krOptions}
                value={r.keyResultId}
                onChange={(v) => patch(r.key, { keyResultId: v })}
                placeholder="所属 KR"
              />
            </div>
            <div className="task-sheet-cell">
              <Input
                maxLength={200}
                placeholder="任务名称"
                value={r.name}
                onChange={(e) => patch(r.key, { name: e.target.value })}
              />
            </div>
            <div className="task-sheet-cell">
              <Select
                style={{ width: "100%" }}
                options={ownerOptions}
                value={r.ownerId}
                onChange={(v) => patch(r.key, { ownerId: v })}
                showSearch
                filterOption={(input, option) =>
                  `${option?.label ?? ""}${option?.username ?? ""}`
                    .toLowerCase()
                    .includes(input.toLowerCase())
                }
                placeholder="负责人"
              />
            </div>
            <div className="task-sheet-cell">
              <DateRangeField value={r.period} onChange={(v) => patch(r.key, { period: v })} />
            </div>
            <div className="task-sheet-cell">
              <Input
                maxLength={100}
                placeholder="预期交付物（选填）"
                value={r.outputName}
                onChange={(e) => patch(r.key, { outputName: e.target.value })}
              />
            </div>
            <Button
              type="text"
              size="small"
              onClick={() => setRows((rs) => rs.filter((x) => x.key !== r.key))}
              aria-label="删除该任务行"
            >
              ✕
            </Button>
          </div>
        ))}
      </div>
      <div className="task-sheet-actions">
        <Button size="small" onClick={() => setRows((rs) => [...rs, newRow()])}>
          ＋ 继续添加任务
        </Button>
        <span className="muted" style={{ fontSize: 12 }}>
          可连续录入多项任务，保存后统一提交各自所属 KR 的入池审批。
        </span>
      </div>
      <div className="notice" style={{ marginTop: 12 }}>
        任务提交后处于“待入池审批”；所属 KR 负责人通过后，才进入执行池并变为“未开始”。KR
        负责人在本人负责的 KR 下创建的任务免审，保存后直接进入“未开始”。
      </div>
    </Modal>
  );
}

function InviteOwnersModal({
  open,
  projectId,
  krList,
  members,
  currentUserId,
  onClose,
  onSent,
}: {
  open: boolean;
  projectId: number;
  krList: KrOption[];
  members: ProjectMember[];
  currentUserId: number;
  onClose: () => void;
  onSent: (latest: TaskInvite[]) => void;
}) {
  const [keyResultId, setKeyResultId] = useState<number | undefined>(undefined);
  const [selected, setSelected] = useState<number[]>([]);
  const [note, setNote] = useState("请结合你负责的工作，在该 KR 下补充需要推进的任务。");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) {
      setKeyResultId(krList[0]?.id);
      setSelected([]);
      setError(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // 候选受邀成员：非只读、非本人（原型 inviteMemberCandidates）。
  const candidates = members.filter((m) => m.role !== "viewer" && m.userId !== currentUserId);

  const send = async () => {
    if (!keyResultId) {
      setError("请选择邀请对应的 KR");
      return;
    }
    if (selected.length === 0) {
      setError("请至少选择一名受邀成员");
      return;
    }
    setSaving(true);
    setError(null);
    const res = await client.POST("/projects/{projectId}/task-invites", {
      params: { path: { projectId } },
      body: {
        keyResultId,
        inviteeIds: selected,
        note: note.trim() || undefined,
      },
    });
    setSaving(false);
    if (res.data) {
      onSent(res.data);
    } else {
      setError(res.error?.message ?? "发送失败");
    }
  };

  return (
    <Modal
      title={
        <div>
          邀请成员创建任务
          <span className="modal-sub">KR 负责人可以在任务尚未建立时先发出邀请</span>
        </div>
      }
      open={open}
      width={900}
      confirmLoading={saving}
      onOk={send}
      onCancel={onClose}
      okText="发送邀请"
      cancelText="取消"
      destroyOnHidden
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <div style={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr)", gap: 12 }}>
        <div style={{ minWidth: 0 }}>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
            邀请成员为哪个 KR 创建任务
          </div>
          <Select
            style={{ width: "100%" }}
            value={keyResultId}
            onChange={setKeyResultId}
            options={krList.map((k) => ({ value: k.id, label: `${k.code} · ${k.description}` }))}
            placeholder="选择 KR"
          />
        </div>
        <div>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
            选择受邀成员（可多选）
          </div>
          {/* #126：不再按角色分组、不用树形穿梭框——扁平多选，一人一行「头像 姓名（用户名）」。 */}
          <Select
            mode="multiple"
            style={{ width: "100%" }}
            placeholder="搜索姓名或用户名"
            value={selected}
            onChange={setSelected}
            optionFilterProp="label"
            options={candidates.map((m) => ({
              value: m.userId,
              label: `${m.displayName}（${m.username}）`,
            }))}
            optionRender={(opt) => {
              const m = candidates.find((c) => c.userId === opt.value);
              return (
                <span className="owner-cell">
                  <span className="avatar">{m?.displayName.slice(0, 1)}</span>
                  <span className="cell-text">
                    {m?.displayName}（{m?.username}）
                  </span>
                </span>
              );
            }}
          />
        </div>
        <div>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
            邀请说明
          </div>
          <Input.TextArea rows={3} maxLength={500} value={note} onChange={(e) => setNote(e.target.value)} />
        </div>
        <div className="notice">
          邀请不依赖现有任务。发送后，右侧已选成员会在其「全部任务」页看到带 KR 上下文的邀请。
        </div>
      </div>
    </Modal>
  );
}


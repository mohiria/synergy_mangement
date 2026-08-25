import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Button, DatePicker, Input, Modal, Select, Spin, message } from "antd";
import type { Dayjs } from "dayjs";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type TaskStatus = components["schemas"]["TaskStatus"];
type CreateTaskItem = components["schemas"]["CreateTaskItem"];
type ProjectMember = components["schemas"]["ProjectMember"];

// 任务生命周期状态的中文标签与徽章样式（PRD §5.1；配色按原型 statusClass 规则）。
const STATUS_LABEL: Record<TaskStatus, string> = {
  draft: "草稿",
  pending_pool_review: "待入池审批",
  not_started: "未开始",
  waiting_input: "等待输入",
  in_progress: "进行中",
  pending_intermediate_review: "待中间审核",
  pending_final_review: "待 KR 终审",
  completed: "已完成",
  cancelled: "已取消",
};
const STATUS_CLASS: Record<TaskStatus, string> = {
  draft: "",
  pending_pool_review: "warning",
  not_started: "",
  waiting_input: "warning",
  in_progress: "in_progress",
  pending_intermediate_review: "review",
  pending_final_review: "review",
  completed: "completed",
  cancelled: "",
};

const fmtDate = (d?: string | null) => (d ? d.slice(5).replace("-", ".") : "—");

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
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [krFilter, setKrFilter] = useState<number | "all">("all");
  const [statusFilter, setStatusFilter] = useState<TaskStatus | "all">("all");

  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, objectivesRes, tasksRes, membersRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/objectives", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/tasks", { params: { path: { projectId } } }),
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
    setTasks(tasksRes.data ?? []);
    setMembers(membersRes.data ?? []);
    setLoading(false);
  }, [projectId, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  // KR 展示编号沿全项目顺序派生（与 OKR 页一致），任务编号按 id 顺序派生 T1…。
  const krList = useMemo(() => {
    let seq = 0;
    return objectives.flatMap((o) =>
      o.keyResults.map((k) => ({ ...k, code: `KR${++seq}` })),
    );
  }, [objectives]);
  const taskCode = useMemo(() => {
    const sorted = [...tasks].sort((a, b) => a.id - b.id);
    return new Map(sorted.map((t, i) => [t.id, `T${i + 1}`]));
  }, [tasks]);

  const filtered = tasks.filter((t) => {
    if (krFilter !== "all" && t.keyResultId !== krFilter) return false;
    if (statusFilter !== "all" && t.status !== statusFilter) return false;
    const q = search.trim().toLowerCase();
    if (!q) return true;
    return `${taskCode.get(t.id)}${t.name}${t.ownerName}`.toLowerCase().includes(q);
  });

  const submitPool = async (task: Task) => {
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/submit-pool-review", {
      params: { path: { projectId, taskId: task.id } },
    });
    if (res.data) {
      message.success("已提交所属 KR 负责人入池审批");
      load();
    } else {
      message.error(res.error?.message ?? "提交失败");
    }
  };

  const decidePool = async (task: Task, decision: "approved" | "rejected", opinion?: string) => {
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/pool-review-decision", {
      params: { path: { projectId, taskId: task.id } },
      body: { decision, opinion },
    });
    if (res.data) {
      message.success(decision === "approved" ? "已通过，任务进入未开始" : "已退回，任务回到草稿");
      load();
    } else {
      message.error(res.error?.message ?? "处理失败");
    }
  };

  const [rejectTask, setRejectTask] = useState<Task | null>(null);
  const [rejectOpinion, setRejectOpinion] = useState("");

  const groups = krList
    .map((kr) => ({ kr, list: filtered.filter((t) => t.keyResultId === kr.id) }))
    .filter((g) => g.list.length > 0);

  const rows = groups.flatMap(({ kr, list }) => [
    <tr key={`kr-${kr.id}`} className="table-group">
      <td colSpan={9}>
        {kr.code}　{kr.description}　<span className="muted">{list.length} 项</span>
      </td>
    </tr>,
    ...list.map((t) => (
      <tr key={t.id}>
        <td className="mono">{taskCode.get(t.id)}</td>
        <td>{t.name}</td>
        <td>
          <span className="owner-cell">
            <span className="avatar">{t.ownerName.slice(0, 1)}</span>
            {t.ownerName}
          </span>
        </td>
        <td>
          <span className={`status-pill ${STATUS_CLASS[t.status]}`}>{STATUS_LABEL[t.status]}</span>
          {t.status === "draft" && t.poolReview?.status === "rejected" && t.poolReview.opinion && (
            <div className="muted" style={{ fontSize: 12, marginTop: 2 }}>
              退回：{t.poolReview.opinion}
            </div>
          )}
        </td>
        <td className="muted">—</td>
        <td>{fmtDate(t.startDate)}</td>
        <td>{fmtDate(t.endDate)}</td>
        <td className="muted">—</td>
        <td>
          <div className="row-actions">
            {t.canSubmitPoolReview && (
              <Button size="small" onClick={() => submitPool(t)}>
                提交入池
              </Button>
            )}
            {t.canDecidePoolReview && (
              <>
                <Button size="small" type="primary" onClick={() => decidePool(t, "approved")}>
                  通过
                </Button>
                <Button
                  size="small"
                  danger
                  onClick={() => {
                    setRejectTask(t);
                    setRejectOpinion("");
                  }}
                >
                  退回
                </Button>
              </>
            )}
          </div>
        </td>
      </tr>
    )),
  ]);

  const canCreate = members.some((m) => m.userId === user.id && m.role !== "viewer") || !!project?.canEdit;

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
            {canCreate && (
              <Button type="primary" onClick={() => setModalOpen(true)}>
                ＋ 创建任务
              </Button>
            )}
          </div>
          <div className="toolbar">
            <div className="toolbar-group">
              <Input
                allowClear
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
                  ...(Object.keys(STATUS_LABEL) as TaskStatus[]).map((s) => ({
                    value: s,
                    label: STATUS_LABEL[s],
                  })),
                ]}
              />
            </div>
          </div>
          <div className="data-table-wrap">
            <table className="data-table">
              <thead>
                <tr>
                  <th style={{ width: 60 }}>编号</th>
                  <th>任务</th>
                  <th style={{ width: 130 }}>负责人</th>
                  <th style={{ width: 130 }}>状态</th>
                  <th style={{ width: 80 }}>可选进度</th>
                  <th style={{ width: 80 }}>开始</th>
                  <th style={{ width: 80 }}>截止</th>
                  <th style={{ width: 110 }}>预期交付物</th>
                  <th style={{ width: 170 }} />
                </tr>
              </thead>
              <tbody>
                {rows.length > 0 ? (
                  rows
                ) : (
                  <tr>
                    <td colSpan={9}>
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
        open={modalOpen}
        projectId={projectId}
        krList={krList}
        members={members}
        currentUserId={user.id}
        onClose={() => setModalOpen(false)}
        onSaved={(latest) => {
          setTasks(latest);
          setModalOpen(false);
        }}
      />
      <Modal
        title="退回入池申请"
        open={!!rejectTask}
        okText="确认退回"
        cancelText="取消"
        okButtonProps={{ danger: true }}
        onCancel={() => setRejectTask(null)}
        onOk={async () => {
          if (rejectTask) {
            await decidePool(rejectTask, "rejected", rejectOpinion.trim() || undefined);
          }
          setRejectTask(null);
        }}
      >
        <p className="muted" style={{ marginTop: 0 }}>
          退回后任务回到草稿，提交人可修改后重新提交。
        </p>
        <Input.TextArea
          rows={3}
          maxLength={500}
          placeholder="退回意见（选填）"
          value={rejectOpinion}
          onChange={(e) => setRejectOpinion(e.target.value)}
        />
      </Modal>
    </ProjectShell>
  );
}

type KrOption = { id: number; code: string; description: string; ownerId?: number | null };

type TaskRow = {
  key: number;
  keyResultId?: number;
  name: string;
  ownerId?: number;
  period?: [Dayjs | null, Dayjs | null] | null;
};

let taskRowSeq = 0;

function CreateTaskModal({
  open,
  projectId,
  krList,
  members,
  currentUserId,
  onClose,
  onSaved,
}: {
  open: boolean;
  projectId: number;
  krList: KrOption[];
  members: ProjectMember[];
  currentUserId: number;
  onClose: () => void;
  onSaved: (latest: Task[]) => void;
}) {
  const [rows, setRows] = useState<TaskRow[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const newRow = (): TaskRow => ({
    key: ++taskRowSeq,
    keyResultId: krList[0]?.id,
    name: "",
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
  const ownerOptions = members
    .filter((m) => m.role !== "viewer")
    .map((m) => ({ value: m.userId, label: `${m.displayName}（${m.username}）` }));

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
      });
    }
    setSaving(true);
    setError(null);
    const res = await client.POST("/projects/{projectId}/tasks", {
      params: { path: { projectId } },
      body: { items, submitForReview: true },
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
          创建任务
          <span className="modal-sub">按 KR 连续录入任务骨架并指定负责人</span>
        </div>
      }
      open={open}
      width={1080}
      confirmLoading={saving}
      onOk={save}
      onCancel={onClose}
      okText="提交入池审批"
      cancelText="取消"
      destroyOnClose
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <div className="task-sheet">
        <div className="task-sheet-head">
          <span>所属 KR</span>
          <span>任务名称</span>
          <span>负责人</span>
          <span>开始 / 截止</span>
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
                optionFilterProp="label"
                placeholder="任务负责人"
              />
            </div>
            <div className="task-sheet-cell">
              <DatePicker.RangePicker
                style={{ width: "100%" }}
                value={r.period ?? undefined}
                onChange={(v) => patch(r.key, { period: v })}
                placeholder={["开始", "截止"]}
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

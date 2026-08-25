import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import {
  Alert,
  Button,
  DatePicker,
  Drawer,
  Input,
  InputNumber,
  Mentions,
  Modal,
  Select,
  Spin,
  Tabs,
  Transfer,
  message,
} from "antd";
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
type TaskInvite = components["schemas"]["TaskInvite"];
type TaskDetail = components["schemas"]["TaskDetail"];
type EdgeType = components["schemas"]["EdgeType"];

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

const EDGE_TYPE_LABEL: Record<EdgeType, string> = {
  hard_prerequisite: "硬前置交付",
  information: "信息输入",
  handover: "正式成果接收",
  feedback: "迭代／反馈",
};

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
  useEffect(() => {
    if (focusTaskId && !loading) {
      setDrawerTaskId(Number(focusTaskId));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusTaskId, loading]);

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
  const [drawerTaskId, setDrawerTaskId] = useState<number | null>(null);
  const [editTask, setEditTask] = useState<Task | null>(null);
  const [fcReject, setFcReject] = useState<{ task: Task; changeId: number } | null>(null);
  const [fcRejectOpinion, setFcRejectOpinion] = useState("");

  const decideFieldChange = async (
    task: Task,
    changeId: number,
    decision: "approved" | "rejected",
    opinion?: string,
  ) => {
    const res = await client.POST(
      "/projects/{projectId}/tasks/{taskId}/field-changes/{changeId}/decision",
      {
        params: { path: { projectId, taskId: task.id, changeId } },
        body: { decision, opinion },
      },
    );
    if (res.data) {
      message.success(decision === "approved" ? "已通过，新值生效" : "已退回，拟议值作废");
      load();
    } else {
      message.error(res.error?.message ?? "处理失败");
    }
  };

  const abandonFieldChange = async (task: Task, changeId: number) => {
    const res = await client.POST(
      "/projects/{projectId}/tasks/{taskId}/field-changes/{changeId}/abandon",
      { params: { path: { projectId, taskId: task.id, changeId } } },
    );
    if (res.data) {
      message.success("已放弃本次变更");
      load();
    } else {
      message.error(res.error?.message ?? "操作失败");
    }
  };
  const [inputTask, setInputTask] = useState<Task | null>(null);
  const [reviewerTask, setReviewerTask] = useState<Task | null>(null);

  const removeEdge = async (edgeId: number) => {
    const res = await client.DELETE("/projects/{projectId}/edges/{edgeId}", {
      params: { path: { projectId, edgeId } },
    });
    if (res.response.ok) {
      message.success("已解除该输入关系");
      load();
    } else {
      message.error(res.error?.message ?? "解除失败");
    }
  };
  const [completionTask, setCompletionTask] = useState<Task | null>(null);
  const [completionNote, setCompletionNote] = useState("");
  const [crReject, setCrReject] = useState<{ task: Task; reviewId: number } | null>(null);
  const [crRejectOpinion, setCrRejectOpinion] = useState("");

  const submitCompletion = async (task: Task, note: string) => {
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/completion-reviews", {
      params: { path: { projectId, taskId: task.id } },
      body: { note },
    });
    if (res.data) {
      message.success("完成申请已提交，进入待 KR 终审");
      load();
    } else {
      message.error(res.error?.message ?? "提交失败");
    }
  };

  const decideCompletion = async (
    task: Task,
    reviewId: number,
    decision: "approved" | "rejected",
    opinion?: string,
  ) => {
    const res = await client.POST(
      "/projects/{projectId}/tasks/{taskId}/completion-reviews/{reviewId}/decision",
      {
        params: { path: { projectId, taskId: task.id, reviewId } },
        body: { decision, opinion },
      },
    );
    if (res.data) {
      message.success(
        decision === "approved"
          ? "终审通过，候选内容已覆盖当前交付物，任务完成"
          : "已退回，候选文件删除，任务回到进行中",
      );
      load();
    } else {
      message.error(res.error?.message ?? "处理失败");
    }
  };
  const [cancelTask, setCancelTask] = useState<Task | null>(null);
  const [cancelReason, setCancelReason] = useState("");
  const [progressTask, setProgressTask] = useState<Task | null>(null);
  const [progressValue, setProgressValue] = useState<number | null>(null);

  const startTask = async (task: Task) => {
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/update-status", {
      params: { path: { projectId, taskId: task.id } },
      body: { status: "in_progress" },
    });
    if (res.data) {
      message.success("已开始执行");
      load();
    } else {
      message.error(res.error?.message ?? "操作失败");
    }
  };

  const doCancelTask = async (task: Task, reason: string) => {
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/update-status", {
      params: { path: { projectId, taskId: task.id } },
      body: { status: "cancelled", reason },
    });
    if (res.data) {
      message.success("任务已取消");
      load();
    } else {
      message.error(res.error?.message ?? "操作失败");
    }
  };

  const saveProgress = async (task: Task, progress: number | null) => {
    const res = await client.PUT("/projects/{projectId}/tasks/{taskId}/progress", {
      params: { path: { projectId, taskId: task.id } },
      body: progress === null ? {} : { progress },
    });
    if (res.data) {
      message.success(progress === null ? "已清除进度" : "进度已更新");
      load();
    } else {
      message.error(res.error?.message ?? "操作失败");
    }
  };

  const groups = krList
    .map((kr) => ({ kr, list: filtered.filter((t) => t.keyResultId === kr.id) }))
    .filter((g) => g.list.length > 0);

  const rows = groups.flatMap(({ kr, list }) => [
    <tr key={`kr-${kr.id}`} className="table-group">
      <td colSpan={9}>
        {kr.code}　{kr.description}　<span className="muted">{list.length} 项</span>
        {kr.progressSummary && kr.progressSummary.totalTasks > 0 && (
          <span className="muted" style={{ fontWeight: 400 }}>
            　·　{kr.progressSummary.filledTasks}／{kr.progressSummary.totalTasks} 个任务已填写进度
            {kr.progressSummary.averageProgress != null &&
              `，平均 ${kr.progressSummary.averageProgress}%`}
          </span>
        )}
      </td>
    </tr>,
    ...list.map((t) => (
      <tr key={t.id}>
        <td className="mono">{taskCode.get(t.id)}</td>
        <td>
          <Button type="link" size="small" style={{ padding: 0 }} onClick={() => setDrawerTaskId(t.id)}>
            {t.name}
          </Button>
        </td>
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
          {t.status === "cancelled" && t.cancelReason && (
            <div className="muted" style={{ fontSize: 12, marginTop: 2 }}>
              原因:{t.cancelReason}
            </div>
          )}
          {t.fieldChange?.state === "pending" && (
            <div className="muted" style={{ fontSize: 12, marginTop: 2 }}>
              变更审批中：
              {t.fieldChange.changes.map((c) => `${c.label} ${c.oldValue || "—"}→${c.newValue}`).join("；")}
            </div>
          )}
          {t.fieldChange?.state === "rejected" && !t.fieldChange.resolved && (
            <div style={{ fontSize: 12, marginTop: 2, color: "var(--red)" }}>
              变更已退回{t.fieldChange.opinion ? `：${t.fieldChange.opinion}` : ""}
            </div>
          )}
        </td>
        <td>
          {t.progress != null ? (
            <div>
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
        <td>{fmtDate(t.startDate)}</td>
        <td>{fmtDate(t.endDate)}</td>
        <td>
          {t.deliverableNames && t.deliverableNames.length > 0 ? (
            t.deliverableNames.join("、")
          ) : (
            <span className="muted">—</span>
          )}
        </td>
        <td>
          <div className="row-actions">
            {t.canStart && (
              <Button size="small" onClick={() => startTask(t)}>
                开始
              </Button>
            )}
            {t.canUpdateProgress && (
              <Button
                size="small"
                onClick={() => {
                  setProgressTask(t);
                  setProgressValue(t.progress ?? null);
                }}
              >
                进度
              </Button>
            )}
            {t.canCancel && (
              <Button
                size="small"
                onClick={() => {
                  setCancelTask(t);
                  setCancelReason("");
                }}
              >
                取消
              </Button>
            )}
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
      <TaskDrawer
        projectId={projectId}
        task={tasks.find((t) => t.id === drawerTaskId) ?? null}
        code={drawerTaskId != null ? taskCode.get(drawerTaskId) : undefined}
        members={members}
        initialTab={drawerTaskId === Number(focusTaskId) ? focusTab : "overview"}
        onClose={() => {
          setDrawerTaskId(null);
          if (focusTaskId) setSearchParams({}, { replace: true });
        }}
        actions={{
          start: startTask,
          submitPool,
          approvePool: (t) => decidePool(t, "approved"),
          openReject: (t) => {
            setRejectTask(t);
            setRejectOpinion("");
          },
          openCancel: (t) => {
            setCancelTask(t);
            setCancelReason("");
          },
          openProgress: (t) => {
            setProgressTask(t);
            setProgressValue(t.progress ?? null);
          },
          openEdit: (t) => setEditTask(t),
          approveFieldChange: (t, id) => decideFieldChange(t, id, "approved"),
          openFcReject: (t, id) => {
            setFcReject({ task: t, changeId: id });
            setFcRejectOpinion("");
          },
          abandonFieldChange,
          openSubmitCompletion: (t) => {
            setCompletionTask(t);
            setCompletionNote("");
          },
          approveCompletion: (t, id) => decideCompletion(t, id, "approved"),
          openCrReject: (t, id) => {
            setCrReject({ task: t, reviewId: id });
            setCrRejectOpinion("");
          },
          openConfigureInput: (t) => setInputTask(t),
          openReviewers: (t) => setReviewerTask(t),
          removeEdge,
          openTask: (id) => setDrawerTaskId(id),
        }}
      />
      <ConfigureInputModal
        projectId={projectId}
        task={inputTask}
        tasks={tasks}
        taskCode={taskCode}
        onClose={() => setInputTask(null)}
        onSaved={() => {
          setInputTask(null);
          load();
        }}
      />
      <ReviewersModal
        projectId={projectId}
        task={reviewerTask}
        members={members}
        onClose={() => setReviewerTask(null)}
        onSaved={() => {
          setReviewerTask(null);
          load();
        }}
      />
      <Modal
        title="提交完成申请"
        open={!!completionTask}
        okText="提交完成申请"
        cancelText="取消"
        okButtonProps={{ disabled: !completionNote.trim() }}
        onCancel={() => setCompletionTask(null)}
        onOk={async () => {
          if (completionTask) {
            await submitCompletion(completionTask, completionNote.trim());
          }
          setCompletionTask(null);
        }}
      >
        <p className="muted" style={{ marginTop: 0 }}>
          本次全部候选交付物整体提交；已配置中间审核人时进入多人或签，否则直接进入待 KR 终审。
        </p>
        <Input.TextArea
          rows={3}
          maxLength={500}
          placeholder="提交说明（必填）"
          value={completionNote}
          onChange={(e) => setCompletionNote(e.target.value)}
        />
      </Modal>
      <Modal
        title="退回完成申请"
        open={!!crReject}
        okText="确认退回"
        cancelText="取消"
        okButtonProps={{ danger: true, disabled: !crRejectOpinion.trim() }}
        onCancel={() => setCrReject(null)}
        onOk={async () => {
          if (crReject) {
            await decideCompletion(crReject.task, crReject.reviewId, "rejected", crRejectOpinion.trim());
          }
          setCrReject(null);
        }}
      >
        <p className="muted" style={{ marginTop: 0 }}>
          退回后本次候选文件删除、原当前交付物保持不变，任务回到进行中；退回意见必填。
        </p>
        <Input.TextArea
          rows={3}
          maxLength={500}
          placeholder="退回意见（必填）"
          value={crRejectOpinion}
          onChange={(e) => setCrRejectOpinion(e.target.value)}
        />
      </Modal>
      <FieldChangeModal
        task={editTask}
        members={members}
        onClose={() => setEditTask(null)}
        onSaved={() => {
          setEditTask(null);
          load();
        }}
        projectId={projectId}
      />
      <Modal
        title="退回关键字段修改"
        open={!!fcReject}
        okText="确认退回"
        cancelText="取消"
        okButtonProps={{ danger: true }}
        onCancel={() => setFcReject(null)}
        onOk={async () => {
          if (fcReject) {
            await decideFieldChange(fcReject.task, fcReject.changeId, "rejected", fcRejectOpinion.trim() || undefined);
          }
          setFcReject(null);
        }}
      >
        <p className="muted" style={{ marginTop: 0 }}>
          退回后拟议值作废，旧值保持不变；提交人会看到退回待处理事项。
        </p>
        <Input.TextArea
          rows={3}
          maxLength={500}
          placeholder="审批意见（选填）"
          value={fcRejectOpinion}
          onChange={(e) => setFcRejectOpinion(e.target.value)}
        />
      </Modal>
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
      <Modal
        title="更新进度"
        open={!!progressTask}
        okText="保存"
        cancelText="取消"
        onCancel={() => setProgressTask(null)}
        onOk={async () => {
          if (progressTask) {
            await saveProgress(progressTask, progressValue);
          }
          setProgressTask(null);
        }}
      >
        <p className="muted" style={{ marginTop: 0 }}>
          按真实情况填写百分比；留空表示不填写，页面只展示状态。
        </p>
        <InputNumber
          min={0}
          max={100}
          value={progressValue}
          onChange={(v) => setProgressValue(v ?? null)}
          addonAfter="%"
          placeholder="未填写"
          style={{ width: 160 }}
        />
      </Modal>
      <Modal
        title="取消任务"
        open={!!cancelTask}
        okText="确认取消任务"
        cancelText="返回"
        okButtonProps={{ danger: true, disabled: !cancelReason.trim() }}
        onCancel={() => setCancelTask(null)}
        onOk={async () => {
          if (cancelTask) {
            await doCancelTask(cancelTask, cancelReason.trim());
          }
          setCancelTask(null);
        }}
      >
        <p className="muted" style={{ marginTop: 0 }}>
          任务不再执行并保留原因；已取消任务不计入 KR 进度覆盖度。
        </p>
        <Input.TextArea
          rows={3}
          maxLength={500}
          placeholder="取消原因（必填）"
          value={cancelReason}
          onChange={(e) => setCancelReason(e.target.value)}
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
      destroyOnClose
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
          <span>开始 / 截止</span>
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
  const [selected, setSelected] = useState<string[]>([]);
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
        inviteeIds: selected.map(Number),
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
      width={760}
      confirmLoading={saving}
      onOk={send}
      onCancel={onClose}
      okText="发送邀请"
      cancelText="取消"
      destroyOnClose
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <div style={{ display: "grid", gap: 12 }}>
        <div>
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
          <Transfer
            dataSource={candidates.map((m) => ({
              key: String(m.userId),
              title: m.displayName,
              description: m.username,
            }))}
            targetKeys={selected}
            onChange={(keys) => setSelected(keys.map(String))}
            render={(item) => `${item.title}（${item.description}）`}
            titles={["可选成员", "已选成员"]}
            showSearch
            listStyle={{ width: 320, height: 280 }}
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

const fmtTime = (s?: string) => (s ? s.slice(0, 16).replace("T", " ") : "");

// 任务详情抽屉（AC-31/AC-34）：任务概况、讨论、审核三 Tab；
// PRD §7.5 规定 Tab 顺序为概况、讨论、审核（原型 nav 顺序与 PRD 不一致，以 PRD 为准）。
function TaskDrawer({
  projectId,
  task,
  code,
  members,
  initialTab,
  onClose,
  actions,
}: {
  projectId: number;
  task: Task | null;
  code?: string;
  members: ProjectMember[];
  initialTab?: string;
  onClose: () => void;
  actions: {
    start: (t: Task) => void;
    submitPool: (t: Task) => void;
    approvePool: (t: Task) => void;
    openReject: (t: Task) => void;
    openCancel: (t: Task) => void;
    openProgress: (t: Task) => void;
    openEdit: (t: Task) => void;
    approveFieldChange: (t: Task, changeId: number) => void;
    openFcReject: (t: Task, changeId: number) => void;
    abandonFieldChange: (t: Task, changeId: number) => void;
    openSubmitCompletion: (t: Task) => void;
    approveCompletion: (t: Task, reviewId: number) => void;
    openCrReject: (t: Task, reviewId: number) => void;
    openConfigureInput: (t: Task) => void;
    openReviewers: (t: Task) => void;
    removeEdge: (edgeId: number) => void;
    openTask: (taskId: number) => void;
  };
}) {
  const [detail, setDetail] = useState<TaskDetail | null>(null);
  const [newDeliverableName, setNewDeliverableName] = useState("");
  const [discussionDraft, setDiscussionDraft] = useState("");
  const [postingDiscussion, setPostingDiscussion] = useState(false);

  const postDiscussion = async () => {
    if (!task) return;
    // 被 @ 成员按文本中的 @姓名 匹配项目成员（原型「姓名匹配」交互）。
    const mentionUserIds = members
      .filter((m) => discussionDraft.includes(`@${m.displayName}`))
      .map((m) => m.userId);
    setPostingDiscussion(true);
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/discussions", {
      params: { path: { projectId, taskId: task.id } },
      body: { content: discussionDraft.trim(), mentionUserIds },
    });
    setPostingDiscussion(false);
    if (res.data) {
      message.success("意见已提交");
      setDiscussionDraft("");
      setRefreshTick((n) => n + 1);
    } else {
      message.error(res.error?.message ?? "提交失败");
    }
  };
  const [uploadingId, setUploadingId] = useState<number | null>(null);
  const [refreshTick, setRefreshTick] = useState(0);


  useEffect(() => {
    if (!task) {
      setDetail(null);
      return;
    }
    let alive = true;
    client
      .GET("/projects/{projectId}/tasks/{taskId}", {
        params: { path: { projectId, taskId: task.id } },
      })
      .then((res) => {
        if (alive && res.data) setDetail(res.data);
      });
    return () => {
      alive = false;
    };
  }, [projectId, task, refreshTick]);

  const openFile = async (fileId: number) => {
    const res = await client.GET("/projects/{projectId}/files/{fileId}/download-url", {
      params: { path: { projectId, fileId } },
    });
    if (res.data) {
      window.open(res.data.url, "_blank");
    } else {
      message.error(res.error?.message ?? "获取下载地址失败");
    }
  };

  const addDeliverable = async () => {
    if (!task) return;
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/deliverables", {
      params: { path: { projectId, taskId: task.id } },
      body: { name: newDeliverableName.trim() },
    });
    if (res.data) {
      message.success("交付物项已新增");
      setNewDeliverableName("");
      setRefreshTick((n) => n + 1);
    } else {
      message.error(res.error?.message ?? "新增失败");
    }
  };

  const pickAndUpload = (deliverableId: number) => {
    if (!task) return;
    const input = document.createElement("input");
    input.type = "file";
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) return;
      setUploadingId(deliverableId);
      const res = await client.POST(
        "/projects/{projectId}/tasks/{taskId}/deliverables/{deliverableId}/candidate",
        {
          params: { path: { projectId, taskId: task.id, deliverableId } },
          body: {
            fileName: file.name,
            fileType: file.name.split(".").pop() ?? "",
            fileSize: file.size,
          },
        },
      );
      if (!res.data) {
        setUploadingId(null);
        message.error(res.error?.message ?? "登记候选内容失败");
        return;
      }
      try {
        const put = await fetch(res.data.uploadUrl, { method: "PUT", body: file });
        if (!put.ok) throw new Error(`HTTP ${put.status}`);
        message.success("候选内容已上传；随完成申请提交后进入审核");
      } catch {
        message.error("文件上传失败，请确认文件服务可用后重试");
      }
      setUploadingId(null);
      setRefreshTick((n) => n + 1);
    };
    input.click();
  };

  if (!task) return null;
  const reviewers = detail?.reviewers ?? [];
  const inputs = detail?.inputs ?? [];
  const outputs = detail?.outputs ?? [];
  const requiredInputs = inputs.filter((e) => e.necessity === "required");
  const referenceInputs = inputs.filter((e) => e.necessity === "reference");
  const currentFiles = (detail?.deliverables ?? [])
    .filter((d) => d.current)
    .map((d) => ({ d, f: d.current! }));
  const candidateCount = (detail?.deliverables ?? []).filter((d) => d.candidate).length;
  const pendingReviews =
    (detail?.poolReviews ?? []).filter((r) => r.status === "pending").length +
    (detail?.fieldChanges ?? []).filter((fc) => fc.state === "pending").length +
    (detail?.completionReviews ?? []).filter(
      (cr) => cr.state === "pending_final" || cr.state === "intermediate_review",
    ).length;

  const overview = (
    <>
      <section className="drawer-section" style={{ marginTop: 4 }}>
        <h3>基础信息</h3>
        <div className="task-info-list">
          <div className="task-info-row">
            <span>状态</span>
            <div>
              <span className={`status-pill ${STATUS_CLASS[task.status]}`}>
                {STATUS_LABEL[task.status]}
              </span>
              {task.status === "cancelled" && task.cancelReason && (
                <span className="muted" style={{ marginLeft: 8, fontSize: 12 }}>
                  原因：{task.cancelReason}
                </span>
              )}
            </div>
          </div>
          <div className="task-info-row">
            <span>负责人</span>
            <strong>{task.ownerName}</strong>
          </div>
          <div className="task-info-row">
            <span>参与人</span>
            <strong className="muted">未设置</strong>
          </div>
          <div className="task-info-row">
            <span>开始／截止</span>
            <strong>
              {task.startDate} — {task.endDate}
            </strong>
          </div>
          <div className="task-info-row">
            <span>进度</span>
            <strong>{task.progress != null ? `${task.progress}%` : "未填写"}</strong>
          </div>
          <div className="task-info-row">
            <span>当前环节／待行动人</span>
            <strong>
              {task.currentStage}
              {task.pendingActorName ? ` · ${task.pendingActorName}` : ""}
            </strong>
          </div>
          <div className="task-info-row">
            <span>中间审核（或签）</span>
            <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <strong className={reviewers.length ? "" : "muted"}>
                {reviewers.length ? reviewers.map((r) => r.displayName).join("、") : "未配置"}
              </strong>
              {task.canManageReviewers && (
                <Button size="small" onClick={() => actions.openReviewers(task)}>
                  调整
                </Button>
              )}
            </div>
          </div>
          <div className="task-info-row">
            <span>任务说明</span>
            <strong className={task.description ? "" : "muted"}>
              {task.description || "未填写"}
            </strong>
          </div>
          <div className="task-info-row">
            <span>完成标准</span>
            <strong className={task.completionCriteria ? "" : "muted"}>
              {task.completionCriteria || "未填写"}
            </strong>
          </div>
        </div>
      </section>
      {requiredInputs.length > 0 && (
        <section className="drawer-section">
          <h3>
            必要输入{" "}
            <span className="muted" style={{ fontWeight: 400, fontSize: 12 }}>
              {requiredInputs.length} 项
            </span>
          </h3>
          {requiredInputs.map((e) => (
            <div key={e.id} className="input-fact">
              <div>
                <b>{e.name}</b>
                <small>
                  已有任务 · {e.sourceTaskName ?? "—"}
                  {e.deliverableName ? ` · ${e.deliverableName}` : ""}
                  {e.sourceOwnerName ? ` · 提供人 ${e.sourceOwnerName}` : ""}
                </small>
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                <span className={`status-pill ${e.ready ? "completed" : "warning"}`}>
                  {e.ready ? "已就绪" : "未就绪"}
                </span>
                {e.canRemove && (
                  <Button size="small" type="text" onClick={() => actions.removeEdge(e.id)}>
                    解除
                  </Button>
                )}
              </div>
            </div>
          ))}
        </section>
      )}
      {referenceInputs.length > 0 && (
        <section className="drawer-section">
          <h3>
            参考输入{" "}
            <span className="muted" style={{ fontWeight: 400, fontSize: 12 }}>
              仅提示，不产生“等待他人”事项
            </span>
          </h3>
          {referenceInputs.map((e) => (
            <div key={e.id} className="input-fact">
              <div>
                <b>{e.name}</b>
                <small>
                  {e.sourceTaskName ?? "—"} · {EDGE_TYPE_LABEL[e.edgeType]}
                </small>
              </div>
              {e.canRemove && (
                <Button size="small" type="text" onClick={() => actions.removeEdge(e.id)}>
                  解除
                </Button>
              )}
            </div>
          ))}
        </section>
      )}
      <section className="drawer-section">
        <h3>
          当前交付物{" "}
          <span className="muted" style={{ fontWeight: 400, fontSize: 12 }}>
            {currentFiles.length} 项
          </span>
        </h3>
        {candidateCount > 0 && (
          <div className="notice warning" style={{ marginBottom: 10 }}>
            有 {candidateCount} 项更新审核中，候选内容请在“审核”Tab 查看；当前内容继续有效。
          </div>
        )}
        {currentFiles.map(({ d, f }) => (
          <article key={f.id} className="deliverable-card">
            <div>
              <b>{d.name}</b>
              <span className="file-link" onClick={() => openFile(f.id)}>
                {f.fileName}
              </span>
              <div className="muted" style={{ fontSize: 12 }}>
                {f.fileType || "文件"} · {f.fileSize ? `${Math.ceil(f.fileSize / 1024)} KB` : "—"}
                {f.effectiveAt ? ` · 生效于 ${fmtTime(f.effectiveAt)}` : ""}
              </div>
            </div>
            <Button size="small" onClick={() => openFile(f.id)}>
              下载
            </Button>
          </article>
        ))}
        {currentFiles.length === 0 && (
          <div className="empty compact-empty">尚无已生效的当前交付物</div>
        )}
      </section>
      <section className="drawer-section">
        <h3>
          交付物项{" "}
          <span className="muted" style={{ fontWeight: 400, fontSize: 12 }}>
            {(detail?.deliverables ?? []).length} 项
          </span>
        </h3>
        {(detail?.deliverables ?? []).map((d) => (
          <article key={d.id} className="deliverable-card">
            <div>
              <b>{d.name}</b>
              <div className="muted" style={{ fontSize: 12 }}>
                {d.current ? "已有当前内容" : "尚无当前内容"}
                {d.candidate ? ` · 候选「${d.candidate.fileName}」审核准备中` : ""}
              </div>
            </div>
            {task.canUploadCandidate && (
              <Button size="small" loading={uploadingId === d.id} onClick={() => pickAndUpload(d.id)}>
                {d.candidate ? "重传候选内容" : "上传候选内容"}
              </Button>
            )}
          </article>
        ))}
        {task.canManageDeliverables && (
          <div style={{ display: "flex", gap: 8, marginTop: 8 }}>
            <Input
              size="small"
              maxLength={100}
              placeholder="新增交付物项名称"
              value={newDeliverableName}
              onChange={(e) => setNewDeliverableName(e.target.value)}
              style={{ width: 240 }}
            />
            <Button size="small" onClick={addDeliverable} disabled={!newDeliverableName.trim()}>
              ＋ 新增交付物项
            </Button>
          </div>
        )}
      </section>
      {(inputs.length > 0 || outputs.length > 0) && (
        <section className="drawer-section">
          <h3>协作关系</h3>
          {inputs.length > 0 && (
            <div style={{ marginBottom: 8 }}>
              <b style={{ fontSize: 12, color: "var(--muted)" }}>直接上游 · {inputs.length}</b>
              {inputs.map((e) =>
                e.sourceTaskId ? (
                  <button
                    key={e.id}
                    type="button"
                    className="relation-card"
                    onClick={() => actions.openTask(e.sourceTaskId!)}
                  >
                    <span>
                      <b>{e.sourceTaskName}</b>
                      <small>
                        {EDGE_TYPE_LABEL[e.edgeType]} · {e.name}
                        {e.currentFileName ? ` · ${e.currentFileName}` : ""}
                      </small>
                    </span>
                    <span className={`status-pill ${e.ready ? "completed" : "warning"}`}>
                      {e.ready ? "已就绪" : "未就绪"}
                    </span>
                  </button>
                ) : null,
              )}
            </div>
          )}
          {outputs.length > 0 && (
            <div>
              <b style={{ fontSize: 12, color: "var(--muted)" }}>直接下游 · {outputs.length}</b>
              {outputs.map((e) => (
                <button
                  key={e.id}
                  type="button"
                  className="relation-card"
                  onClick={() => actions.openTask(e.targetTaskId)}
                >
                  <span>
                    <b>{e.targetTaskName}</b>
                    <small>
                      {EDGE_TYPE_LABEL[e.edgeType]} · {e.name}
                    </small>
                  </span>
                  <span className={`status-pill ${e.ready ? "completed" : "warning"}`}>
                    {e.ready ? "已就绪" : "未就绪"}
                  </span>
                </button>
              ))}
            </div>
          )}
        </section>
      )}
    </>
  );

  const discussions = detail?.discussions ?? [];
  const discussion = (
    <div>
      {discussions.length === 0 && <div className="empty compact-empty">尚无讨论意见</div>}
      {discussions.map((d) => (
        <article key={d.id} className="deliverable-card" style={{ alignItems: "flex-start" }}>
          <div style={{ minWidth: 0 }}>
            <b>
              {d.authorName}
              <span className="muted" style={{ fontWeight: 400, fontSize: 12, marginLeft: 8 }}>
                {fmtTime(d.createdAt)}
                {d.mentionNames && d.mentionNames.length > 0 && ` · @ ${d.mentionNames.join("、")}`}
              </span>
            </b>
            <div style={{ whiteSpace: "pre-wrap" }}>{d.content}</div>
          </div>
        </article>
      ))}
      <div style={{ marginTop: 12 }}>
        <Mentions
          rows={3}
          placeholder="输入文字意见，可使用 @姓名 提醒项目成员"
          value={discussionDraft}
          onChange={setDiscussionDraft}
          options={members.map((m) => ({ value: m.displayName, label: m.displayName }))}
        />
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            marginTop: 8,
            gap: 8,
          }}
        >
          <span className="muted" style={{ fontSize: 12 }}>
            提交后不可编辑、不可删除；任务负责人和被 @ 成员会收到通知。
          </span>
          <Button
            type="primary"
            size="small"
            loading={postingDiscussion}
            disabled={!discussionDraft.trim()}
            onClick={postDiscussion}
          >
            提交讨论
          </Button>
        </div>
      </div>
    </div>
  );

  const audit = (
    <div style={{ paddingTop: 4 }}>
      {(detail?.poolReviews ?? []).length === 0 &&
        (detail?.fieldChanges ?? []).length === 0 &&
        (detail?.completionReviews ?? []).length === 0 && (
          <div className="empty compact-empty">暂无审核记录</div>
        )}
      {(detail?.completionReviews ?? []).map((cr) => (
        <article key={"cr-" + cr.id} className={"audit-card " + (cr.state === "pending_final" ? "pending" : "")}>
          <div className="audit-card-head">
            <div>
              <b>完成申请</b>{" "}
              <span
                className={
                  "status-pill " +
                  (cr.state === "pending_final"
                    ? "review"
                    : cr.state === "approved"
                      ? "completed"
                      : cr.state === "rejected"
                        ? "danger"
                        : "warning")
                }
              >
                {cr.state === "pending_final"
                  ? "待 KR 终审"
                  : cr.state === "approved"
                    ? "已通过"
                    : cr.state === "rejected"
                      ? "已退回"
                      : "中间审核中"}
              </span>
            </div>
            <span className="meta muted" style={{ fontSize: 12 }}>
              申请人 {cr.submittedByName} · {fmtTime(cr.submittedAt)}
            </span>
          </div>
          <div style={{ marginTop: 8, fontSize: 13 }}>
            <div className="muted" style={{ fontSize: 12 }}>
              提交说明:{cr.note}；本次 {cr.items.length} 项候选整体通过或退回。
              {cr.reviewers && cr.reviewers.length > 0 &&
                `或签组：${cr.reviewers.map((r) => r.displayName).join("、")}（任一人通过即进入待 KR 终审）。`}
            </div>
            {cr.intermediateByName && (
              <div className="handled-fact" style={{ marginTop: 6 }}>
                <b>
                  中间或签通过 · {cr.intermediateByName}
                  {cr.intermediateAt ? ` · ${fmtTime(cr.intermediateAt)}` : ""}
                </b>
                <div>{cr.intermediateOpinion || "未填写意见"}</div>
              </div>
            )}
            {cr.items.map((it) => (
              <div key={cr.id + "-" + it.deliverableId} style={{ marginTop: 4 }}>
                <b>{it.deliverableName}</b>：
                {it.fileId ? (
                  <span className="file-link" onClick={() => openFile(it.fileId!)}>
                    {it.fileName}
                  </span>
                ) : (
                  <span className="muted">{it.fileName}（文件已按覆盖／退回规则删除）</span>
                )}
              </div>
            ))}
          </div>
          {(cr.decidedByName || cr.opinion) && (
            <div className="handled-fact">
              {cr.decidedByName && (
                <b>
                  {cr.decidedByName}
                  {cr.decidedAt ? " · " + fmtTime(cr.decidedAt) : ""}
                </b>
              )}
              <div>{cr.opinion || "未填写意见"}</div>
            </div>
          )}
          {cr.state === "pending_final" && cr.canDecide && (
            <div className="audit-actions">
              <Button size="small" danger onClick={() => actions.openCrReject(task, cr.id)}>
                退回
              </Button>
              <Button size="small" type="primary" onClick={() => actions.approveCompletion(task, cr.id)}>
                通过 / 闭环
              </Button>
            </div>
          )}
        </article>
      ))}
      {(detail?.fieldChanges ?? []).map((fc) => (
        <article key={`fc-${fc.id}`} className={`audit-card ${fc.state === "pending" ? "pending" : ""}`}>
          <div className="audit-card-head">
            <div>
              <b>关键字段修改</b>{" "}
              <span
                className={`status-pill ${
                  fc.state === "pending" ? "warning" : fc.state === "approved" ? "completed" : "danger"
                }`}
              >
                {fc.exempt ? "免审生效" : fc.state === "pending" ? "待审批" : fc.state === "approved" ? "已通过" : "已退回"}
              </span>
            </div>
            <span className="meta muted" style={{ fontSize: 12 }}>
              申请人 {fc.submittedByName} · {fmtTime(fc.submittedAt)}
            </span>
          </div>
          <div style={{ marginTop: 8, fontSize: 13 }}>
            {fc.changes.map((c) => (
              <div key={c.field}>
                <span className="muted">{c.label}：</span>
                {c.oldValue || "—"} → <b>{c.newValue}</b>
              </div>
            ))}
            {fc.reason && (
              <div className="muted" style={{ fontSize: 12, marginTop: 4 }}>
                修改原因:{fc.reason}；审批完成前旧值继续生效。
              </div>
            )}
          </div>
          {(fc.decidedByName || fc.opinion) && (
            <div className="handled-fact">
              {fc.decidedByName && (
                <b>
                  {fc.decidedByName}
                  {fc.decidedAt ? ` · ${fmtTime(fc.decidedAt)}` : ""}
                </b>
              )}
              <div>{fc.opinion || "未填写意见"}</div>
            </div>
          )}
          {fc.state === "pending" && fc.canDecide && (
            <div className="audit-actions">
              <Button size="small" danger onClick={() => actions.openFcReject(task, fc.id)}>
                退回
              </Button>
              <Button size="small" type="primary" onClick={() => actions.approveFieldChange(task, fc.id)}>
                通过
              </Button>
            </div>
          )}
          {fc.state === "rejected" && !fc.resolved && fc.canAbandon && (
            <div className="audit-actions">
              <Button size="small" onClick={() => actions.abandonFieldChange(task, fc.id)}>
                放弃本次变更
              </Button>
              <Button size="small" type="primary" onClick={() => actions.openEdit(task)}>
                修改并重提
              </Button>
            </div>
          )}
        </article>
      ))}
      {(detail?.poolReviews ?? []).map((r, i) => (
        <article key={i} className={`audit-card ${r.status === "pending" ? "pending" : ""}`}>
          <div className="audit-card-head">
            <div>
              <b>创建入池审批</b>{" "}
              <span
                className={`status-pill ${
                  r.status === "pending" ? "warning" : r.status === "approved" ? "completed" : "danger"
                }`}
              >
                {r.exempt ? "免审通过" : r.status === "pending" ? "待审批" : r.status === "approved" ? "已通过" : "已退回"}
              </span>
            </div>
            <span className="meta muted" style={{ fontSize: 12 }}>
              申请人 {r.submittedByName} · {fmtTime(r.submittedAt)}
            </span>
          </div>
          {(r.decidedByName || r.opinion) && (
            <div className="handled-fact">
              {r.decidedByName && (
                <b>
                  {r.decidedByName}
                  {r.decidedAt ? ` · ${fmtTime(r.decidedAt)}` : ""}
                </b>
              )}
              <div>{r.opinion || "未填写意见"}</div>
            </div>
          )}
          {r.status === "pending" && task.canDecidePoolReview && (
            <div className="audit-actions">
              <Button size="small" danger onClick={() => actions.openReject(task)}>
                退回
              </Button>
              <Button size="small" type="primary" onClick={() => actions.approvePool(task)}>
                通过
              </Button>
            </div>
          )}
        </article>
      ))}
    </div>
  );

  return (
    <Drawer
      open={!!task}
      onClose={onClose}
      width={720}
      title={
        <div>
          {code} · {task.name}
          <div className="drawer-sub">
            所属 O / KR：{detail ? `${detail.objectiveTitle} / ${detail.krDescription}` : "…"}
          </div>
        </div>
      }
      footer={
        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          {task.canProposeFieldChange && (
            <Button onClick={() => actions.openEdit(task)}>编辑任务</Button>
          )}
          {task.canManageDeliverables && (
            <Button onClick={() => actions.openConfigureInput(task)}>配置输入</Button>
          )}
          {task.canStart && <Button onClick={() => actions.start(task)}>开始执行</Button>}
          {task.canUpdateProgress && (
            <Button onClick={() => actions.openProgress(task)}>更新进度</Button>
          )}
          {task.canCancel && <Button onClick={() => actions.openCancel(task)}>取消任务</Button>}
          {task.canSubmitPoolReview && (
            <Button type="primary" onClick={() => actions.submitPool(task)}>
              提交入池审批
            </Button>
          )}
          {task.canSubmitCompletion && (
            <Button type="primary" onClick={() => actions.openSubmitCompletion(task)}>
              提交完成申请
            </Button>
          )}
        </div>
      }
    >
      <Tabs
        key={task.id}
        defaultActiveKey={initialTab ?? "overview"}
        items={[
          { key: "overview", label: "任务概况", children: overview },
          { key: "discussion", label: `讨论 ${discussions.length}`, children: discussion },
          { key: "audit", label: `审核 ${pendingReviews}`, children: audit },
        ]}
      />
    </Drawer>
  );
}

// 编辑任务／提交关键字段修改（AC-23）：草稿直接生效；已入池任务进入审批（KR 负责人本人免审）。
function FieldChangeModal({
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
      destroyOnClose
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

// 配置输入（AC-28）：默认搜索系统内已有任务，选择来源任务及其交付物建立交付物边。
function ConfigureInputModal({
  projectId,
  task,
  tasks,
  taskCode,
  onClose,
  onSaved,
}: {
  projectId: number;
  task: Task | null;
  tasks: Task[];
  taskCode: Map<number, string>;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [sourceTaskId, setSourceTaskId] = useState<number | undefined>(undefined);
  const [sourceDeliverables, setSourceDeliverables] = useState<{ id: number; name: string }[]>([]);
  const [deliverableId, setDeliverableId] = useState<number | undefined>(undefined);
  const [name, setName] = useState("");
  const [edgeType, setEdgeType] = useState<EdgeType>("hard_prerequisite");
  const [necessity, setNecessity] = useState<"required" | "reference">("required");
  const [expectedDate, setExpectedDate] = useState<Dayjs | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (task) {
      setSourceTaskId(undefined);
      setSourceDeliverables([]);
      setDeliverableId(undefined);
      setName("");
      setEdgeType("hard_prerequisite");
      setNecessity("required");
      setExpectedDate(null);
      setError(null);
    }
  }, [task]);

  useEffect(() => {
    if (!sourceTaskId) {
      setSourceDeliverables([]);
      setDeliverableId(undefined);
      return;
    }
    client
      .GET("/projects/{projectId}/tasks/{taskId}", {
        params: { path: { projectId, taskId: sourceTaskId } },
      })
      .then((res) => {
        if (res.data) {
          setSourceDeliverables(res.data.deliverables.map((d) => ({ id: d.id, name: d.name })));
        }
      });
  }, [projectId, sourceTaskId]);

  if (!task) return null;
  const candidates = tasks.filter((t) => t.id !== task.id && t.status !== "cancelled");

  const save = async () => {
    if (!sourceTaskId) {
      setError("请选择来源任务");
      return;
    }
    if (!name.trim()) {
      setError("请填写输入名称");
      return;
    }
    setSaving(true);
    setError(null);
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/inputs", {
      params: { path: { projectId, taskId: task.id } },
      body: {
        name: name.trim(),
        necessity,
        edgeType,
        sourceTaskId,
        deliverableId,
        expectedDate: expectedDate ? expectedDate.format("YYYY-MM-DD") : undefined,
      },
    });
    setSaving(false);
    if (res.data) {
      message.success("已建立「来源任务 → 本任务」的交付物边");
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
          <span className="modal-sub">来源为系统内已有任务；当前交付物生效后输入自动就绪</span>
        </div>
      }
      open={!!task}
      width={640}
      confirmLoading={saving}
      onOk={save}
      onCancel={onClose}
      okText="建立输入关系"
      cancelText="取消"
      destroyOnClose
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <div style={{ display: "grid", gap: 12 }}>
        <div>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>来源任务</div>
          <Select
            style={{ width: "100%" }}
            showSearch
            optionFilterProp="label"
            placeholder="按任务名称、编号或负责人搜索"
            value={sourceTaskId}
            onChange={setSourceTaskId}
            options={candidates.map((t) => ({
              value: t.id,
              label: `${taskCode.get(t.id) ?? ""} ${t.name}（${t.ownerName} · ${STATUS_LABEL[t.status]}）`,
            }))}
          />
        </div>
        <div>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
            对应交付物（选填；无文件的条件可不选）
          </div>
          <Select
            style={{ width: "100%" }}
            allowClear
            placeholder="选择来源任务的交付物项"
            value={deliverableId}
            onChange={(v) => {
              setDeliverableId(v);
              const d = sourceDeliverables.find((x) => x.id === v);
              if (d && !name.trim()) setName(d.name);
            }}
            options={sourceDeliverables.map((d) => ({ value: d.id, label: d.name }))}
          />
        </div>
        <div>
          <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>输入名称</div>
          <Input maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: 12 }}>
          <div>
            <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>关系类型</div>
            <Select
              style={{ width: "100%" }}
              value={edgeType}
              onChange={setEdgeType}
              options={(Object.keys(EDGE_TYPE_LABEL) as EdgeType[]).map((k) => ({
                value: k,
                label: EDGE_TYPE_LABEL[k],
              }))}
            />
          </div>
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
          <div>
            <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>期望时间（选填）</div>
            <DatePicker style={{ width: "100%" }} value={expectedDate} onChange={setExpectedDate} />
          </div>
        </div>
        <div className="notice">
          某项信息不到位会阻止下游开始或完成时，请选择「硬前置交付」；必要输入未就绪的任务显示“等待输入”。
        </div>
      </div>
    </Modal>
  );
}

// 中间审核人配置（§5.4）：非关键字段，可直接调整；0～多名，或签。
function ReviewersModal({
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
      destroyOnClose
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

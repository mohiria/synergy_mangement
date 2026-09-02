import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Spin, Tabs, message } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";
import TaskDrawerHost from "./task-drawer";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type TaskStatus = components["schemas"]["TaskStatus"];
type MyWork = components["schemas"]["MyWork"];
type WorkItem = components["schemas"]["WorkItem"];

// 状态色（与全部任务、项目总览各页一致）；状态文案一律消费 API 的 statusLabel（AC-04）。
const STATUS_CLASS: Record<TaskStatus, string> = {
  not_started: "",
  waiting_input: "warning",
  in_progress: "in_progress",
  pending_intermediate_review: "review",
  pending_final_review: "review",
  completed: "completed",
  cancelled: "",
};

// 事项类型的单字标记（原型 work-kind 徽标）；分组语义由 Tab 表达，卡片正文不再展开原因。
// 裁决 10（#180）：关闭申请退场，无 cancel_request／waiting_cancel_request 条目。
const KIND_BADGE: Record<string, string> = {
  task: "办",
  invite: "邀",
  intermediate_review: "审",
  final_review: "审",
  receipt: "收",
  upstream: "等",
  waiting_completion: "等",
  blocker: "卡",
};

// 五分组的固定顺序与空态文案（AC-16；终审归入待我审批）。
const GROUPS = [
  { key: "pending", label: "待我处理", empty: "暂无需要你处理的事项" },
  { key: "approvals", label: "待我审批", empty: "暂无等待你审批的事项" },
  { key: "receipts", label: "待我接收", empty: "暂无待你确认接收的交付物" },
  { key: "waiting", label: "等待他人", empty: "没有停在他人手里的事项" },
  { key: "blockers", label: "与我相关的卡点", empty: "没有与你相关的卡点" },
] as const satisfies readonly { key: keyof Omit<MyWork, "identity">; label: string; empty: string }[];

// 我的工作（AC-16、AC-55、MW-21）：五分组个人行动与等待事实。
// 卡片正文只显示任务编号与名称、所属 KR、日期和左对齐的任务状态；
// 原因、输入、影响、已等待天数和退回理由全部进任务详情（模块 PRD §5.1、§5.2）。
export default function MyWorkPage({
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
  const [work, setWork] = useState<MyWork | null>(null);
  const [objectives, setObjectives] = useState<Objective[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, workRes, objectivesRes, tasksRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/my-work", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/objectives", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/tasks", { params: { path: { projectId } } }),
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
    setWork(workRes.data ?? null);
    setObjectives(objectivesRes.data ?? []);
    setTasks(tasksRes.data ?? []);
    setLoading(false);
  }, [projectId, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  // 编号是持久字段（AC-64）：跨页一致、增删任务后不位移，前端只取不算。
  const krCode = useMemo(() => {
    const m = new Map<number, string>();
    objectives.forEach((o) => o.keyResults.forEach((k) => m.set(k.id, k.code)));
    return m;
  }, [objectives]);
  const taskCode = useMemo(() => new Map(tasks.map((t) => [t.id, t.code])), [tasks]);
  const taskById = useMemo(() => new Map(tasks.map((t) => [t.id, t])), [tasks]);

  // 卡片只负责定位：在本页原地打开任务详情抽屉，并带上来源分组供抽屉落位（模块 PRD §6.2、#110）。
  // 抽屉是与全部任务页同一个组件（#109），动作集合完全一致，不另做只读视图。
  const [drawer, setDrawer] = useState<{ taskId: number; tab: string; source: string } | null>(null);
  const openItem = (item: WorkItem, source: string) => {
    if (!item.taskId) {
      // 没有关联任务的事项（任务创建邀请）仍去全部任务页处理。
      navigate(`/projects/${projectId}/tasks`);
      return;
    }
    setDrawer({ taskId: item.taskId, tab: item.drawerTab ?? "overview", source });
  };

  const remind = async (item: WorkItem) => {
    if (!item.refKey) return;
    const res = await client.POST("/projects/{projectId}/reminders", {
      params: { path: { projectId } },
      body: { targetKey: item.refKey },
    });
    if (res.response.status === 204) {
      message.success("已提醒当前待行动人");
      load();
    } else {
      message.error(res.error?.message ?? "提醒失败");
    }
  };

  const renderGroup = (items: WorkItem[], source: string, emptyText: string) => (
    <div className="work-list">
      {items.length === 0 && <div className="work-empty">{emptyText}</div>}
      {items.map((it, i) => {
        const task = it.taskId ? taskById.get(it.taskId) : undefined;
        // 卡片主体一律是所属任务；无关联任务的事项（任务创建邀请）退回事项标题。
        const title = task ? `${taskCode.get(task.id) ?? ""} ${task.name}` : it.title;
        const kr = task ? (krCode.get(task.keyResultId) ?? "—") : "—";
        const blocked = !!it.unreadyNote;
        return (
          <article
            key={`${it.kind}-${it.refId ?? it.refKey ?? it.taskId ?? i}`}
            className={`work-item${it.overdue ? " overdue" : blocked ? " blocked" : ""}`}
            onClick={() => openItem(it, source)}
          >
            <div className="work-kind" aria-hidden>
              {KIND_BADGE[it.kind] ?? "事"}
            </div>
            <div className="work-main">
              <h3 title={title}>{title}</h3>
              {/* #130：五组副行统一「KR 编号 · 截止 日期」。 */}
              <div className="work-meta">
                <span title={kr}>{kr}</span>
                <span>· 截止 {it.dueDate ?? "—"}</span>
              </div>
            </div>
            <div className="work-trailing">
              <div className="work-state">
                <span className={`status-pill ${task ? STATUS_CLASS[task.status] : ""}`}>
                  {task ? task.statusLabel : "待处理"}
                </span>
              </div>
              <div className="work-actions">
                {it.canRemind && (
                  <button
                    type="button"
                    className="work-text-action quiet"
                    onClick={(e) => {
                      e.stopPropagation();
                      remind(it);
                    }}
                  >
                    提醒
                  </button>
                )}
                {/* #168：只读组（等待他人、卡点）actionLabel 为空、不渲染文字按钮，点条目行开抽屉。 */}
                {it.actionLabel && (
                  <button type="button" className="work-text-action">
                    {it.actionLabel} ›
                  </button>
                )}
              </div>
            </div>
          </article>
        );
      })}
    </div>
  );

  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="我的工作"
      pageWidth="narrow"
      onLogout={onLogout}
    >
      {notFound ? (
        <Alert type="error" message="项目不存在" description={<Link to="/">返回项目列表</Link>} />
      ) : loading || !work ? (
        <Spin />
      ) : (
        <>
          <div className="page-head">
            <div>
              <h1>我的工作</h1>
              <p>按当前职责派生的个人行动与等待事实；处理动作在任务详情抽屉中完成。</p>
            </div>
          </div>
          {/* 身份卡（模块 PRD §7.2）：姓名、系统权限与当前职责，说明五分组的派生依据。
              职责文案是 API 派生字段，前端不按事实重算。 */}
          <section className="work-identity">
            <div>
              <span className="avatar" aria-hidden>
                {work.identity.displayName.slice(0, 1)}
              </span>
              <div>
                <b>{work.identity.displayName}</b>
                <span>
                  {work.identity.roleLabel} · @{work.identity.username}
                </span>
              </div>
            </div>
            <p>
              <span>当前职责</span>
              {work.identity.responsibilitiesLabel}
            </p>
          </section>
          {/* 处理完关闭即回到我的工作；抽屉内动作落库后回调刷新，五组归类与计数随之更新。 */}
          <TaskDrawerHost
            projectId={projectId}
            taskId={drawer?.taskId ?? null}
            initialTab={drawer?.tab}
            source={drawer?.source}
            onClose={() => setDrawer(null)}
            onChanged={load}
          />
          <div className="work-board">
            <Tabs
              className="work-tabs"
              items={GROUPS.map(({ key, label, empty }) => ({
                key,
                label: (
                  <>
                    {label}
                    <span className="pill">{work[key].length}</span>
                  </>
                ),
                children: renderGroup(work[key], key, empty),
              }))}
            />
          </div>
        </>
      )}
    </ProjectShell>
  );
}

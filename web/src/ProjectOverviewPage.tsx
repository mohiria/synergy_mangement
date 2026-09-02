import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Spin } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";
import ProjectShell from "./ProjectShell";
import TaskDrawerHost from "./task-drawer";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type TaskStatus = components["schemas"]["TaskStatus"];

const STATUS_CLASS: Record<TaskStatus, string> = {
  not_started: "",
  waiting_input: "warning",
  in_progress: "in_progress",
  pending_intermediate_review: "review",
  pending_final_review: "review",
  completed: "completed",
  cancelled: "",
};

const fmtDate = (d?: string | null) => (d ? d.slice(5).replace("-", ".") : "…");

// 项目总览（AC-05/06）：首层只显示 O、KR、风险颜色与一行原因；KR 展开后显示任务事实。
export default function ProjectOverviewPage({
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
  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  // #156（PRD §7.5、AC-31）：点击任务留在总览，本页右侧打开任务详情抽屉。
  const [drawerTask, setDrawerTask] = useState<number | null>(null);

  // #160 裁决：KR 展开层只剩任务列表，交付物边与卡点数据不再在总览消费。
  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, objectivesRes, tasksRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
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
    setObjectives(objectivesRes.data ?? []);
    setTasks(tasksRes.data ?? []);
    setLoading(false);
  }, [projectId, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  // 编号是持久字段（AC-64），直接取 task.code，不再按数组下标现算。
  const taskCode = useMemo(() => new Map(tasks.map((t) => [t.id, t.code])), [tasks]);

  const toggle = (krId: number) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(krId)) next.delete(krId);
      else next.add(krId);
      return next;
    });

  // 头卡事实：只汇总 O／KR 层级数量（AC-05 不在概览首层出现任务与进度数字）。
  const krTotal = objectives.reduce((n, o) => n + o.keyResults.length, 0);
  const attentionKrs = objectives.reduce(
    (n, o) => n + o.keyResults.filter((k) => k.riskLevel !== "normal").length,
    0,
  );
  const highRiskKrs = objectives.reduce(
    (n, o) => n + o.keyResults.filter((k) => k.riskLevel === "high_risk").length,
    0,
  );

  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="项目总览"
      onLogout={onLogout}
    >
      {notFound ? (
        <Alert type="error" message="项目不存在" description={<Link to="/">返回项目列表</Link>} />
      ) : loading || !project ? (
        <Spin />
      ) : (
        <>
          {/* 标题区不重复「阶段 · 项目态势」眉题（风格基线 §8）：阶段落到下方头卡里。 */}
          <div className="page-head">
            <div>
              <h1>项目总览</h1>
              <p>
                {project.plannedStartDate || project.plannedEndDate
                  ? `项目周期 ${fmtDate(project.plannedStartDate)}—${fmtDate(project.plannedEndDate)} · `
                  : ""}
                主负责人 {project.ownerName}
              </p>
            </div>
            {/* §7.1：页头不提供「创建任务」，关系入口统一命名为「查看协作全景」。 */}
            <div style={{ display: "flex", gap: 8 }}>
              {/* #125：OKR 管理并入总览——有编辑权限者从这里进入全页管理模式。 */}
              {project.canEdit && (
                <Button onClick={() => navigate(`/projects/${projectId}/okr`)}>管理 O/KR</Button>
              )}
              <Button onClick={() => navigate(`/projects/${projectId}/graph`)}>查看协作全景</Button>
            </div>
          </div>
          {/* overview-brief 头卡：只汇总 O／KR 层级事实与需关注数量。
              原型这里还有一列「活跃任务」，但 AC-05 要求概览首层不出现任务与进度数字，
              因此本实现不列任务数——这是相对原型的有意偏差。 */}
          <section className="overview-brief" aria-label="项目态势摘要">
            <div className="overview-brief-copy">
              <h2>{project.stage || project.name}</h2>
              <p>
                {attentionKrs === 0
                  ? "当前没有预警或高风险 KR；展开 KR 查看任务与进度事实。"
                  : `${attentionKrs} 个 KR 需要关注，其中 ${highRiskKrs} 个为高风险；建议优先处理硬前置输入与待审批事项。`}
              </p>
            </div>
            <dl className="overview-brief-facts">
              <div>
                <dt>目标</dt>
                <dd>{objectives.length}</dd>
              </div>
              <div>
                <dt>关键结果</dt>
                <dd>{krTotal}</dd>
              </div>
              <div className="attention">
                <dt>需关注 KR</dt>
                <dd>{attentionKrs}</dd>
              </div>
            </dl>
          </section>
          {objectives.length === 0 && <div className="empty">暂无 O／KR，请先在 OKR 管理中录入</div>}
          {objectives.map((o) => (
            <section key={o.id} className="objective">
              <div className="objective-head">
                <span className="objective-code">{o.code}</span>
                <div>
                  <h2>{o.title}</h2>
                  <p>{o.description ?? ""}</p>
                </div>
                {/* AC-59：O 的风险等级取下级 KR 的最大值，由后端派生，前端只渲染。 */}
                <span className="objective-meta">
                  <span className={`status-pill risk-${o.riskLevel}`}>{o.riskLevelLabel}</span>
                  <span className="objective-count">{o.keyResults.length} 个 KR</span>
                </span>
              </div>
              {o.keyResults.map((k) => {
                const code = k.code;
                const isOpen = expanded.has(k.id);
                // #170（#8 反馈）：总览 KR 展开的任务列表默认不显示已关闭任务；
                // 全部任务等其他页面口径不受影响。
                const krTasks = tasks.filter(
                  (t) => t.keyResultId === k.id && t.status !== "cancelled",
                );
                return (
                  <div key={k.id} className="kr-row">
                    <button type="button" className="kr-main" onClick={() => toggle(k.id)}>
                      <span className={`risk-stripe ${k.riskLevel}`} />
                      <span className="kr-code">{code}</span>
                      {/* #160 裁决：标题下一行始终显示负责人／周期／量化指标（普通元信息样式），
                          卡点／风险原因文字不再出现，风险表达只保留状态颜色。 */}
                      <span className="kr-title-cell">
                        <span>{k.description}</span>
                        <small>
                          负责人 {k.ownerName ?? "未指定"} · 周期 {fmtDate(k.startDate)}—
                          {fmtDate(k.endDate)} · 量化指标：{k.metric ?? "待补充"}
                        </small>
                      </span>
                      <span className={`status-pill risk-${k.riskLevel}`}>
                        {k.riskLevelLabel}
                      </span>
                      <span className="muted" aria-hidden>
                        <Icon name={isOpen ? "down" : "chevron"} size={15} />
                      </span>
                    </button>
                    {/* #160 裁决：展开后只显示任务列表，无进度汇总、输入输出、
                        风险依据或影响链入口；具体卡点经任务详情抽屉（#156）查看。 */}
                    {isOpen && (
                      <div className="kr-tasks">
                        {krTasks.length > 0 && (
                          <div className="mini-task mini-task-head" aria-hidden>
                            <span>编号</span>
                            <span>任务</span>
                            <span>负责人</span>
                            <span>状态</span>
                            <span>进度</span>
                            <span />
                          </div>
                        )}
                        {krTasks.length === 0 && (
                          <div className="muted" style={{ fontSize: 12, padding: "8px 0" }}>
                            该 KR 下暂无任务
                          </div>
                        )}
                        {krTasks.map((t) => (
                          <div key={t.id} className="mini-task">
                            <span className="mono">{taskCode.get(t.id)}</span>
                            <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                              {t.name}
                            </span>
                            <span className="owner-cell">
                              <span className="avatar">{t.ownerName.slice(0, 1)}</span>
                              {t.ownerName}
                            </span>
                            <span>
                              <span className={`status-pill ${STATUS_CLASS[t.status]}`}>
                                {t.statusLabel}
                              </span>
                            </span>
                            <span className="muted" style={{ fontSize: 12 }}>
                              {t.progress == null ? "未填进度" : `${t.progress}%`}
                            </span>
                            <button
                              type="button"
                              className="nav-row"
                              style={{ width: 28, height: 28, padding: 0, justifyContent: "center" }}
                              aria-label="查看任务"
                              onClick={() => setDrawerTask(t.id)}
                            >
                              <Icon name="chevron" size={15} />
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                );
              })}
            </section>
          ))}
        </>
      )}
      {/* #156：任务详情抽屉在本页打开（PRD §7.5、AC-31），动作落库后刷新总览数据。 */}
      <TaskDrawerHost
        projectId={projectId}
        taskId={drawerTask}
        onClose={() => setDrawerTask(null)}
        onChanged={load}
      />
    </ProjectShell>
  );
}


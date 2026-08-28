import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Spin } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";
import ProjectShell from "./ProjectShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type TaskStatus = components["schemas"]["TaskStatus"];
type RiskLevel = components["schemas"]["RiskLevel"];

const RISK_LABEL: Record<RiskLevel, string> = {
  normal: "正常",
  warning: "预警",
  high_risk: "高风险",
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

  const taskCode = useMemo(() => {
    const sorted = [...tasks].sort((a, b) => a.id - b.id);
    return new Map(sorted.map((t, i) => [t.id, `T${i + 1}`]));
  }, [tasks]);

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

  let krSeq = 0;

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
          {objectives.map((o, oIndex) => (
            <section key={o.id} className="objective">
              <div className="objective-head">
                <span className="objective-code">O{oIndex + 1}</span>
                <div>
                  <h2>{o.title}</h2>
                  <p>{o.description ?? ""}</p>
                </div>
                <span className="objective-count">{o.keyResults.length} 个 KR</span>
              </div>
              {o.keyResults.map((k) => {
                krSeq += 1;
                const code = `KR${krSeq}`;
                const isOpen = expanded.has(k.id);
                const krTasks = tasks.filter((t) => t.keyResultId === k.id);
                return (
                  <div key={k.id} className="kr-row">
                    <button type="button" className="kr-main" onClick={() => toggle(k.id)}>
                      <span className={`risk-stripe ${k.riskLevel}`} />
                      <span className="kr-code">{code}</span>
                      <span className="kr-title-cell">
                        <span>{k.description}</span>
                        {k.riskNote && <small>{k.riskNote}</small>}
                      </span>
                      <span className={`status-pill risk-${k.riskLevel}`}>
                        {RISK_LABEL[k.riskLevel]}
                      </span>
                      <span className="muted" aria-hidden>
                        <Icon name={isOpen ? "down" : "chevron"} size={15} />
                      </span>
                    </button>
                    {isOpen && (
                      <div className="kr-tasks">
                        <div className="kr-meta">
                          负责人 {k.ownerName ?? "未指定"} · 周期 {fmtDate(k.startDate)}—
                          {fmtDate(k.endDate)} · 量化指标：{k.metric ?? "待补充"}
                          {k.progressSummary && k.progressSummary.totalTasks > 0 && (
                            <>
                              {k.progressSummary.averageProgress != null &&
                                `　·　平均 ${k.progressSummary.averageProgress}%`}
                              　·　其中 {k.progressSummary.filledTasks}／
                              {k.progressSummary.totalTasks} 个任务由负责人填写，未填按 0 计入
                            </>
                          )}
                        </div>
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
                              onClick={() =>
                                navigate(`/projects/${projectId}/tasks?task=${t.id}&tab=overview`)
                              }
                            >
                              <Icon name="chevron" size={15} />
                            </button>
                          </div>
                        ))}
                        <div className="kr-graph-link">
                          <Button
                            type="link"
                            size="small"
                            style={{ padding: 0 }}
                            onClick={() => navigate(`/projects/${projectId}/graph?kr=${k.id}`)}
                          >
                            在协作全景中查看 {code} 影响链 →
                          </Button>
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </section>
          ))}
        </>
      )}
    </ProjectShell>
  );
}

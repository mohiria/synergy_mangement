import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Spin } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
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
          <div className="page-head">
            <div>
              <h1>项目总览</h1>
              <p>
                {project.plannedStartDate || project.plannedEndDate
                  ? `项目周期 ${fmtDate(project.plannedStartDate)}—${fmtDate(project.plannedEndDate)} · `
                  : ""}
                主负责人 {project.ownerName}
                {project.stage ? ` · ${project.stage}` : ""}
              </p>
            </div>
          </div>
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
                        {k.riskNote && (
                          <small>
                            {k.riskLevel === "high_risk" ? "风险因素" : "卡点"}：{k.riskNote}
                          </small>
                        )}
                      </span>
                      <span className={`status-pill risk-${k.riskLevel}`}>
                        {RISK_LABEL[k.riskLevel]}
                      </span>
                      <span className="muted">{isOpen ? "▾" : "▸"}</span>
                    </button>
                    {isOpen && (
                      <div className="kr-tasks">
                        <div className="kr-meta">
                          负责人 {k.ownerName ?? "未指定"} · 周期 {fmtDate(k.startDate)}—
                          {fmtDate(k.endDate)} · 量化指标：{k.metric ?? "待补充"}
                          {k.progressSummary && k.progressSummary.totalTasks > 0 && (
                            <>
                              　·　{k.progressSummary.filledTasks}／{k.progressSummary.totalTasks}
                              个任务已填写进度
                              {k.progressSummary.averageProgress != null &&
                                `，平均 ${k.progressSummary.averageProgress}%`}
                            </>
                          )}
                        </div>
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
                              ›
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
    </ProjectShell>
  );
}

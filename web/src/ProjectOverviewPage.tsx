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
type DeliverableEdge = components["schemas"]["DeliverableEdge"];
type Blocker = components["schemas"]["Blocker"];

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
  // KR 展开层要显示关键输入／输出、直接上下游 KR 与风险依据（§7.1），
  // 这三块的事实分别来自交付物边与派生卡点，故总览也要取这两份数据。
  const [edges, setEdges] = useState<DeliverableEdge[]>([]);
  const [blockers, setBlockers] = useState<Blocker[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());

  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, objectivesRes, tasksRes, edgesRes, blockersRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/objectives", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/tasks", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/edges", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/blockers", { params: { path: { projectId } } }),
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
    setEdges(edgesRes.data ?? []);
    setBlockers(blockersRes.data ?? []);
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

  // KR 归属与编号：直接上下游 KR 要按对方任务反查所属 KR，故先建两张索引表。
  const krOfTask = useMemo(() => new Map(tasks.map((t) => [t.id, t.keyResultId])), [tasks]);
  const krCode = useMemo(() => {
    const m = new Map<number, string>();
    objectives.forEach((o) => o.keyResults.forEach((k) => m.set(k.id, k.code)));
    return m;
  }, [objectives]);
  const krLabel = useMemo(() => {
    const m = new Map<number, string>();
    objectives.forEach((o) => o.keyResults.forEach((k) => m.set(k.id, k.description)));
    return m;
  }, [objectives]);

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
            <Button onClick={() => navigate(`/projects/${projectId}/graph`)}>查看协作全景</Button>
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
                <span className="objective-count">{o.keyResults.length} 个 KR</span>
              </div>
              {o.keyResults.map((k) => {
                const code = k.code;
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
                        {k.riskLevelLabel}
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
                        {/* §7.1 KR 展开层第四块：关键输入、输出与直接上下游 KR。
                            输入是别人交给本 KR 的、输出是本 KR 交出去的；上下游 KR 由对方任务所属 KR 反查，
                            同一 KR 合并成一条，本 KR 自身不算上下游。 */}
                        <KrRelationBlock
                          krId={k.id}
                          tasks={krTasks}
                          edges={edges}
                          krOfTask={krOfTask}
                          krCode={krCode}
                          krLabel={krLabel}
                        />
                        {/* §7.1 KR 展开层第五块：风险依据与下钻入口——
                            KR 颜色的由来在这里说清，并能一路点到具体任务与待行动人。 */}
                        <KrRiskBlock
                          riskLevel={k.riskLevel}
                          riskNote={k.riskNote}
                          blockers={blockers.filter((b) => krOfTask.get(b.taskId) === k.id)}
                          taskCode={taskCode}
                          onOpenTask={(taskId) =>
                            navigate(`/projects/${projectId}/tasks?task=${taskId}&tab=blockers`)
                          }
                        />
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

// KrRelationBlock KR 展开层的「关键输入／输出与直接上下游 KR」（§7.1）。
// 只看本 KR 下任务两端的交付物边：指向本 KR 任务的是输入，从本 KR 任务出发的是输出；
// 对方任务所属 KR 即直接上下游 KR，本 KR 自身不计。
function KrRelationBlock({
  krId,
  tasks,
  edges,
  krOfTask,
  krCode,
  krLabel,
}: {
  krId: number;
  tasks: Task[];
  edges: DeliverableEdge[];
  krOfTask: Map<number, number>;
  krCode: Map<number, string>;
  krLabel: Map<number, string>;
}) {
  const taskIds = new Set(tasks.map((t) => t.id));
  const inputs = edges.filter((e) => taskIds.has(e.targetTaskId));
  const outputs = edges.filter((e) => e.sourceTaskId != null && taskIds.has(e.sourceTaskId));
  const upstreamKrs = new Set<number>();
  inputs.forEach((e) => {
    const other = e.sourceTaskId != null ? krOfTask.get(e.sourceTaskId) : undefined;
    if (other != null && other !== krId) upstreamKrs.add(other);
  });
  const downstreamKrs = new Set<number>();
  outputs.forEach((e) => {
    const other = krOfTask.get(e.targetTaskId);
    if (other != null && other !== krId) downstreamKrs.add(other);
  });
  const krNames = (ids: Set<number>) =>
    [...ids].map((id) => `${krCode.get(id) ?? ""} ${krLabel.get(id) ?? ""}`.trim()).join("、");

  if (inputs.length === 0 && outputs.length === 0) {
    return (
      <div className="kr-relation-block muted" style={{ fontSize: 12 }}>
        关键输入／输出：该 KR 下任务尚未配置交付物关系
      </div>
    );
  }
  return (
    <div className="kr-relation-block">
      <div>
        <b>关键输入</b>
        {inputs.length === 0 ? (
          <span className="muted">　—</span>
        ) : (
          <span>
            　
            {inputs
              .map((e) => `${e.name}${e.ready ? "（已就绪）" : "（未就绪）"}`)
              .join("、")}
          </span>
        )}
      </div>
      <div>
        <b>关键输出</b>
        {outputs.length === 0 ? (
          <span className="muted">　—</span>
        ) : (
          <span>　{outputs.map((e) => e.name).join("、")}</span>
        )}
      </div>
      <div>
        <b>直接上游 KR</b>
        <span className={upstreamKrs.size === 0 ? "muted" : undefined}>
          　{upstreamKrs.size === 0 ? "—" : krNames(upstreamKrs)}
        </span>
      </div>
      <div>
        <b>直接下游 KR</b>
        <span className={downstreamKrs.size === 0 ? "muted" : undefined}>
          　{downstreamKrs.size === 0 ? "—" : krNames(downstreamKrs)}
        </span>
      </div>
    </div>
  );
}

// KrRiskBlock KR 展开层的「风险依据与下钻入口」（§7.1 风险下钻链条第二环）：
// KR 颜色来自下属任务的卡点与临期／超期事实，这里把那些事实逐条列出来，
// 每条给出待行动人并可点进任务的卡点区块，链条不再断在风险摘要。
function KrRiskBlock({
  riskLevel,
  riskNote,
  blockers,
  taskCode,
  onOpenTask,
}: {
  riskLevel: RiskLevel;
  riskNote?: string;
  blockers: Blocker[];
  taskCode: Map<number, string>;
  onOpenTask: (taskId: number) => void;
}) {
  if (riskLevel === "normal" && blockers.length === 0) {
    return (
      <div className="kr-relation-block muted" style={{ fontSize: 12 }}>
        风险依据：当前无卡点，也未临近或超过截止时间
      </div>
    );
  }
  return (
    <div className="kr-relation-block">
      <b>风险依据</b>
      {riskNote && <div className="muted">　{riskNote}</div>}
      {blockers.length === 0 ? (
        <div className="muted">　该等级来自任务临期或超期，无结构化卡点</div>
      ) : (
        blockers.map((b) => (
          <button
            key={b.key}
            type="button"
            className="fact-card fact-card-link"
            style={{ marginTop: 6 }}
            onClick={() => onOpenTask(b.taskId)}
          >
            <span>
              <b>
                {taskCode.get(b.taskId) ?? ""} · {b.taskName} · {b.kindLabel}
              </b>
              <small>
                {b.reason}
                {b.missing ? ` · 缺 ${b.missing}` : ""} · 待行动人{" "}
                {b.actionOwnerNames.length > 0 ? b.actionOwnerNames.join("、") : "未指定"}
              </small>
            </span>
            <span className={`status-pill risk-${b.level}`}>
              {b.level === "high_risk" ? "高风险" : "预警"}
            </span>
          </button>
        ))
      )}
    </div>
  );
}

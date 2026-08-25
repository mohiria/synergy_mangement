import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Spin } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type DeliverableEdge = components["schemas"]["DeliverableEdge"];
type Blocker = components["schemas"]["Blocker"];
type RiskLevel = components["schemas"]["RiskLevel"];

const RISK_LABEL: Record<RiskLevel, string> = {
  normal: "正常",
  warning: "预警",
  high_risk: "高风险",
};

type Mode = { kind: "tree" } | { kind: "o"; objectiveId: number } | { kind: "kr"; krId: number };

type NodePos = { x: number; y: number; w: number; h: number };

// 协作关系（AC-08）：默认 O／KR 层级树（层级连线不可点击、无 KR↔KR 汇总边），
// 点击 O 聚焦下属 KR，点击 KR 下钻任务关系层（本 KR 任务 + 直接相连的其他 KR 任务 + 真实交付物边）。
export default function CollaborationPage({
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
  const [edges, setEdges] = useState<DeliverableEdge[]>([]);
  const [blockers, setBlockers] = useState<Blocker[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [mode, setMode] = useState<Mode>({ kind: "tree" });
  const [history, setHistory] = useState<Mode[]>([]);
  const [selectedTask, setSelectedTask] = useState<number | null>(null);

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

  const krList = useMemo(() => {
    let seq = 0;
    return objectives.flatMap((o) =>
      o.keyResults.map((k) => ({ ...k, code: `KR${++seq}`, objectiveId: o.id })),
    );
  }, [objectives]);
  const krById = useMemo(() => new Map(krList.map((k) => [k.id, k])), [krList]);

  const enter = (next: Mode) => {
    setHistory((h) => [...h, mode]);
    setMode(next);
    setSelectedTask(null);
  };
  const back = () => {
    setHistory((h) => {
      if (h.length === 0) return h;
      setMode(h[h.length - 1]);
      setSelectedTask(null);
      return h.slice(0, -1);
    });
  };

  const openBlockers = blockers.filter((b) => b.state === "open");

  // —— O／KR 层级树布局（含 O 聚焦模式）——
  const tree = useMemo(() => {
    const nodes: { key: string; kind: "o" | "kr"; id: number; pos: NodePos; dimmed: boolean }[] = [];
    const lines: { x1: number; y1: number; x2: number; y2: number; dimmed: boolean }[] = [];
    const oX = 30;
    const krX = 340;
    const oW = 220;
    const oH = 58;
    const krW = 300;
    const krH = 62;
    let y = 20;
    const focusO = mode.kind === "o" ? mode.objectiveId : null;
    for (const o of objectives) {
      const krs = krList.filter((k) => k.objectiveId === o.id);
      const blockHeight = Math.max(krs.length * (krH + 18) - 18, oH);
      const oY = y + blockHeight / 2 - oH / 2;
      const dimmed = focusO != null && focusO !== o.id;
      nodes.push({ key: `o-${o.id}`, kind: "o", id: o.id, pos: { x: oX, y: oY, w: oW, h: oH }, dimmed });
      krs.forEach((k, i) => {
        const kY = y + i * (krH + 18);
        nodes.push({ key: `kr-${k.id}`, kind: "kr", id: k.id, pos: { x: krX, y: kY, w: krW, h: krH }, dimmed });
        lines.push({
          x1: oX + oW,
          y1: oY + oH / 2,
          x2: krX,
          y2: kY + krH / 2,
          dimmed,
        });
      });
      y += blockHeight + 34;
    }
    return { nodes, lines, height: y + 20 };
  }, [objectives, krList, mode]);

  // —— KR 任务关系层布局 ——
  const krLayer = useMemo(() => {
    if (mode.kind !== "kr") return null;
    const kr = krById.get(mode.krId);
    if (!kr) return null;
    const inKr = tasks.filter((t) => t.keyResultId === mode.krId);
    const inKrIds = new Set(inKr.map((t) => t.id));
    // 直接相连的其他 KR 任务（真实交付物边）。
    const relevantEdges = edges.filter(
      (e) =>
        (e.sourceTaskId != null && inKrIds.has(e.sourceTaskId)) || inKrIds.has(e.targetTaskId),
    );
    const neighborIds = new Set<number>();
    for (const e of relevantEdges) {
      if (e.sourceTaskId != null && !inKrIds.has(e.sourceTaskId)) neighborIds.add(e.sourceTaskId);
      if (!inKrIds.has(e.targetTaskId)) neighborIds.add(e.targetTaskId);
    }
    const neighbors = tasks.filter((t) => neighborIds.has(t.id));
    const nodeW = 250;
    const nodeH = 66;
    const gap = 16;
    const positions = new Map<number, NodePos>();
    inKr.forEach((t, i) => {
      positions.set(t.id, { x: 360, y: 24 + i * (nodeH + gap), w: nodeW, h: nodeH });
    });
    neighbors.forEach((t, i) => {
      positions.set(t.id, { x: 40, y: 24 + i * (nodeH + gap), w: nodeW, h: nodeH });
    });
    const height = Math.max(inKr.length, neighbors.length) * (nodeH + gap) + 60;
    // 成员来源节点（#14 输入请求）。
    const memberNodes: { key: string; label: string; pos: NodePos; edgeId: number }[] = [];
    relevantEdges.forEach((e, i) => {
      if (e.sourceTaskId == null && e.inputRequest) {
        memberNodes.push({
          key: `m-${e.id}`,
          label: e.inputRequest.providerName,
          pos: { x: 40, y: height - 40 + i * 0, w: 180, h: 44 },
          edgeId: e.id,
        });
        positions.set(-e.id, { x: 40, y: height - 40, w: 180, h: 44 });
      }
    });
    return { kr, inKr, neighbors, relevantEdges, positions, memberNodes, height: height + 60 };
  }, [mode, krById, tasks, edges]);

  const edgePath = (from: NodePos, to: NodePos) => {
    const x1 = from.x + from.w;
    const y1 = from.y + from.h / 2;
    const x2 = to.x;
    const y2 = to.y + to.h / 2;
    const mx = (x1 + x2) / 2;
    return `M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`;
  };

  const krNodeContent = (krId: number) => {
    const k = krById.get(krId);
    if (!k) return null;
    const taskCount = k.progressSummary?.totalTasks ?? 0;
    const blockerCount = k.openBlockerCount ?? 0;
    return (
      <>
        <b>
          {k.code} {k.description}
        </b>
        <small>
          {taskCount} 项任务 · {blockerCount} 个卡点 · {RISK_LABEL[k.riskLevel]}
        </small>
      </>
    );
  };

  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="协作关系"
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
              <h1>协作关系</h1>
              <p>交付物作为关系边连接来源和目标；本模块只读，业务处理从任务相关页面进入。</p>
            </div>
            <div style={{ display: "flex", gap: 8 }}>
              <Button disabled={history.length === 0} onClick={back}>
                ← 返回上一级
              </Button>
            </div>
          </div>
          <div className="graph-layout">
            <aside className="risk-queue">
              <div className="risk-queue-head">风险队列</div>
              {krList.filter((k) => k.riskLevel !== "normal").length === 0 &&
                openBlockers.length === 0 && (
                  <div className="muted" style={{ padding: 16, fontSize: 12 }}>
                    暂无需要关注的风险
                  </div>
                )}
              {krList
                .filter((k) => k.riskLevel !== "normal")
                .map((k) => (
                  <button
                    key={`rk-${k.id}`}
                    type="button"
                    className="risk-queue-item"
                    onClick={() => enter({ kind: "kr", krId: k.id })}
                  >
                    <b>
                      {k.code} · {RISK_LABEL[k.riskLevel]}
                    </b>
                    <small>{k.riskNote ?? k.description}</small>
                  </button>
                ))}
              {openBlockers.map((b) => {
                const task = tasks.find((t) => t.id === b.taskId);
                const krId = task?.keyResultId;
                return (
                  <button
                    key={`rb-${b.id}`}
                    type="button"
                    className="risk-queue-item"
                    onClick={() => {
                      if (krId) {
                        enter({ kind: "kr", krId });
                        setSelectedTask(b.taskId);
                      }
                    }}
                  >
                    <b>{b.level === "high_risk" ? "高风险卡点" : "预警卡点"}</b>
                    <small>
                      {task?.name ?? ""}：缺 {b.missing}
                    </small>
                  </button>
                );
              })}
            </aside>
            <div className="graph-shell">
              {mode.kind !== "kr" ? (
                <div className="graph-canvas-inner" style={{ height: tree.height, minWidth: 700 }}>
                  <div className="graph-note">
                    默认只显示 O、KR 与层级连线（不可点击）；点击 O 聚焦下属 KR，点击 KR 进入任务关系层
                  </div>
                  <svg className="graph-svg" width="700" height={tree.height}>
                    {tree.lines.map((l, i) => (
                      <path
                        key={i}
                        d={`M ${l.x1} ${l.y1} C ${(l.x1 + l.x2) / 2} ${l.y1}, ${(l.x1 + l.x2) / 2} ${l.y2}, ${l.x2} ${l.y2}`}
                        fill="none"
                        stroke="#b8c4ce"
                        strokeWidth={1.6}
                        opacity={l.dimmed ? 0.2 : 1}
                      />
                    ))}
                  </svg>
                  {tree.nodes.map((n) =>
                    n.kind === "o" ? (
                      <div
                        key={n.key}
                        className={`gnode gnode-o ${n.dimmed ? "dimmed" : ""}`}
                        style={{ left: n.pos.x, top: n.pos.y, width: n.pos.w, height: n.pos.h }}
                        onClick={() => enter({ kind: "o", objectiveId: n.id })}
                      >
                        <b>{objectives.find((o) => o.id === n.id)?.title}</b>
                        <small>
                          {krList.filter((k) => k.objectiveId === n.id).length} 个 KR
                        </small>
                      </div>
                    ) : (
                      <div
                        key={n.key}
                        className={`gnode ${n.dimmed ? "dimmed" : ""} ${
                          krById.get(n.id)?.riskLevel !== "normal"
                            ? `risk-${krById.get(n.id)?.riskLevel}`
                            : ""
                        }`}
                        style={{ left: n.pos.x, top: n.pos.y, width: n.pos.w, height: n.pos.h }}
                        onClick={() => enter({ kind: "kr", krId: n.id })}
                      >
                        {krNodeContent(n.id)}
                      </div>
                    ),
                  )}
                </div>
              ) : krLayer ? (
                <div
                  className="graph-canvas-inner"
                  style={{ height: krLayer.height, minWidth: 700 }}
                >
                  <div className="graph-note">
                    {krLayer.kr.code} 任务关系层：本 KR 全部任务与直接相连的其他 KR 任务；环形与双向关系保留真实连线
                  </div>
                  <svg className="graph-svg" width="700" height={krLayer.height}>
                    {krLayer.relevantEdges.map((e) => {
                      const from =
                        e.sourceTaskId != null
                          ? krLayer.positions.get(e.sourceTaskId)
                          : krLayer.positions.get(-e.id);
                      const to = krLayer.positions.get(e.targetTaskId);
                      if (!from || !to) return null;
                      const hard = e.edgeType === "hard_prerequisite";
                      const feedback = e.edgeType === "feedback";
                      return (
                        <path
                          key={e.id}
                          d={edgePath(from, to)}
                          fill="none"
                          stroke={feedback ? "#5a62c9" : hard ? "#436d84" : "#8ea3b0"}
                          strokeWidth={hard ? 2.6 : 1.6}
                          strokeDasharray={feedback ? "4 4" : undefined}
                        />
                      );
                    })}
                  </svg>
                  {[...krLayer.inKr, ...krLayer.neighbors].map((t) => {
                    const pos = krLayer.positions.get(t.id);
                    if (!pos) return null;
                    const taskBlockers = openBlockers.filter((b) => b.taskId === t.id);
                    const risk =
                      taskBlockers.some((b) => b.level === "high_risk")
                        ? "high_risk"
                        : taskBlockers.length > 0
                          ? "warning"
                          : "";
                    return (
                      <div
                        key={t.id}
                        className={`gnode ${risk ? `risk-${risk}` : ""} ${
                          selectedTask === t.id ? "selected" : ""
                        }`}
                        style={{ left: pos.x, top: pos.y, width: pos.w, height: pos.h }}
                        onClick={() =>
                          setSelectedTask((prev) => (prev === t.id ? null : t.id))
                        }
                        onDoubleClick={() =>
                          navigate(`/projects/${projectId}/tasks?task=${t.id}&tab=overview`)
                        }
                      >
                        <b>{t.name}</b>
                        <small>
                          {krById.get(t.keyResultId)?.code} · {t.ownerName} ·{" "}
                          {t.status === "completed" ? "已完成" : t.currentStage}
                          {taskBlockers.length > 0 && ` · ${taskBlockers.length} 个卡点`}
                        </small>
                      </div>
                    );
                  })}
                  {krLayer.memberNodes.map((m) => (
                    <div
                      key={m.key}
                      className="gnode gnode-member"
                      style={{
                        left: krLayer.positions.get(-m.edgeId)?.x,
                        top: krLayer.positions.get(-m.edgeId)?.y,
                        width: 180,
                        height: 44,
                      }}
                    >
                      <b>{m.label}</b>
                      <small>输入提供成员</small>
                    </div>
                  ))}
                </div>
              ) : null}
            </div>
          </div>
        </>
      )}
    </ProjectShell>
  );
}

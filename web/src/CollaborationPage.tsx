import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { Alert, AutoComplete, Button, Select, Spin, Switch } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type TaskDetail = components["schemas"]["TaskDetail"];
type DeliverableEdge = components["schemas"]["DeliverableEdge"];
type Blocker = components["schemas"]["Blocker"];
type RiskLevel = components["schemas"]["RiskLevel"];
type TaskStatus = components["schemas"]["TaskStatus"];

const RISK_LABEL: Record<RiskLevel, string> = {
  normal: "正常",
  warning: "预警",
  high_risk: "高风险",
};
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

const EDGE_TYPE_LABEL: Record<string, string> = {
  hard_prerequisite: "硬前置交付",
  information: "信息输入",
  handover: "正式成果接收",
  feedback: "迭代／反馈",
};

type Mode = { kind: "tree" } | { kind: "o"; objectiveId: number } | { kind: "kr"; krId: number } | { kind: "full" };

type NodePos = { x: number; y: number; w: number; h: number };

// 协作关系（AC-08/09/27）：O／KR 层级树 → KR 任务关系层 → 全局展开。
// 全局展开保留 O/KR 分组骨架、真实交付物边（环形/多中心/跨层级不转树）、缩放与筛选淡化。
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
  const [inspectorDetail, setInspectorDetail] = useState<TaskDetail | null>(null);
  const [zoom, setZoom] = useState(1);
  const [oFilter, setOFilter] = useState<number | "all">("all");
  const [krFilter, setKrFilter] = useState<number | "all">("all");
  const [personFilter, setPersonFilter] = useState<number | "all">("all");
  const [showCompleted, setShowCompleted] = useState(false);
  const [impactMode, setImpactMode] = useState(false);
  const [selectedEdge, setSelectedEdge] = useState<number | null>(null);
  const [dragOffsets, setDragOffsets] = useState<Map<number, { dx: number; dy: number }>>(new Map());
  const [searchText, setSearchText] = useState("");
  const [searchParams] = useSearchParams();

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

  // 从任务详情进入：聚焦当前任务并展示一层直接关系（AC-42）。
  const focusTaskParam = searchParams.get("task");
  useEffect(() => {
    if (!focusTaskParam || loading) return;
    const t = tasks.find((x) => x.id === Number(focusTaskParam));
    if (t) {
      setMode({ kind: "kr", krId: t.keyResultId });
      setHistory([{ kind: "tree" }]);
      setSelectedTask(t.id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusTaskParam, loading]);

  // 选中任务 → 右侧详情（AC-27）。
  useEffect(() => {
    if (selectedTask == null) {
      setInspectorDetail(null);
      return;
    }
    let alive = true;
    client
      .GET("/projects/{projectId}/tasks/{taskId}", {
        params: { path: { projectId, taskId: selectedTask } },
      })
      .then((res) => {
        if (alive && res.data) setInspectorDetail(res.data);
      });
    return () => {
      alive = false;
    };
  }, [projectId, selectedTask]);

  const krList = useMemo(() => {
    let seq = 0;
    return objectives.flatMap((o) =>
      o.keyResults.map((k) => ({ ...k, code: `KR${++seq}`, objectiveId: o.id })),
    );
  }, [objectives]);
  const krById = useMemo(() => new Map(krList.map((k) => [k.id, k])), [krList]);
  const taskById = useMemo(() => new Map(tasks.map((t) => [t.id, t])), [tasks]);

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

  // —— O／KR 层级树布局 ——
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
        lines.push({ x1: oX + oW, y1: oY + oH / 2, x2: krX, y2: kY + krH / 2, dimmed });
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
    const relevantEdges = edges.filter(
      (e) => (e.sourceTaskId != null && inKrIds.has(e.sourceTaskId)) || inKrIds.has(e.targetTaskId),
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
    inKr.forEach((t, i) => positions.set(t.id, { x: 360, y: 24 + i * (nodeH + gap), w: nodeW, h: nodeH }));
    neighbors.forEach((t, i) => positions.set(t.id, { x: 40, y: 24 + i * (nodeH + gap), w: nodeW, h: nodeH }));
    let height = Math.max(inKr.length, neighbors.length) * (nodeH + gap) + 60;
    const memberNodes: { edgeId: number; label: string }[] = [];
    relevantEdges.forEach((e) => {
      if (e.sourceTaskId == null && e.inputRequest) {
        positions.set(-e.id, { x: 40, y: height, w: 180, h: 44 });
        memberNodes.push({ edgeId: e.id, label: e.inputRequest.providerName });
        height += 56;
      }
    });
    return { kr, inKr, neighbors, relevantEdges, positions, memberNodes, height: height + 40 };
  }, [mode, krById, tasks, edges]);

  // —— 全局展开布局（AC-09）：O/KR 分组骨架 + 全部任务 + 关系相关项目成员 ——
  const full = useMemo(() => {
    if (mode.kind !== "full") return null;
    const visibleTasks = tasks.filter(
      (t) => t.status !== "cancelled" && (showCompleted || t.status !== "completed"),
    );
    const visibleIds = new Set(visibleTasks.map((t) => t.id));
    const nodeW = 240;
    const nodeH = 62;
    const positions = new Map<number, NodePos>();
    const groups: { key: string; label: string; pos: NodePos; isO: boolean }[] = [];
    let y = 16;
    const groupX = 24;
    const groupW = 2 * (nodeW + 14) + 28;
    for (const o of objectives) {
      groups.push({ key: `o-${o.id}`, label: o.title, pos: { x: groupX, y, w: groupW, h: 30 }, isO: true });
      y += 38;
      for (const k of krList.filter((k) => k.objectiveId === o.id)) {
        const krTasks = visibleTasks.filter((t) => t.keyResultId === k.id);
        const rows = Math.max(1, Math.ceil(krTasks.length / 2));
        const boxH = rows * (nodeH + 12) + 40;
        groups.push({ key: `kr-${k.id}`, label: `${k.code} ${k.description}`, pos: { x: groupX, y, w: groupW, h: boxH }, isO: false });
        krTasks.forEach((t, i) => {
          const cx = groupX + 14 + (i % 2) * (nodeW + 14);
          const cy = y + 32 + Math.floor(i / 2) * (nodeH + 12);
          positions.set(t.id, { x: cx, y: cy, w: nodeW, h: nodeH });
        });
        y += boxH + 14;
      }
      y += 10;
    }
    // 关系相关项目成员节点（词汇表）：承担输入责任的成员。
    const memberEdges = edges.filter((e) => e.sourceTaskId == null && e.inputRequest && visibleIds.has(e.targetTaskId));
    const memberByProvider = new Map<number, { name: string; edgeIds: number[] }>();
    for (const e of memberEdges) {
      const pid = e.inputRequest!.providerId;
      const entry = memberByProvider.get(pid) ?? { name: e.inputRequest!.providerName, edgeIds: [] };
      entry.edgeIds.push(e.id);
      memberByProvider.set(pid, entry);
    }
    const memberNodes: { providerId: number; name: string; pos: NodePos; edgeIds: number[] }[] = [];
    let mi = 0;
    for (const [pid, entry] of memberByProvider) {
      const pos = { x: groupX + 14 + (mi % 2) * (nodeW + 14), y: y + 8 + Math.floor(mi / 2) * 56, w: 200, h: 44 };
      memberNodes.push({ providerId: pid, name: entry.name, pos, edgeIds: entry.edgeIds });
      for (const eid of entry.edgeIds) positions.set(-eid, pos);
      mi++;
    }
    if (memberNodes.length > 0) y += Math.ceil(memberNodes.length / 2) * 56 + 24;
    const visibleEdges = edges.filter((e) => {
      const targetOK = visibleIds.has(e.targetTaskId);
      const sourceOK = e.sourceTaskId != null ? visibleIds.has(e.sourceTaskId) : positions.has(-e.id);
      return targetOK && sourceOK;
    });
    return { visibleTasks, positions, groups, memberNodes, visibleEdges, height: y + 30, width: groupX + groupW + 40 };
  }, [mode, tasks, edges, objectives, krList, showCompleted]);

  // 筛选淡化（AC-09；细化 AC-45 随 #20）：O/KR/人员不匹配 → 淡化保留上下文。
  const taskMatchesFilter = (t: Task) => {
    if (krFilter !== "all" && t.keyResultId !== krFilter) return false;
    if (oFilter !== "all" && krById.get(t.keyResultId)?.objectiveId !== oFilter) return false;
    if (personFilter !== "all" && t.ownerId !== personFilter) return false;
    return true;
  };
  const hasFilter = oFilter !== "all" || krFilter !== "all" || personFilter !== "all";

  // 选中聚焦：一层邻居强化，其余降噪（AC-27）。
  const neighborIds = useMemo(() => {
    if (selectedEdge != null) {
      const e = edges.find((x) => x.id === selectedEdge);
      if (e) {
        const set = new Set<number>([e.targetTaskId]);
        if (e.sourceTaskId != null) set.add(e.sourceTaskId);
        return set;
      }
    }
    if (selectedTask == null) return null;
    const set = new Set<number>([selectedTask]);
    if (impactMode) {
      // 影响路径（AC-42）：沿下游硬前置边可达的连续链路。
      const queue = [selectedTask];
      while (queue.length > 0) {
        const cur = queue.shift()!;
        for (const e of edges) {
          if (e.edgeType === "hard_prerequisite" && e.sourceTaskId === cur && !set.has(e.targetTaskId)) {
            set.add(e.targetTaskId);
            queue.push(e.targetTaskId);
          }
        }
      }
      return set;
    }
    for (const e of edges) {
      if (e.sourceTaskId === selectedTask) set.add(e.targetTaskId);
      if (e.targetTaskId === selectedTask && e.sourceTaskId != null) set.add(e.sourceTaskId);
    }
    return set;
  }, [selectedTask, selectedEdge, edges, impactMode]);

  // 候选式搜索（AC-44）：按类型进入对应层级；卡点候选定位所属任务。
  const searchOptions = useMemo(() => {
    const opts: { value: string; label: string }[] = [];
    objectives.forEach((o) => opts.push({ value: `o:${o.id}`, label: `O · ${o.title}` }));
    krList.forEach((k) => opts.push({ value: `kr:${k.id}`, label: `${k.code} · ${k.description}` }));
    tasks.forEach((t) => opts.push({ value: `task:${t.id}`, label: `任务 · ${t.name}` }));
    const providers = new Map<number, string>();
    edges.forEach((e) => {
      if (e.inputRequest) providers.set(e.inputRequest.providerId, e.inputRequest.providerName);
    });
    providers.forEach((name, id) => opts.push({ value: `member:${id}`, label: `成员 · ${name}` }));
    edges.forEach((e) => opts.push({ value: `edge:${e.id}`, label: `关系 · ${e.name}（→ ${e.targetTaskName ?? ""}）` }));
    openBlockers.forEach((b) =>
      opts.push({ value: `blocker:${b.id}`, label: `卡点 · ${taskById.get(b.taskId)?.name ?? ""}：缺 ${b.missing}` }),
    );
    return opts;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [objectives, krList, tasks, edges, blockers]);

  const onSearchSelect = (v: string) => {
    const [kind, idStr] = v.split(":");
    const id = Number(idStr);
    setSelectedEdge(null);
    setImpactMode(false);
    switch (kind) {
      case "o":
        enter({ kind: "o", objectiveId: id });
        break;
      case "kr":
        enter({ kind: "kr", krId: id });
        break;
      case "task": {
        const t = taskById.get(id);
        if (t) {
          enter({ kind: "kr", krId: t.keyResultId });
          setSelectedTask(id);
        }
        break;
      }
      case "member":
        enter({ kind: "full" });
        break;
      case "edge": {
        const e = edges.find((x) => x.id === id);
        const t = e ? taskById.get(e.targetTaskId) : undefined;
        if (t) {
          enter({ kind: "kr", krId: t.keyResultId });
          setSelectedEdge(id);
        }
        break;
      }
      case "blocker": {
        const b = blockers.find((x) => x.id === id);
        const t = b ? taskById.get(b.taskId) : undefined;
        if (t) {
          enter({ kind: "kr", krId: t.keyResultId });
          setSelectedTask(t.id);
        }
        break;
      }
    }
  };

  const edgePath = (from: NodePos, to: NodePos) => {
    const x1 = from.x + from.w;
    const y1 = from.y + from.h / 2;
    const x2 = to.x;
    const y2 = to.y + to.h / 2;
    const mx = (x1 + x2) / 2;
    return `M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`;
  };

  const edgeStroke = (e: DeliverableEdge) => {
    const interlock = !!e.interlockRisk;
    const feedback = e.edgeType === "feedback";
    const hard = e.edgeType === "hard_prerequisite";
    return {
      stroke: interlock ? "#c44752" : feedback ? "#5a62c9" : hard ? "#436d84" : "#8ea3b0",
      width: e.onCriticalPath ? 3.2 : hard ? 2.4 : 1.6,
      dash: interlock ? "5 3" : feedback ? "4 4" : undefined,
    };
  };

  const startDrag = (taskId: number, startX: number, startY: number) => {
    if (mode.kind !== "full") return;
    const base = dragOffsets.get(taskId) ?? { dx: 0, dy: 0 };
    let moved = false;
    const onMove = (ev: MouseEvent) => {
      const dx = base.dx + (ev.clientX - startX) / zoom;
      const dy = base.dy + (ev.clientY - startY) / zoom;
      if (Math.abs(dx - base.dx) > 3 || Math.abs(dy - base.dy) > 3) moved = true;
      setDragOffsets((prev) => {
        const next = new Map(prev);
        next.set(taskId, { dx, dy });
        return next;
      });
    };
    const onUp = () => {
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
      void moved;
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  };

  const withOffset = (taskId: number, pos: NodePos): NodePos => {
    const off = dragOffsets.get(taskId);
    if (!off || mode.kind !== "full") return pos;
    return { ...pos, x: pos.x + off.dx, y: pos.y + off.dy };
  };

  const taskNode = (t: Task, posBase: NodePos) => {
    const pos = withOffset(t.id, posBase);
    const taskBlockers = openBlockers.filter((b) => b.taskId === t.id);
    const risk = taskBlockers.some((b) => b.level === "high_risk")
      ? "high_risk"
      : taskBlockers.length > 0
        ? "warning"
        : "";
    const dimByFilter = hasFilter && !taskMatchesFilter(t);
    const dimBySelect = neighborIds != null && !neighborIds.has(t.id);
    return (
      <div
        key={t.id}
        className={`gnode ${risk ? `risk-${risk}` : ""} ${selectedTask === t.id ? "selected" : ""} ${
          dimByFilter || dimBySelect ? "dimmed" : ""
        }`}
        style={{ left: pos.x, top: pos.y, width: pos.w, height: pos.h }}
        onMouseDown={(ev) => startDrag(t.id, ev.clientX, ev.clientY)}
        onClick={() => {
          setImpactMode(false);
          setSelectedEdge(null);
          setSelectedTask((prev) => (prev === t.id ? null : t.id));
        }}
        onDoubleClick={() => navigate(`/projects/${projectId}/tasks?task=${t.id}&tab=overview`)}
      >
        <b>{t.name}</b>
        <small>
          {krById.get(t.keyResultId)?.code} · {t.ownerName} · {STATUS_LABEL[t.status]}
          {taskBlockers.length > 0 && ` · ${taskBlockers.length} 个卡点`}
        </small>
      </div>
    );
  };

  const selectedEdgeObj = selectedEdge != null ? edges.find((e) => e.id === selectedEdge) : null;
  const edgeInspector = selectedEdgeObj && (
    <aside
      style={{
        position: "absolute",
        right: 0,
        top: 0,
        bottom: 0,
        width: 300,
        background: "#fff",
        borderLeft: "1px solid var(--line)",
        overflow: "auto",
        padding: 14,
        zIndex: 5,
      }}
    >
      <b style={{ fontSize: 14 }}>交付物边 · {selectedEdgeObj.name}</b>
      <div style={{ fontSize: 13, display: "grid", gap: 6, marginTop: 8 }}>
        <div>关系类型：{EDGE_TYPE_LABEL[selectedEdgeObj.edgeType]}</div>
        <div>必要性：{selectedEdgeObj.necessity === "required" ? "必要" : "参考"}</div>
        <div>
          提供方：
          {selectedEdgeObj.sourceTaskName ??
            selectedEdgeObj.inputRequest?.providerName ??
            selectedEdgeObj.sourceOwnerName ??
            "—"}
        </div>
        <div>接收方：{selectedEdgeObj.targetTaskName ?? "—"}</div>
        <div>
          就绪状态：
          <span className={`status-pill ${selectedEdgeObj.ready ? "completed" : "warning"}`}>
            {selectedEdgeObj.ready ? "已就绪" : "未就绪"}
          </span>
          {selectedEdgeObj.hasCandidate && <span className="muted">　候选更新审核中</span>}
        </div>
        {selectedEdgeObj.interlockRisk && (
          <div style={{ color: "var(--red)" }}>硬前置循环：互锁风险</div>
        )}
        <div>
          当前交付物：
          {selectedEdgeObj.currentFileName ?? "（暂无已生效内容）"}
        </div>
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
          {selectedEdgeObj.sourceTaskId != null && (
            <Button
              size="small"
              onClick={() =>
                navigate(`/projects/${projectId}/tasks?task=${selectedEdgeObj.sourceTaskId}&tab=overview`)
              }
            >
              打开来源任务
            </Button>
          )}
          <Button
            size="small"
            onClick={() =>
              navigate(`/projects/${projectId}/tasks?task=${selectedEdgeObj.targetTaskId}&tab=overview`)
            }
          >
            打开目标任务
          </Button>
        </div>
        <div className="muted" style={{ fontSize: 12 }}>
          关系详情为只读；关系维护从任务详情的「配置输入」进入。
        </div>
      </div>
    </aside>
  );

  const inspector = selectedTask != null && inspectorDetail && (
    <aside
      style={{
        position: "absolute",
        right: 0,
        top: 0,
        bottom: 0,
        width: 300,
        background: "#fff",
        borderLeft: "1px solid var(--line)",
        overflow: "auto",
        padding: 14,
        zIndex: 5,
      }}
    >
      <b style={{ fontSize: 14 }}>{inspectorDetail.task.name}</b>
      <div className="muted" style={{ fontSize: 12, marginBottom: 8 }}>
        所属：{inspectorDetail.objectiveTitle} / {inspectorDetail.krDescription}
      </div>
      <div style={{ fontSize: 13, display: "grid", gap: 6 }}>
        <div>负责人：{inspectorDetail.task.ownerName}</div>
        <div>
          状态：<span className="status-pill">{STATUS_LABEL[inspectorDetail.task.status]}</span>
          {inspectorDetail.task.progress != null && ` · ${inspectorDetail.task.progress}%`}
        </div>
        <div>
          输入 {inspectorDetail.inputs.length} 条 / 输出 {inspectorDetail.outputs.length} 条
        </div>
        {inspectorDetail.inputs.map((e) => (
          <div key={e.id} className="muted" style={{ fontSize: 12 }}>
            ← {e.sourceTaskName ?? e.inputRequest?.providerName} · {e.name} ·{" "}
            {e.ready ? "已就绪" : "未就绪"}
          </div>
        ))}
        {inspectorDetail.outputs.map((e) => (
          <div key={e.id} className="muted" style={{ fontSize: 12 }}>
            → {e.targetTaskName} · {e.name}
          </div>
        ))}
        {inspectorDetail.blockers.filter((b) => b.state === "open").length > 0 && (
          <div style={{ color: "var(--red)", fontSize: 12 }}>
            卡点：
            {inspectorDetail.blockers
              .filter((b) => b.state === "open")
              .map((b) => `缺 ${b.missing}`)
              .join("；")}
          </div>
        )}
        <div>
          当前交付物：
          {inspectorDetail.deliverables.filter((d) => d.current).length === 0
            ? "无"
            : inspectorDetail.deliverables
                .filter((d) => d.current)
                .map((d) => d.current!.fileName)
                .join("、")}
        </div>
        <div style={{ display: "flex", gap: 6 }}>
          <Button size="small" type={impactMode ? "primary" : "default"} onClick={() => setImpactMode((v) => !v)}>
            {impactMode ? "退出影响路径" : "查看影响路径"}
          </Button>
          <Button
            size="small"
            onClick={() => navigate(`/projects/${projectId}/tasks?task=${selectedTask}&tab=overview`)}
          >
            打开任务详情
          </Button>
        </div>
      </div>
    </aside>
  );

  const krNodeContent = (krId: number) => {
    const k = krById.get(krId);
    if (!k) return null;
    return (
      <>
        <b>
          {k.code} {k.description}
        </b>
        <small>
          {k.progressSummary?.totalTasks ?? 0} 项任务 · {k.openBlockerCount ?? 0} 个卡点 ·{" "}
          {RISK_LABEL[k.riskLevel]}
        </small>
      </>
    );
  };

  return (
    <ProjectShell user={user} project={project} projectId={projectId} pageLabel="协作关系" onLogout={onLogout}>
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
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <AutoComplete
                style={{ width: 240 }}
                placeholder="搜索 O / KR / 任务 / 成员 / 关系"
                value={searchText}
                onChange={setSearchText}
                options={searchOptions}
                onSelect={(v) => {
                  onSearchSelect(String(v));
                  setSearchText("");
                }}
                filterOption={(input, option) =>
                  String(option?.label ?? "").toLowerCase().includes(input.toLowerCase())
                }
              />
              <Button disabled={history.length === 0} onClick={back}>
                ← 返回上一级
              </Button>
              {mode.kind !== "full" ? (
                <Button type="primary" onClick={() => enter({ kind: "full" })}>
                  全局展开
                </Button>
              ) : (
                <Button onClick={() => { setMode({ kind: "tree" }); setHistory([]); setSelectedTask(null); }}>
                  返回层级视图
                </Button>
              )}
            </div>
          </div>
          {mode.kind === "full" && (
            <div className="toolbar">
              <div className="toolbar-group">
                <Select
                  size="small"
                  style={{ width: 150 }}
                  value={oFilter}
                  onChange={setOFilter}
                  options={[
                    { value: "all" as const, label: "全部 O" },
                    ...objectives.map((o) => ({ value: o.id, label: o.title })),
                  ]}
                />
                <Select
                  size="small"
                  style={{ width: 140 }}
                  value={krFilter}
                  onChange={setKrFilter}
                  options={[
                    { value: "all" as const, label: "全部 KR" },
                    ...krList.map((k) => ({ value: k.id, label: k.code })),
                  ]}
                />
                <Select
                  size="small"
                  style={{ width: 150 }}
                  value={personFilter}
                  onChange={setPersonFilter}
                  options={[
                    { value: "all" as const, label: "全部人员" },
                    ...[...new Map(tasks.map((t) => [t.ownerId, t.ownerName])).entries()].map(
                      ([id, name]) => ({ value: id, label: name }),
                    ),
                  ]}
                />
                <span className="muted" style={{ fontSize: 12 }}>
                  显示已完成 <Switch size="small" checked={showCompleted} onChange={setShowCompleted} />
                </span>
              </div>
              <div className="toolbar-group">
                <Button size="small" onClick={() => setZoom((z) => Math.max(0.4, z - 0.15))}>
                  −
                </Button>
                <span className="muted" style={{ fontSize: 12 }}>{Math.round(zoom * 100)}%</span>
                <Button size="small" onClick={() => setZoom((z) => Math.min(1.6, z + 0.15))}>
                  ＋
                </Button>
                <Button size="small" onClick={() => setZoom(1)}>
                  适应
                </Button>
                <Button size="small" onClick={() => setDragOffsets(new Map())}>
                  重新布局
                </Button>
              </div>
            </div>
          )}
          <div className="graph-layout" style={mode.kind === "full" ? { gridTemplateColumns: "minmax(0,1fr)" } : {}}>
            {mode.kind !== "full" && (
              <aside className="risk-queue">
                <div className="risk-queue-head">风险队列</div>
                {krList.filter((k) => k.riskLevel !== "normal").length === 0 && openBlockers.length === 0 && (
                  <div className="muted" style={{ padding: 16, fontSize: 12 }}>
                    暂无需要关注的风险
                  </div>
                )}
                {krList
                  .filter((k) => k.riskLevel !== "normal")
                  .map((k) => (
                    <button key={`rk-${k.id}`} type="button" className="risk-queue-item" onClick={() => enter({ kind: "kr", krId: k.id })}>
                      <b>
                        {k.code} · {RISK_LABEL[k.riskLevel]}
                      </b>
                      <small>{k.riskNote ?? k.description}</small>
                    </button>
                  ))}
                {openBlockers.map((b) => {
                  const krId = taskById.get(b.taskId)?.keyResultId;
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
                        {taskById.get(b.taskId)?.name ?? ""}：缺 {b.missing}
                      </small>
                    </button>
                  );
                })}
              </aside>
            )}
            <div className="graph-shell" style={{ position: "relative" }}>
              {(mode.kind === "full" || mode.kind === "kr") && (edgeInspector || inspector)}
              {mode.kind === "tree" || mode.kind === "o" ? (
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
                        <small>{krList.filter((k) => k.objectiveId === n.id).length} 个 KR</small>
                      </div>
                    ) : (
                      <div
                        key={n.key}
                        className={`gnode ${n.dimmed ? "dimmed" : ""} ${
                          krById.get(n.id)?.riskLevel !== "normal" ? `risk-${krById.get(n.id)?.riskLevel}` : ""
                        }`}
                        style={{ left: n.pos.x, top: n.pos.y, width: n.pos.w, height: n.pos.h }}
                        onClick={() => enter({ kind: "kr", krId: n.id })}
                      >
                        {krNodeContent(n.id)}
                      </div>
                    ),
                  )}
                </div>
              ) : mode.kind === "kr" && krLayer ? (
                <div className="graph-canvas-inner" style={{ height: krLayer.height, minWidth: 700 }}>
                  <div className="graph-note">
                    {krLayer.kr.code} 任务关系层：硬前置加粗、关键路径最粗、互锁红色虚线、反馈紫色虚线
                  </div>
                  <svg className="graph-svg" width="700" height={krLayer.height}>
                    {krLayer.relevantEdges.map((e) => {
                      const from =
                        e.sourceTaskId != null ? krLayer.positions.get(e.sourceTaskId) : krLayer.positions.get(-e.id);
                      const to = krLayer.positions.get(e.targetTaskId);
                      if (!from || !to) return null;
                      const st = edgeStroke(e);
                      const d = edgePath(from, to);
                      const isSel = selectedEdge === e.id;
                      return (
                        <g key={e.id}>
                          <path d={d} fill="none" stroke={st.stroke} strokeWidth={isSel ? st.width + 1.5 : st.width} strokeDasharray={st.dash} />
                          <path
                            d={d}
                            fill="none"
                            stroke="transparent"
                            strokeWidth={14}
                            style={{ pointerEvents: "stroke", cursor: "pointer" }}
                            onClick={() => {
                              setSelectedTask(null);
                              setImpactMode(false);
                              setSelectedEdge((prev) => (prev === e.id ? null : e.id));
                            }}
                          />
                        </g>
                      );
                    })}
                  </svg>
                  {[...krLayer.inKr, ...krLayer.neighbors].map((t) => {
                    const pos = krLayer.positions.get(t.id);
                    return pos ? taskNode(t, pos) : null;
                  })}
                  {krLayer.memberNodes.map((m) => {
                    const pos = krLayer.positions.get(-m.edgeId);
                    return pos ? (
                      <div key={`m-${m.edgeId}`} className="gnode gnode-member" style={{ left: pos.x, top: pos.y, width: pos.w, height: pos.h }}>
                        <b>{m.label}</b>
                        <small>输入提供成员</small>
                      </div>
                    ) : null;
                  })}
                </div>
              ) : mode.kind === "full" && full ? (
                <div
                  className="graph-canvas-inner"
                  style={{
                    height: full.height * zoom,
                    width: full.width * zoom,
                    minWidth: 400,
                  }}
                >
                  <div
                    style={{
                      transform: `scale(${zoom})`,
                      transformOrigin: "top left",
                      position: "relative",
                      width: full.width,
                      height: full.height,
                    }}
                  >
                    <svg className="graph-svg" width={full.width} height={full.height}>
                      {full.visibleEdges.map((e) => {
                        const fromBase = e.sourceTaskId != null ? full.positions.get(e.sourceTaskId) : full.positions.get(-e.id);
                        const toBase = full.positions.get(e.targetTaskId);
                        const from = fromBase && e.sourceTaskId != null ? withOffset(e.sourceTaskId, fromBase) : fromBase;
                        const to = toBase ? withOffset(e.targetTaskId, toBase) : toBase;
                        if (!from || !to) return null;
                        const st = edgeStroke(e);
                        const dim =
                          (neighborIds != null &&
                            !(
                              (e.sourceTaskId != null && neighborIds.has(e.sourceTaskId) && neighborIds.has(e.targetTaskId)) ||
                              e.sourceTaskId === selectedTask ||
                              e.targetTaskId === selectedTask
                            )) ||
                          (hasFilter &&
                            !(
                              (e.sourceTaskId != null && taskMatchesFilter(taskById.get(e.sourceTaskId)!)) ||
                              taskMatchesFilter(taskById.get(e.targetTaskId)!)
                            ));
                        const isSel = selectedEdge === e.id;
                        return (
                          <g key={e.id}>
                            <path
                              d={edgePath(from, to)}
                              fill="none"
                              stroke={st.stroke}
                              strokeWidth={isSel ? st.width + 1.5 : st.width}
                              strokeDasharray={st.dash}
                              opacity={dim ? 0.15 : 1}
                            />
                            <path
                              d={edgePath(from, to)}
                              fill="none"
                              stroke="transparent"
                              strokeWidth={14}
                              style={{ pointerEvents: "stroke", cursor: "pointer" }}
                              onClick={() => {
                                setSelectedTask(null);
                                setImpactMode(false);
                                setSelectedEdge((prev) => (prev === e.id ? null : e.id));
                              }}
                            />
                          </g>
                        );
                      })}
                    </svg>
                    {full.groups.map((g) =>
                      g.isO ? (
                        <div
                          key={g.key}
                          style={{
                            position: "absolute",
                            left: g.pos.x,
                            top: g.pos.y,
                            width: g.pos.w,
                            fontWeight: 700,
                            color: "var(--navy)",
                            fontSize: 14,
                          }}
                        >
                          {g.label}
                        </div>
                      ) : (
                        <div
                          key={g.key}
                          style={{
                            position: "absolute",
                            left: g.pos.x,
                            top: g.pos.y,
                            width: g.pos.w,
                            height: g.pos.h,
                            border: "1px dashed #c3cdd8",
                            borderRadius: 10,
                            background: "rgba(255,255,255,0.35)",
                          }}
                        >
                          <div className="muted" style={{ fontSize: 12, padding: "6px 10px" }}>{g.label}</div>
                        </div>
                      ),
                    )}
                    {full.visibleTasks.map((t) => {
                      const pos = full.positions.get(t.id);
                      return pos ? taskNode(t, pos) : null;
                    })}
                    {full.memberNodes.map((m) => (
                      <div
                        key={`fm-${m.providerId}`}
                        className="gnode gnode-member"
                        style={{ left: m.pos.x, top: m.pos.y, width: m.pos.w, height: m.pos.h }}
                      >
                        <b>{m.name}</b>
                        <small>关系相关项目成员</small>
                      </div>
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </>
      )}
    </ProjectShell>
  );
}

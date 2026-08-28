import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { Alert, AutoComplete, Button, Input, Select, Spin, Switch } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";
import ProjectShell from "./ProjectShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type TaskDetail = components["schemas"]["TaskDetail"];
type DeliverableEdge = components["schemas"]["DeliverableEdge"];
type Blocker = components["schemas"]["Blocker"];
type RiskLevel = components["schemas"]["RiskLevel"];

const RISK_LABEL: Record<RiskLevel, string> = {
  normal: "正常",
  warning: "预警",
  high_risk: "高风险",
};

// 箭头按边色分档：SVG marker 不继承所属 path 的 stroke（context-stroke 支持面不稳），
// 只能一色一个 marker。取值与 edgeStroke 保持一致。
const ARROW_COLORS: Record<string, string> = {
  interlock: "#c44752",
  feedback: "#5a62c9",
  hard: "#436d84",
  plain: "#8ea3b0",
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
  const [viewMode, setViewMode] = useState<"graph" | "list">("graph");
  const [listSearch, setListSearch] = useState("");
  const [listSort, setListSort] = useState<"id" | "ready" | "type">("id");
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

  // 从项目总览的「在协作全景中查看 KRx 影响链」进入：直接落到该 KR 的任务关系层。
  const focusKrParam = searchParams.get("kr");
  useEffect(() => {
    if (!focusKrParam || loading) return;
    const krId = Number(focusKrParam);
    if (krList.some((k) => k.id === krId)) {
      setMode({ kind: "kr", krId });
      setHistory([{ kind: "tree" }]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusKrParam, loading]);

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

  const openBlockers = blockers;

  // AC-45：可见任务口径只此一份。已取消任务在图谱与列表两侧一律不出现，
  // 已完成任务由「显示已完成」开关控制；tree / kr / full / list 四处都走这里，
  // 免得再出现某一层漏带过滤的情况（U1）。
  const isTaskVisible = useCallback(
    (t: Task | undefined) =>
      !!t && t.status !== "cancelled" && (showCompleted || t.status !== "completed"),
    [showCompleted],
  );

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
    const inKr = tasks.filter((t) => t.keyResultId === mode.krId && isTaskVisible(t));
    const inKrIds = new Set(inKr.map((t) => t.id));
    const relevantEdges = edges.filter(
      (e) => (e.sourceTaskId != null && inKrIds.has(e.sourceTaskId)) || inKrIds.has(e.targetTaskId),
    );
    const neighborIds = new Set<number>();
    for (const e of relevantEdges) {
      if (e.sourceTaskId != null && !inKrIds.has(e.sourceTaskId)) neighborIds.add(e.sourceTaskId);
      if (!inKrIds.has(e.targetTaskId)) neighborIds.add(e.targetTaskId);
    }
    const neighbors = tasks.filter((t) => neighborIds.has(t.id) && isTaskVisible(t));
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
  }, [mode, krById, tasks, edges, isTaskVisible]);

  // —— 全局展开布局（AC-09）：O/KR 分组骨架 + 全部任务 + 关系相关项目成员 ——
  const full = useMemo(() => {
    if (mode.kind !== "full") return null;
    const visibleTasks = tasks.filter(isTaskVisible);
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
  }, [mode, tasks, edges, objectives, krList, isTaskVisible]);

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
      opts.push({
        value: `blocker:${b.key}`,
        label: `${b.kindLabel} · ${taskById.get(b.taskId)?.name ?? ""}：缺 ${b.missing}`,
      }),
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
        const b = blockers.find((x) => x.key === v.slice("blocker:".length));
        const t = b ? taskById.get(b.taskId) : undefined;
        if (t) {
          enter({ kind: "kr", krId: t.keyResultId });
          setSelectedTask(t.id);
        }
        break;
      }
    }
  };

  // 关系列表（AC-46）：与图谱同一份关系数据与筛选状态。
  const listRows = useMemo(() => {
    let rows = edges.filter((e) => {
      const target = taskById.get(e.targetTaskId);
      const source = e.sourceTaskId != null ? taskById.get(e.sourceTaskId) : undefined;
      if (!isTaskVisible(target) || (e.sourceTaskId != null && !isTaskVisible(source))) return false;
      if (hasFilter) {
        const match = (target ? taskMatchesFilter(target) : false) || (source ? taskMatchesFilter(source) : false);
        if (!match) return false;
      }
      const q = listSearch.trim().toLowerCase();
      if (q) {
        const hay = `${e.name}${e.sourceTaskName ?? ""}${e.targetTaskName ?? ""}${e.inputRequest?.providerName ?? ""}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
    rows = [...rows];
    if (listSort === "ready") {
      rows.sort((a, b) => Number(a.ready) - Number(b.ready));
    } else if (listSort === "type") {
      rows.sort((a, b) => a.edgeType.localeCompare(b.edgeType));
    } else {
      rows.sort((a, b) => a.id - b.id);
    }
    return rows;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [edges, taskById, isTaskVisible, oFilter, krFilter, personFilter, listSearch, listSort]);

  // AC-07：边是有向的。锚点按源目相对位置选，方向靠 marker-end 的箭头表达；
  // 反向边走相反的曲率（bow），避免 A→B 与 B→A 落在同一条曲线上无法区分。
  // 同列（KR 内部任务同排一列）不能从右缘反折回左缘横穿整列，改从两端右缘向外绕（U2）。
  const edgePath = (from: NodePos, to: NodePos) => {
    const y1 = from.y + from.h / 2;
    const y2 = to.y + to.h / 2;
    const bow = 16;
    if (to.x >= from.x + from.w) {
      const x1 = from.x + from.w;
      const x2 = to.x;
      const mx = (x1 + x2) / 2;
      return `M ${x1} ${y1} C ${mx} ${y1 - bow}, ${mx} ${y2 - bow}, ${x2} ${y2}`;
    }
    if (from.x >= to.x + to.w) {
      const x1 = from.x;
      const x2 = to.x + to.w;
      const mx = (x1 + x2) / 2;
      return `M ${x1} ${y1} C ${mx} ${y1 + bow}, ${mx} ${y2 + bow}, ${x2} ${y2}`;
    }
    const x1 = from.x + from.w;
    const x2 = to.x + to.w;
    // 外绕幅度封顶：KR 层画布固定 700 宽，绕太远会被裁掉。
    const detour = Math.max(x1, x2) + 36 + Math.min(Math.abs(y2 - y1) / 3, 40);
    return `M ${x1} ${y1} C ${detour} ${y1}, ${detour} ${y2}, ${x2} ${y2}`;
  };

  const arrowKind = (e: DeliverableEdge) =>
    e.interlockRisk ? "interlock" : e.edgeType === "feedback" ? "feedback" : e.edgeType === "hard_prerequisite" ? "hard" : "plain";
  // 原型 collaboration-prototype.js:316 的箭头形状与尺寸。
  const arrowDefs = (
    <defs>
      {Object.entries(ARROW_COLORS).map(([kind, color]) => (
        <marker
          key={kind}
          id={`cp-arrow-${kind}`}
          viewBox="0 0 10 10"
          refX="9"
          refY="5"
          markerWidth="6"
          markerHeight="6"
          orient="auto-start-reverse"
        >
          <path d="M0 0 10 5 0 10Z" fill={color} />
        </marker>
      ))}
    </defs>
  );

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
          {krById.get(t.keyResultId)?.code} · {t.ownerName} · {t.statusLabel}
          {taskBlockers.length > 0 && ` · ${taskBlockers.length} 个卡点`}
        </small>
      </div>
    );
  };

  const selectedEdgeObj = selectedEdge != null ? edges.find((e) => e.id === selectedEdge) : null;
  const edgeInspector = selectedEdgeObj && (
    <aside className="graph-inspector">
      <div className="graph-inspector-head">
        <h2>交付物边 · {selectedEdgeObj.name}</h2>
        <button type="button" aria-label="关闭详情" onClick={() => setSelectedEdge(null)}>
          ✕
        </button>
      </div>
      <div className="graph-inspector-body">
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
        {/* 互锁解释与关键路径降级提示（PRD §4.4、AC-10）：等级本身不说明问题出在哪，
            这里把「为什么算互锁」和「为什么没有关键路径」讲清，否则用户只看到一条红虚线。 */}
        {selectedEdgeObj.interlockRisk && (
          <div style={{ color: "var(--red)" }}>
            硬前置循环：互锁风险
            <div className="muted" style={{ fontSize: 12 }}>
              两端任务互相把对方的交付当作硬前置，谁都无法先开始；循环内的边暂停参与关键路径计算，
              需由环内各任务所属 KR 负责人协商拆环。
            </div>
          </div>
        )}
        {selectedEdgeObj.edgeType === "hard_prerequisite" &&
          !selectedEdgeObj.interlockRisk &&
          selectedEdgeObj.onCriticalPath == null && (
            <div className="muted">
              关键路径未计算：相关任务缺少完整的开始／截止时间，系统只确认硬依赖链，不宣称关键路径。
            </div>
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
        <div className="muted">关系详情为只读；关系维护从任务详情的「配置输入」进入。</div>
      </div>
    </aside>
  );

  const inspector = selectedTask != null && inspectorDetail && (
    <aside className="graph-inspector">
      <div className="graph-inspector-head">
        <h2>{inspectorDetail.task.name}</h2>
        <button
          type="button"
          aria-label="关闭详情"
          onClick={() => {
            setSelectedTask(null);
            setImpactMode(false);
          }}
        >
          ✕
        </button>
      </div>
      <div className="graph-inspector-body">
        <div className="muted">
          所属：{inspectorDetail.objectiveTitle} / {inspectorDetail.krDescription}
        </div>
        <div>负责人：{inspectorDetail.task.ownerName}</div>
        <div>
          状态：<span className="status-pill">{inspectorDetail.task.statusLabel}</span>
          {inspectorDetail.task.progress != null && ` · ${inspectorDetail.task.progress}%`}
        </div>
        <div>
          输入 {inspectorDetail.inputs.length} 条 / 输出 {inspectorDetail.outputs.length} 条
        </div>
        {inspectorDetail.inputs.map((e) => (
          <div key={e.id} className="muted">
            ← {e.sourceTaskName ?? e.inputRequest?.providerName} · {e.name} ·{" "}
            {e.ready ? "已就绪" : "未就绪"}
          </div>
        ))}
        {inspectorDetail.outputs.map((e) => (
          <div key={e.id} className="muted">
            → {e.targetTaskName} · {e.name}
          </div>
        ))}
        {inspectorDetail.blockers.length > 0 && (
          <div className="muted" style={{ color: "var(--red)" }}>
            卡点：{inspectorDetail.blockers.map((b) => `${b.kindLabel}：缺 ${b.missing}`).join("；")}
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

  // CR-21 KR 节点三态：高风险归红态，预警归橙态，其余灰态。
  // riskLevel 本身已由后端读时派生（卡点等级、超期、临期取最大值），前端不再叠加卡点数
  // 重算——否则预警级卡点会被画成红色描边，与文字标签自相矛盾。
  const krVisualState = (krId: number): "normal" | "warning" | "high_risk" =>
    krById.get(krId)?.riskLevel ?? "normal";

  // AC-08 新口径：KR 节点只显示编号与名称、风险状态和非零卡点数；
  // CR-21：预警／高风险再叠一个「!」标记，不只靠描边颜色区分。
  const krNodeContent = (krId: number) => {
    const k = krById.get(krId);
    if (!k) return null;
    const blockers = k.openBlockerCount ?? 0;
    const visual = krVisualState(krId);
    return (
      <>
        {visual !== "normal" && (
          <span className={`gnode-risk-marker risk-${visual}`} aria-hidden>
            !
          </span>
        )}
        <b>
          {k.code} {k.description}
        </b>
        <small>
          {RISK_LABEL[k.riskLevel]}
          {blockers > 0 ? ` · ${blockers} 个卡点` : ""}
        </small>
      </>
    );
  };

  return (
    <ProjectShell user={user} project={project} projectId={projectId} pageLabel="协作关系" pageWidth="wide" onLogout={onLogout}>
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
          {/* CR-22 顶部控制区单行：左侧搜索与筛选，右侧「图谱／列表」切换。
              返回上一级、缩放、适应与重新布局属画布操作，放在画布内（模块 PRD §5.1）。 */}
          <div className="toolbar">
            <div className="toolbar-group">
              <AutoComplete
                style={{ width: 240 }}
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
              >
                <Input prefix={<Icon name="search" size={15} />} placeholder="搜索 O / KR / 任务 / 成员 / 关系" />
              </AutoComplete>
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
            {/* 基线 §10：控制栏右侧「图谱／列表」是 segment（h36 灰底），不是实心蓝底按钮。 */}
            <div className="segment" role="group" aria-label="视图切换">
              <button type="button" aria-pressed={viewMode === "graph"} onClick={() => setViewMode("graph")}>
                图谱
              </button>
              <button type="button" aria-pressed={viewMode === "list"} onClick={() => setViewMode("list")}>
                列表
              </button>
            </div>
          </div>
          {viewMode === "list" ? (
            <>
              <div className="toolbar" style={{ marginTop: -4 }}>
                <div className="toolbar-group">
                  <AutoComplete
                    style={{ width: 240 }}
                    value={listSearch}
                    onChange={setListSearch}
                    options={[]}
                  >
                    <Input prefix={<Icon name="search" size={15} />} placeholder="搜索关系、任务或成员" />
                  </AutoComplete>
                  <Select
                    size="small"
                    style={{ width: 140 }}
                    value={listSort}
                    onChange={setListSort}
                    options={[
                      { value: "id", label: "按创建顺序" },
                      { value: "ready", label: "按就绪状态" },
                      { value: "type", label: "按关系类型" },
                    ]}
                  />
                </div>
                <span className="muted" style={{ fontSize: 12 }}>
                  只读呈现：不提供新增、修改、解除或批量维护；业务处理从任务相关页面进入
                </span>
              </div>
              <div className="data-table-wrap">
                <table className="data-table">
                  <thead>
                    <tr>
                      <th>来源任务／成员</th>
                      <th>交付物边</th>
                      <th style={{ width: 110 }}>类型</th>
                      <th style={{ width: 70 }}>必要性</th>
                      <th style={{ width: 150 }}>当前交付物</th>
                      <th>目标任务</th>
                      <th style={{ width: 110 }}>提供方</th>
                      <th style={{ width: 80 }}>就绪</th>
                      <th style={{ width: 90 }}>期望时间</th>
                      <th style={{ width: 150 }} />
                    </tr>
                  </thead>
                  <tbody>
                    {listRows.length === 0 && (
                      <tr>
                        <td colSpan={10}>
                          <div className="empty">没有匹配的协作关系</div>
                        </td>
                      </tr>
                    )}
                    {listRows.map((e) => (
                      <tr key={e.id}>
                        <td>{e.sourceTaskName ?? e.inputRequest?.providerName ?? "—"}</td>
                        <td>{e.name}</td>
                        <td>{EDGE_TYPE_LABEL[e.edgeType]}</td>
                        <td>{e.necessity === "required" ? "必要" : "参考"}</td>
                        <td>
                          {e.currentFileName ?? <span className="muted">暂无</span>}
                          {e.hasCandidate && (
                            <div className="muted" style={{ fontSize: 12 }}>
                              候选审核中
                            </div>
                          )}
                        </td>
                        <td>{e.targetTaskName}</td>
                        <td>{e.sourceOwnerName ?? e.inputRequest?.providerName ?? "—"}</td>
                        <td>
                          <span className={`status-pill ${e.ready ? "completed" : "warning"}`}>
                            {e.ready ? "已就绪" : "未就绪"}
                          </span>
                        </td>
                        <td>{e.expectedDate ?? "—"}</td>
                        <td>
                          <Button
                            type="link"
                            size="small"
                            onClick={() =>
                              navigate(`/projects/${projectId}/tasks?task=${e.targetTaskId}&tab=overview`)
                            }
                          >
                            跳转任务
                          </Button>
                          <Button
                            type="link"
                            size="small"
                            onClick={() => {
                              setViewMode("graph");
                              onSearchSelect(`edge:${e.id}`);
                            }}
                          >
                            图谱聚焦
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          ) : (
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
                      key={`rb-${b.key}`}
                      type="button"
                      className="risk-queue-item"
                      onClick={() => {
                        if (krId) {
                          enter({ kind: "kr", krId });
                          setSelectedTask(b.taskId);
                        }
                      }}
                    >
                      <b>
                        {b.kindLabel} · {RISK_LABEL[b.level]}
                      </b>
                      <small>
                        {taskById.get(b.taskId)?.name ?? ""}：缺 {b.missing}
                      </small>
                    </button>
                  );
                })}
              </aside>
            )}
            <div className="graph-shell">
              {(mode.kind === "full" || mode.kind === "kr") && (edgeInspector || inspector)}
              <div
                className={`graph-canvas-ops${
                  (mode.kind === "full" || mode.kind === "kr") && (edgeInspector || inspector)
                    ? " with-inspector"
                    : ""
                }`}
              >
                <Button size="small" disabled={history.length === 0} onClick={back}>
                  ← 返回上一级
                </Button>
                {/* 缩放、适应与重新布局只作用于全局展开画布，其余层级不出现。 */}
                {mode.kind === "full" && (
                  <>
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
                  </>
                )}
              </div>
              <div className="graph-scroll">
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
                        className={`gnode gnode-kr ${n.dimmed ? "dimmed" : ""} ${
                          krVisualState(n.id) !== "normal" ? `risk-${krVisualState(n.id)}` : ""
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
                    {arrowDefs}
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
                          <path
                            d={d}
                            fill="none"
                            stroke={st.stroke}
                            strokeWidth={isSel ? st.width + 1.5 : st.width}
                            strokeDasharray={st.dash}
                            markerEnd={`url(#cp-arrow-${arrowKind(e)})`}
                          />
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
                      {arrowDefs}
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
                              markerEnd={`url(#cp-arrow-${arrowKind(e)})`}
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
          </div>
          )}
        </>
      )}
    </ProjectShell>
  );
}

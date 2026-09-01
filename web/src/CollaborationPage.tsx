import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { Alert, AutoComplete, Button, Input, Select, Spin, Switch } from "antd";
import { client } from "./api/client";
import { formatFileSize } from "./FileUploadField";
import type { components } from "./api/schema";
import Icon from "./icons";
import ProjectShell from "./ProjectShell";
import TaskDrawerHost from "./task-drawer";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type TaskDetail = components["schemas"]["TaskDetail"];
type DeliverableEdge = components["schemas"]["DeliverableEdge"];
type Blocker = components["schemas"]["Blocker"];


// 箭头分档（#147 收敛为两档）：默认灰（原型 #cp-arrow #73839a），互锁随边色红；
// SVG marker 不继承所属 path 的 stroke（context-stroke 支持面不稳），只能一色一个 marker。
const ARROW_COLORS: Record<string, string> = {
  interlock: "#bd3e49",
  plain: "#73839a",
};

// 全局展开的分批渲染阈值（协作关系 PRD §12）：首批 200 个节点渲染完即可交互，
// 其余按帧增量补齐。
const GRAPH_FIRST_BATCH = 200;
const GRAPH_BATCH_STEP = 100;

// 缩放范围与步进取原型（collaboration-prototype.js:506、530）：滚轮 0.08、按钮 0.15。
const ZOOM_MIN = 0.45;
const ZOOM_MAX = 2.2;

// #146：任务／成员节点是实线圆环（PRD §11；原型 nodeMarkup r=29、聚焦起点 r=35）。
const TASK_R = 29;
const TASK_R_FOCUS = 35;
const MEMBER_R = 29;

// CR-19（#145）：节点拖拽在当前浏览会话内固定——模块级缓存在组件卸载后存活，
// 刷新页面即清空、其他用户不继承；key 为「项目:视图:节点」，各层级坐标系互不串。
const sessionDragOffsets = new Map<string, { dx: number; dy: number }>();


// 层级固定为：层级树 → O → KR 任务关系层 → 任务聚焦层 → 全局展开（AC-27、CR-05／CR-06）。
// focus 是「逐层展开」层：从一个任务出发，把展开过的节点各自的 1-hop 邻居并进画布，
// 节点集合随点击增长，而不是像 tree/o/kr/full 那样整层替换。
type Mode =
  | { kind: "tree" }
  | { kind: "o"; objectiveId: number }
  | { kind: "kr"; krId: number }
  | { kind: "focus"; taskId: number }
  | { kind: "full" };

type NodePos = { x: number; y: number; w: number; h: number };

// 关系列表「当前交付物」列（裁决 J1，#142）：显示「类型 · 大小」，类型文案由服务端派生；
// 边未绑定具体交付物项且来源任务有多项当前内容时显示「N 项」，悬停列出各项「文件名 · 大小」。
function edgeCurrentFiles(e: DeliverableEdge): { fileName: string; fileTypeLabel: string; fileSize: number }[] {
  if (e.currentFileName) {
    return [
      {
        fileName: e.currentFileName,
        fileTypeLabel: e.currentFileTypeLabel ?? "文件",
        fileSize: e.currentFileSize ?? 0,
      },
    ];
  }
  return e.sourceCurrentFiles ?? [];
}

function edgeCurrentCell(e: DeliverableEdge): string | null {
  const files = edgeCurrentFiles(e);
  if (files.length === 0) return null;
  if (files.length > 1) return `${files.length} 项`;
  const f = files[0];
  return f.fileSize > 0 ? `${f.fileTypeLabel} · ${formatFileSize(f.fileSize)}` : f.fileTypeLabel;
}

function edgeCurrentTitle(e: DeliverableEdge): string {
  const files = edgeCurrentFiles(e);
  if (files.length === 0) return "暂无";
  return files
    .map((f) => (f.fileSize > 0 ? `${f.fileName} · ${formatFileSize(f.fileSize)}` : f.fileName))
    .join("\n");
}

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

  const [project, setProject] = useState<Project | null>(null);
  const [objectives, setObjectives] = useState<Objective[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [edges, setEdges] = useState<DeliverableEdge[]>([]);
  const [blockers, setBlockers] = useState<Blocker[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [mode, setMode] = useState<Mode>({ kind: "tree" });
  // 已展开的任务集合：聚焦层里每点一个节点就把它并进来，画布节点集合随之增长（CR-05）。
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  // 历史栈只用来恢复筛选与视口；层级回退按固定层级推导，不依赖进入路径（CR-06）。
  const [viewStack, setViewStack] = useState<
    {
      oFilter: number | "all";
      krFilter: number | "all";
      personFilter: number | "all";
      zoom: number;
      pan: { x: number; y: number };
    }[]
  >([]);
  const [selectedTask, setSelectedTask] = useState<number | null>(null);
  const [inspectorDetail, setInspectorDetail] = useState<TaskDetail | null>(null);
  const [zoom, setZoom] = useState(1);
  // #145：画布不再滚动，改为平移＋缩放的视口（PRD §6.4）。
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const [panning, setPanning] = useState(false);
  const [viewportSize, setViewportSize] = useState({ w: 0, h: 0 });
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const panRef = useRef({ active: false, moved: false, startX: 0, startY: 0, origin: { x: 0, y: 0 } });
  // 节点拖拽超过阈值后吞掉随后的 click，避免拖完还触发选中/展开（原型 suppressClick）。
  const suppressClickRef = useRef(false);
  const [oFilter, setOFilter] = useState<number | "all">("all");
  const [krFilter, setKrFilter] = useState<number | "all">("all");
  const [personFilter, setPersonFilter] = useState<number | "all">("all");
  const [showCompleted, setShowCompleted] = useState(false);
  const [impactMode, setImpactMode] = useState(false);
  const [selectedEdge, setSelectedEdge] = useState<number | null>(null);
  // 会话内节点固定（CR-19）：初值取模块级缓存，写入时双写，组件重挂载不丢。
  const [dragOffsets, setDragOffsets] = useState<Map<string, { dx: number; dy: number }>>(
    () => new Map(sessionDragOffsets),
  );
  const [searchText, setSearchText] = useState("");
  const [viewMode, setViewMode] = useState<"graph" | "list">("graph");
  // #121：任务详情在本页抽屉打开，关闭后图谱层级与筛选不丢。
  const [drawerTaskId, setDrawerTaskId] = useState<number | null>(null);
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

  // 全局展开分批渲染（CR §12）：首批不超过 200 个节点，之后按帧增量补齐。
  // 布局本身是同步算好的，这里限的是「一次往 DOM 里塞多少节点」——
  // 首批渲染完就能缩放、拖动与返回，不出现整页遮罩。
  const [renderBudget, setRenderBudget] = useState(GRAPH_FIRST_BATCH);
  useEffect(() => {
    if (mode.kind !== "full") {
      setRenderBudget(GRAPH_FIRST_BATCH);
      return;
    }
    const total = tasks.length;
    if (renderBudget >= total) return;
    const id = window.setTimeout(
      () => setRenderBudget((n) => Math.min(total, n + GRAPH_BATCH_STEP)),
      0,
    );
    return () => window.clearTimeout(id);
  }, [mode.kind, renderBudget, tasks.length]);

  // 从任务详情进入：聚焦当前任务并展示一层直接关系（AC-42）。
  const focusTaskParam = searchParams.get("task");
  useEffect(() => {
    if (!focusTaskParam || loading) return;
    const t = tasks.find((x) => x.id === Number(focusTaskParam));
    if (t) {
      setMode({ kind: "kr", krId: t.keyResultId });
      setViewStack([]);
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
      setViewStack([]);
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

  // 编号是持久字段（AC-64），直接用 KR 自带的 code。
  const krList = useMemo(
    () => objectives.flatMap((o) => o.keyResults.map((k) => ({ ...k, objectiveId: o.id }))),
    [objectives],
  );
  const krById = useMemo(() => new Map(krList.map((k) => [k.id, k])), [krList]);
  const taskById = useMemo(() => new Map(tasks.map((t) => [t.id, t])), [tasks]);
  const edgeById = useMemo(() => new Map(edges.map((e) => [e.id, e])), [edges]);

  // 拖拽固定的缓存 key：不同层级布局坐标系不同，偏移只在本视图内生效（CR-19）。
  const modeKey =
    mode.kind === "kr" ? `kr:${mode.krId}` : mode.kind === "focus" ? `focus:${mode.taskId}` : mode.kind;
  const offsetKey = (id: number) => `${projectId}:${modeKey}:${id}`;

  const resetViewport = () => {
    setZoom(1);
    setPan({ x: 0, y: 0 });
  };

  const enter = (next: Mode) => {
    setViewStack((v) => [...v, { oFilter, krFilter, personFilter, zoom, pan }]);
    setMode(next);
    setSelectedTask(null);
    setExpanded(new Set());
    resetViewport();
  };

  // 进入任务聚焦层：以该任务为起点，先展开它自己的一层邻居（AC-27）。
  const enterFocus = (taskId: number) => {
    setViewStack((v) => [...v, { oFilter, krFilter, personFilter, zoom, pan }]);
    setMode({ kind: "focus", taskId });
    setExpanded(new Set([taskId]));
    setSelectedTask(taskId);
    setSelectedEdge(null);
    setImpactMode(false);
    resetViewport();
  };

  // 返回按固定层级回退（CR-06）：聚焦 → 所属 KR 层 → 所属 O 层 → 层级树；
  // 全局展开直接回层级树。从层级树直点 KR 再返回，同样会经过 O 层焦点，
  // 不会因为「进入路径没走过 O」而跳过——这正是历史栈做不到的。
  const back = () => {
    const restore = viewStack[viewStack.length - 1];
    if (restore) {
      setOFilter(restore.oFilter);
      setKrFilter(restore.krFilter);
      setPersonFilter(restore.personFilter);
      setZoom(restore.zoom);
      setPan(restore.pan);
      setViewStack((v) => v.slice(0, -1));
    } else {
      resetViewport();
    }
    setSelectedEdge(null);
    setImpactMode(false);
    setExpanded(new Set());
    if (mode.kind === "focus") {
      const t = taskById.get(mode.taskId);
      setMode(t ? { kind: "kr", krId: t.keyResultId } : { kind: "tree" });
      setSelectedTask(t ? mode.taskId : null);
      return;
    }
    if (mode.kind === "kr") {
      const kr = krById.get(mode.krId);
      setMode(kr ? { kind: "o", objectiveId: kr.objectiveId } : { kind: "tree" });
      setSelectedTask(null);
      return;
    }
    setMode({ kind: "tree" });
    setSelectedTask(null);
  };

  const openBlockers = blockers;

  // AC-45：可见任务口径只此一份。已关闭任务在图谱与列表两侧一律不出现，
  // 已完成任务由「显示已完成」开关控制；tree / kr / full / list 四处都走这里，
  // 免得再出现某一层漏带过滤的情况（U1）。
  const isTaskVisible = useCallback(
    (t: Task | undefined) =>
      !!t && t.status !== "cancelled" && (showCompleted || t.status !== "completed"),
    [showCompleted],
  );

  // 裁决 K＝A（#114，F-11）：层级视图按工具栏筛选淡化——O 筛选淡化非选中 O 及其 KR，
  // KR 筛选淡化非选中 KR，人员筛选按该 KR 下有无该人员的任务淡化；口径与另两层一致（AC-45）。
  const filtering = oFilter !== "all" || krFilter !== "all" || personFilter !== "all";
  const krPass = useCallback(
    (k: (typeof krList)[number]) =>
      (oFilter === "all" || k.objectiveId === oFilter) &&
      (krFilter === "all" || k.id === krFilter) &&
      (personFilter === "all" ||
        tasks.some((t) => t.keyResultId === k.id && t.ownerId === personFilter)),
    [oFilter, krFilter, personFilter, tasks],
  );

  // —— O／KR 层级树布局（#148 对齐原型 aggregateModel）：横向——O 排上方，
  // 下属 KR 在其下方 2 列网格（单 KR 1 列），O→KR 走 V-H-V 正交线。 ——
  const tree = useMemo(() => {
    const nodes: { key: string; kind: "o" | "kr"; id: number; pos: NodePos; dimmed: boolean }[] = [];
    const lines: { d: string; x1: number; y1: number; x2: number; y2: number; dimmed: boolean }[] = [];
    const oW = 220;
    const oH = 58;
    const krW = 280;
    const krH = 62;
    const gapX = 16;
    const rowGap = 26;
    const colGap = 44;
    let x = 24;
    let maxY = 160;
    for (const o of objectives) {
      const krs = krList.filter((k) => k.objectiveId === o.id);
      const columns = krs.length <= 1 ? 1 : 2;
      const colW = columns * krW + (columns - 1) * gapX;
      const oPos = { x: x + colW / 2 - oW / 2, y: 20, w: oW, h: oH };
      const oPass =
        !filtering ||
        ((oFilter === "all" || o.id === oFilter) &&
          (krFilter === "all" && personFilter === "all" ? true : krs.some(krPass)));
      const oDimmed = !oPass;
      nodes.push({ key: `o-${o.id}`, kind: "o", id: o.id, pos: oPos, dimmed: oDimmed });
      const ocx = oPos.x + oW / 2;
      const oby = oPos.y + oH;
      krs.forEach((k, i) => {
        const kx = x + (i % columns) * (krW + gapX);
        const ky = 160 + Math.floor(i / columns) * (krH + rowGap);
        const krDimmed = filtering && !krPass(k);
        nodes.push({ key: `kr-${k.id}`, kind: "kr", id: k.id, pos: { x: kx, y: ky, w: krW, h: krH }, dimmed: krDimmed });
        const kcx = kx + krW / 2;
        // V-H-V 正交线（原型 edgePath 的 owns objective→kr 分支，46% 处转水平）。
        const branchY = oby + (ky - oby) * 0.46;
        lines.push({
          d: `M ${ocx} ${oby} V ${branchY} H ${kcx} V ${ky}`,
          x1: ocx,
          y1: oby,
          x2: kcx,
          y2: ky,
          dimmed: oDimmed || krDimmed,
        });
        maxY = Math.max(maxY, ky + krH);
      });
      x += colW + colGap;
    }
    return { nodes, lines, width: Math.max(700, x - colGap + 24), height: maxY + 40 };
  }, [objectives, krList, filtering, krPass, oFilter, krFilter, personFilter]);

  // —— O 聚焦层（#148 对齐原型 focusModel O 分支；CR-03）：只显该 O 与下属 KR 横排一行。 ——
  const oLayer = useMemo(() => {
    if (mode.kind !== "o") return null;
    const o = objectives.find((it) => it.id === mode.objectiveId);
    if (!o) return null;
    const krs = krList.filter((k) => k.objectiveId === o.id);
    const oW = 220;
    const oH = 58;
    const krW = 280;
    const krH = 62;
    const gapX = 24;
    const rowW = Math.max(krs.length * krW + Math.max(krs.length - 1, 0) * gapX, oW);
    const width = Math.max(700, rowW + 96);
    const oPos = { x: width / 2 - oW / 2, y: 36, w: oW, h: oH };
    const krY = 250;
    const startX = (width - rowW) / 2;
    const ocx = oPos.x + oW / 2;
    const oby = oPos.y + oH;
    const krNodes = krs.map((k, i) => {
      const kx = startX + i * (krW + gapX);
      return { id: k.id, pos: { x: kx, y: krY, w: krW, h: krH }, dimmed: filtering && !krPass(k) };
    });
    const lines = krNodes.map((n) => {
      const kcx = n.pos.x + krW / 2;
      const branchY = oby + (krY - oby) * 0.46;
      return { d: `M ${ocx} ${oby} V ${branchY} H ${kcx} V ${krY}`, x1: ocx, y1: oby, x2: kcx, y2: krY, dimmed: n.dimmed };
    });
    return { o, oPos, krNodes, lines, width, height: krY + krH + 60 };
  }, [mode, objectives, krList, filtering, krPass]);

  // —— KR 任务关系层布局（#148 对齐原型 focusModel KR 分支）：放射状——KR 节点居中，
  // 本 KR 任务按角度环绕（环半径按数量自适应），外部相邻任务分列画布左右两缘，
  // 成员来源输入接在左缘竖排（CR-13 保留成员节点）。 ——
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
    // 槽位存圆心对齐的包围盒（circleBounds 换算后圆心正落 (X,Y)）。
    const slot = (X: number, Y: number, r = TASK_R): NodePos => ({ x: X - r, y: Y - r, w: r * 2, h: r * 2 });
    const n = inKr.length;
    // 环半径按任务数自适应：周长至少容纳每任务约 90px（圆 58＋间距），>12 个自动撑大。
    const rx = Math.max(205, n * 15);
    const ry = Math.max(185, n * 13);
    const cx = Math.max(620, rx + 330);
    const cy = Math.max(330, ry + 110);
    const positions = new Map<number, NodePos>();
    inKr.forEach((t, i) => {
      const angle = -Math.PI / 2 + (i * Math.PI * 2) / Math.max(n, 4);
      positions.set(t.id, slot(cx + Math.cos(angle) * rx, cy + Math.sin(angle) * ry));
    });
    const leftX = 110;
    const rightX = cx + (cx - leftX);
    neighbors.forEach((t, i) => {
      positions.set(t.id, slot(i % 2 ? rightX : leftX, 130 + Math.floor(i / 2) * 145));
    });
    const leftRows = Math.ceil(neighbors.length / 2);
    const rightRows = Math.floor(neighbors.length / 2);
    const memberNodes: { edgeId: number; label: string; inputName: string }[] = [];
    relevantEdges.forEach((e) => {
      if (e.sourceTaskId == null && e.inputRequest) {
        positions.set(-e.id, slot(leftX, 130 + (leftRows + memberNodes.length) * 145));
        memberNodes.push({ edgeId: e.id, label: e.inputRequest.providerName, inputName: e.name });
      }
    });
    // 中心 KR 节点（CR-21 白底三态复用；已在本层，点它不再下钻）。
    const krPos = { x: cx - 150, y: cy - 31, w: 300, h: 62 };
    // 「显示已完成」关闭时被藏起来的本 KR 任务数：全完成的 KR 点进来是空画布，
    // 空态要能指出「打开开关就看得到」（Q-10、AC-45）。
    const hiddenCompleted = tasks.filter(
      (t) => t.keyResultId === mode.krId && t.status === "completed" && !isTaskVisible(t),
    ).length;
    const height = Math.max(
      cy + ry + 150,
      130 + (leftRows + memberNodes.length) * 145 + 80,
      130 + rightRows * 145 + 80,
    );
    const width = rightX + 130;
    return { kr, krPos, cx, cy, inKr, neighbors, relevantEdges, positions, memberNodes, hiddenCompleted, width, height };
  }, [mode, krById, tasks, edges, isTaskVisible]);

  // —— 任务聚焦层布局（AC-27、CR-05）：逐层展开 ——
  // 可见节点＝每个已展开任务与它的 1-hop 邻居的并集；再按到起点的跳数分列，
  // 起点在最左，越往右离起点越远。点一个还没展开的节点就把它并进 expanded，
  // 节点集合随之增长，不像 kr/full 那样整层替换。
  const focusLayer = useMemo(() => {
    if (mode.kind !== "focus") return null;
    const origin = taskById.get(mode.taskId);
    if (!origin) return null;
    const neighborsOf = (id: number) => {
      const out: number[] = [];
      for (const e of edges) {
        if (e.sourceTaskId === id) out.push(e.targetTaskId);
        if (e.targetTaskId === id && e.sourceTaskId != null) out.push(e.sourceTaskId);
      }
      return out;
    };
    const visible = new Set<number>();
    for (const id of expanded) {
      if (isTaskVisible(taskById.get(id))) visible.add(id);
      for (const n of neighborsOf(id)) {
        if (isTaskVisible(taskById.get(n))) visible.add(n);
      }
    }
    if (visible.size === 0) visible.add(origin.id);
    // 跳数分层：只在可见集合内做 BFS，避免把没展开的路径也算进层号。
    const hop = new Map<number, number>([[origin.id, 0]]);
    const queue = [origin.id];
    while (queue.length > 0) {
      const cur = queue.shift()!;
      for (const n of neighborsOf(cur)) {
        if (!visible.has(n) || hop.has(n)) continue;
        hop.set(n, (hop.get(cur) ?? 0) + 1);
        queue.push(n);
      }
    }
    const byHop = new Map<number, number[]>();
    for (const id of visible) {
      const h = hop.get(id) ?? 0;
      byHop.set(h, [...(byHop.get(h) ?? []), id]);
    }
    // #148 中心放射（原型 focusModel 任务分支）：起点居中放大（r=35），第 h 跳绕第 h 环
    // 分布，环半径按该环节点数自适应；同环内按父节点方位排序并从父方位起步布角，
    // 新展开的节点因此落在其父节点外侧附近、已有节点不重排语义（CR-05）不变。
    const rings = [...byHop.keys()].filter((h) => h > 0).sort((a, b) => a - b);
    const angleOf = new Map<number, number>([[origin.id, -Math.PI / 2]]);
    const ringR: { rx: number; ry: number }[] = [];
    rings.forEach((h, ri) => {
      const count = (byHop.get(h) ?? []).length;
      ringR.push({
        rx: Math.max(250 + ri * 170, count * 15),
        ry: Math.max(220 + ri * 150, count * 13),
      });
    });
    const maxRx = ringR.reduce((m, r) => Math.max(m, r.rx), 0);
    const maxRy = ringR.reduce((m, r) => Math.max(m, r.ry), 0);
    const cx = Math.max(620, maxRx + 250);
    const cy = Math.max(350, maxRy + 110);
    const slot = (X: number, Y: number, r = TASK_R): NodePos => ({ x: X - r, y: Y - r, w: r * 2, h: r * 2 });
    const positions = new Map<number, NodePos>();
    positions.set(origin.id, slot(cx, cy, TASK_R_FOCUS));
    rings.forEach((h, ri) => {
      const parentAngle = (id: number) => {
        for (const nb of neighborsOf(id)) {
          if (hop.get(nb) === h - 1 && angleOf.has(nb)) return angleOf.get(nb)!;
        }
        return -Math.PI / 2;
      };
      const ids = (byHop.get(h) ?? []).sort((a, b) => parentAngle(a) - parentAngle(b) || a - b);
      const start = ids.length > 0 ? parentAngle(ids[0]) : -Math.PI / 2;
      const { rx, ry } = ringR[ri];
      ids.forEach((id, i) => {
        const angle = start + (i * Math.PI * 2) / Math.max(ids.length, 4);
        angleOf.set(id, angle);
        positions.set(id, slot(cx + Math.cos(angle) * rx, cy + Math.sin(angle) * ry));
      });
    });
    // 成员来源的输入：左侧竖排（原型 person 落位），作为可点节点参与聚焦（CR-13）。
    const memberNodes: { edgeId: number; label: string; inputName: string }[] = [];
    const relevantEdges = edges.filter((e) => {
      const targetIn = visible.has(e.targetTaskId);
      if (e.sourceTaskId == null) return targetIn && !!e.inputRequest;
      return visible.has(e.sourceTaskId) && targetIn;
    });
    relevantEdges.forEach((e) => {
      if (e.sourceTaskId == null && e.inputRequest) {
        positions.set(-e.id, slot(120, 200 + memberNodes.length * 130));
        memberNodes.push({ edgeId: e.id, label: e.inputRequest.providerName, inputName: e.name });
      }
    });
    const width = cx + maxRx + 160;
    const height = Math.max(cy + maxRy + 140, 200 + memberNodes.length * 130 + 60);
    const visibleTasks = [...visible]
      .map((id) => taskById.get(id))
      .filter((t): t is Task => !!t);
    return { origin, visibleTasks, relevantEdges, positions, memberNodes, height, width };
  }, [mode, expanded, edges, taskById, isTaskVisible]);

  // —— 全局展开布局（AC-09）：O/KR 分组骨架 + 全部任务 + 关系相关项目成员 ——
  // 全局展开（#123）：按原型 fullModel 重做为 O→KR→任务链路图——O、KR 是真实节点，
  // owns 层级连线（O→KR 正交、KR→任务直线）绘于关系边之下；每个 O 一列，列内 KR 纵排、
  // 任务 2 列网格；成员节点按原型放底部横排。淡化在渲染层按筛选实时算，布局不依赖筛选。
  const full = useMemo(() => {
    if (mode.kind !== "full") return null;
    const visibleTasks = tasks.filter(isTaskVisible);
    const visibleIds = new Set(visibleTasks.map((t) => t.id));
    // #146：任务槽收窄成圆环＋caption 的占位（圆 58px），列宽随之变紧凑。
    const nodeW = 150;
    const nodeH = 96;
    const gapX = 14;
    const colPad = 14;
    const colW = 2 * nodeW + gapX + colPad * 2;
    const colGap = 36;
    const oW = 220;
    const oH = 58;
    const krW = 280;
    const krH = 64;
    const positions = new Map<number, NodePos>();
    const oNodes: { id: number; title: string; krCount: number; pos: NodePos }[] = [];
    const krNodes: { id: number; pos: NodePos }[] = [];
    const ownsLines: { d: string; krId: number; taskId?: number }[] = [];
    let maxY = 0;
    objectives.forEach((o, oi) => {
      const colX = 24 + oi * (colW + colGap);
      const innerX = colX + colPad;
      const krs = krList.filter((k) => k.objectiveId === o.id);
      const oPos = { x: colX + (colW - oW) / 2, y: 16, w: oW, h: oH };
      oNodes.push({ id: o.id, title: o.title, krCount: krs.length, pos: oPos });
      const oCx = oPos.x + oW / 2;
      const spineX = colX + 5;
      let y = 16 + oH + 30;
      for (const k of krs) {
        const krTasks = visibleTasks.filter((t) => t.keyResultId === k.id);
        const krPos = { x: colX + (colW - krW) / 2, y, w: krW, h: krH };
        krNodes.push({ id: k.id, pos: krPos });
        const krCx = krPos.x + krW / 2;
        const krCy = y + krH / 2;
        // O→KR：原型的正交连线（V-H-V），沿列左侧走线不穿下方节点。
        ownsLines.push({
          d: `M ${oCx} ${oPos.y + oH} V ${oPos.y + oH + 12} H ${spineX} V ${krCy} H ${krPos.x}`,
          krId: k.id,
        });
        krTasks.forEach((t, i) => {
          const tx = innerX + (i % 2) * (nodeW + gapX);
          const ty = y + krH + 18 + Math.floor(i / 2) * (nodeH + 12);
          positions.set(t.id, { x: tx, y: ty, w: nodeW, h: nodeH });
          // KR→任务：owns 直线绘于节点层之下，端点隐入任务圆心（#146 槽位顶部是圆环）。
          ownsLines.push({
            d: `M ${krCx} ${krCy} L ${tx + nodeW / 2} ${ty + TASK_R}`,
            krId: k.id,
            taskId: t.id,
          });
        });
        const rows = Math.ceil(krTasks.length / 2);
        y += krH + (rows > 0 ? 18 + rows * (nodeH + 12) : 0) + 26;
      }
      maxY = Math.max(maxY, y);
    });
    // 关系相关项目成员节点（词汇表）：承担输入责任的成员，底部横排。
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
    const memberY = maxY + 24;
    for (const [pid, entry] of memberByProvider) {
      const pos = { x: 24 + mi * 216, y: memberY, w: 200, h: 88 };
      memberNodes.push({ providerId: pid, name: entry.name, pos, edgeIds: entry.edgeIds });
      for (const eid of entry.edgeIds) positions.set(-eid, pos);
      mi++;
    }
    const visibleEdges = edges.filter((e) => {
      const targetOK = visibleIds.has(e.targetTaskId);
      const sourceOK = e.sourceTaskId != null ? visibleIds.has(e.sourceTaskId) : positions.has(-e.id);
      return targetOK && sourceOK;
    });
    const height = (memberNodes.length > 0 ? memberY + 88 : maxY) + 40;
    const width = Math.max(24 + objectives.length * (colW + colGap), 24 + mi * 216) + 20;
    return { visibleTasks, positions, oNodes, krNodes, ownsLines, memberNodes, visibleEdges, height, width };
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
      case "member": {
        // CR-13：定位到该成员承担的第一条输入关系并选中，画布上的成员节点随之高亮。
        const memberEdge = edges.find((e) => e.inputRequest?.providerId === id);
        const target = memberEdge ? taskById.get(memberEdge.targetTaskId) : undefined;
        if (memberEdge && target) {
          enter({ kind: "kr", krId: target.keyResultId });
          setSelectedEdge(memberEdge.id);
        } else {
          enter({ kind: "full" });
        }
        break;
      }
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
      // CR §5.2：图谱与关系列表共享搜索词，切视图不丢输入。
      const q = searchText.trim().toLowerCase();
      if (q) {
        // 裁决 J1（#142）：「交付物边」列已删，搜索匹配不再含边名，保留任务名与成员名。
        const hay = `${e.sourceTaskName ?? ""}${e.targetTaskName ?? ""}${e.inputRequest?.providerName ?? ""}`.toLowerCase();
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
  }, [edges, taskById, isTaskVisible, oFilter, krFilter, personFilter, searchText, listSort]);

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

  const arrowKind = (e: DeliverableEdge) => (e.interlockRisk ? "interlock" : "plain");
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

  // #147 边默认灰色系（PRD §11）：普通／成果／硬前置灰实线、反馈灰虚线、互锁红虚线；
  // 非互锁的未就绪关系用橙色异常提示（原型 anomaly）；关键路径只加粗不上色（§6.3），
  // 普通硬前置不再默认加粗——「默认状态不使用多种蓝色、青色或线宽强调普通关系」。
  const edgeStroke = (e: DeliverableEdge) => {
    if (e.interlockRisk) return { stroke: "#bd3e49", width: 2.5, dash: "6 4" };
    const dash = e.edgeType === "feedback" ? "6 4" : undefined;
    if (!e.ready) return { stroke: "#a86917", width: 2, dash };
    return {
      stroke: "#929dad",
      width: e.onCriticalPath ? 2.4 : e.edgeType === "hard_prerequisite" ? 1.7 : 1.6,
      dash,
    };
  };

  // #145：节点拖拽对所有出现任务节点的层级生效（kr／focus／full），不再限于全局展开。
  const startDrag = (taskId: number, startX: number, startY: number) => {
    const key = offsetKey(taskId);
    const base = dragOffsets.get(key) ?? { dx: 0, dy: 0 };
    let moved = false;
    const onMove = (ev: MouseEvent) => {
      const dx = base.dx + (ev.clientX - startX) / zoom;
      const dy = base.dy + (ev.clientY - startY) / zoom;
      if (Math.hypot(dx - base.dx, dy - base.dy) > 4) moved = true;
      const off = { dx, dy };
      sessionDragOffsets.set(key, off);
      setDragOffsets((prev) => {
        const next = new Map(prev);
        next.set(key, off);
        return next;
      });
    };
    const onUp = () => {
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
      if (moved) suppressClickRef.current = true;
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  };

  const withOffset = (taskId: number, pos: NodePos): NodePos => {
    const off = dragOffsets.get(offsetKey(taskId));
    if (!off) return pos;
    return { ...pos, x: pos.x + off.dx, y: pos.y + off.dy };
  };

  // #146：布局槽位（圆环＋圆下 caption 的占位）→ 圆形包围盒。节点渲染与边锚点共用
  // 这一份换算，箭头因此落在圆边而不是槽位边缘；聚焦层起点用大圆（原型 r=35）。
  const taskRadius = (id: number) => (mode.kind === "focus" && mode.taskId === id ? TASK_R_FOCUS : TASK_R);
  const circleBounds = (pos: NodePos, r: number): NodePos => ({
    x: pos.x + pos.w / 2 - r,
    y: pos.y,
    w: r * 2,
    h: r * 2,
  });
  const taskCircle = (id: number, base: NodePos): NodePos => circleBounds(withOffset(id, base), taskRadius(id));
  const memberCircle = (base: NodePos): NodePos => circleBounds(base, MEMBER_R);

  // §11 键盘可达：节点是 role=button 的可聚焦元素，Enter／空格等价单击。
  const pressAsClick = (fn: () => void) => (ev: React.KeyboardEvent) => {
    if (ev.key === "Enter" || ev.key === " ") {
      ev.preventDefault();
      fn();
    }
  };

  // 「重新布局」清除会话内固定并复位视口（PRD §6.4；只清本项目的 key，不动其他项目）。
  const relayout = () => {
    for (const key of [...sessionDragOffsets.keys()]) {
      if (key.startsWith(`${projectId}:`)) sessionDragOffsets.delete(key);
    }
    setDragOffsets(new Map(sessionDragOffsets));
    resetViewport();
  };

  // 画布平移（§6.4）＋点击空白取消选择（§6.2）：位移 ≤4px 的按放视为单击，
  // 只清选择，不收起已展开节点。节点与边的命中目标不触发平移。
  const onViewportPointerDown = (ev: React.PointerEvent<HTMLDivElement>) => {
    const target = ev.target as Element;
    if (target.closest(".gnode") || target.closest(".edge-hit")) return;
    panRef.current = {
      active: true,
      moved: false,
      startX: ev.clientX,
      startY: ev.clientY,
      origin: { ...pan },
    };
    ev.currentTarget.setPointerCapture(ev.pointerId);
    setPanning(true);
  };
  const onViewportPointerMove = (ev: React.PointerEvent<HTMLDivElement>) => {
    const p = panRef.current;
    if (!p.active) return;
    const dx = ev.clientX - p.startX;
    const dy = ev.clientY - p.startY;
    if (Math.hypot(dx, dy) > 4) p.moved = true;
    setPan({ x: p.origin.x + dx, y: p.origin.y + dy });
  };
  const onViewportPointerUp = () => {
    const p = panRef.current;
    if (!p.active) return;
    p.active = false;
    setPanning(false);
    if (!p.moved) {
      setSelectedTask(null);
      setSelectedEdge(null);
      setImpactMode(false);
    }
  };

  // 滚轮缩放：React 的 onWheel 是 passive 监听，preventDefault 无效，走原生监听。
  useEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    const onWheel = (ev: WheelEvent) => {
      ev.preventDefault();
      setZoom((z) => Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, z + (ev.deltaY < 0 ? 0.08 : -0.08))));
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [viewMode, loading, notFound]);

  // 小地图视口框需要视口实际尺寸。
  useEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    const update = () => setViewportSize({ w: el.clientWidth, h: el.clientHeight });
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, [viewMode, loading, notFound]);

  // AC-43：选中一条关系或一个任务时，与之无关的成员节点同样淡化——
  // 否则「高亮关系两端」会被旁边照常亮着的成员节点削弱（Q-07）。
  // 成员节点挂在「指定成员输入」的边上，按它所在边与该边的接收任务判定。
  const memberDimmed = (edgeIds: number[]) => {
    const targets = edgeIds
      .map((id) => edgeById.get(id)?.targetTaskId)
      .filter((id): id is number => id != null);
    const dimByFilter =
      hasFilter &&
      !targets.some((id) => {
        const t = taskById.get(id);
        return t ? taskMatchesFilter(t) : false;
      });
    const dimBySelect =
      selectedEdge != null
        ? !edgeIds.includes(selectedEdge)
        : neighborIds != null && !targets.some((id) => neighborIds.has(id));
    return dimByFilter || dimBySelect;
  };

  // 任务风险三态：卡点等级取最大值；小地图与节点样式共用一份口径。
  const taskRiskLevel = (taskId: number): "high_risk" | "warning" | "" => {
    const taskBlockers = openBlockers.filter((b) => b.taskId === taskId);
    return taskBlockers.some((b) => b.level === "high_risk")
      ? "high_risk"
      : taskBlockers.length > 0
        ? "warning"
        : "";
  };

  // #147：选中边，或选中任务的当前一层边（影响路径模式下为链路内的硬前置边），
  // 临时蓝色粗线＋光晕（PRD §11、CR-11「高亮两端」；节点端淡化沿用 neighborIds）。
  const isEdgeHighlighted = (e: DeliverableEdge) => {
    if (selectedEdge != null) return selectedEdge === e.id;
    if (selectedTask == null) return false;
    if (impactMode) {
      return (
        e.edgeType === "hard_prerequisite" &&
        e.sourceTaskId != null &&
        neighborIds != null &&
        neighborIds.has(e.sourceTaskId) &&
        neighborIds.has(e.targetTaskId)
      );
    }
    return e.sourceTaskId === selectedTask || e.targetTaskId === selectedTask;
  };

  // #147：三个层级共用一份边渲染——可见线、透明命中层、默认隐藏的文字标签
  // （悬停／高亮／异常时显示「关系类型 · 交付物名称」，互锁常驻，原型 labels-key）。
  const renderEdge = (e: DeliverableEdge, from: NodePos, to: NodePos, dim = false) => {
    const st = edgeStroke(e);
    const d = edgePath(from, to);
    const highlighted = isEdgeHighlighted(e);
    const unready = !e.interlockRisk && !e.ready;
    const cls = `edge-g${highlighted ? " highlighted" : ""}${e.interlockRisk ? " interlock anomaly" : ""}${
      unready ? " unready anomaly" : ""
    }`;
    const mx = (from.x + from.w / 2 + to.x + to.w / 2) / 2;
    const my = (from.y + from.h / 2 + to.y + to.h / 2) / 2;
    // 标签截断：交付物名可能是整句描述，常驻标签太长会横穿画布；全称看 <title>。
    const label = `${e.interlockRisk ? "互锁" : e.edgeTypeLabel} · ${e.name}`;
    const shortLabel = label.length > 22 ? `${label.slice(0, 22)}…` : label;
    return (
      <g key={e.id} className={cls} opacity={dim ? 0.15 : 1}>
        <path
          d={d}
          fill="none"
          className="edge-line"
          stroke={highlighted ? "#2f54d4" : st.stroke}
          strokeWidth={highlighted ? 4 : st.width}
          strokeDasharray={highlighted ? undefined : st.dash}
          markerEnd={`url(#cp-arrow-${arrowKind(e)})`}
        />
        <path
          d={d}
          fill="none"
          className="edge-hit"
          stroke="transparent"
          strokeWidth={14}
          style={{ pointerEvents: "stroke", cursor: "pointer" }}
          onClick={() => {
            setSelectedTask(null);
            setImpactMode(false);
            setSelectedEdge((prev) => (prev === e.id ? null : e.id));
          }}
        />
        <text className="edge-label" x={mx} y={my - 8} textAnchor="middle">
          {shortLabel}
        </text>
        <title>{label}</title>
      </g>
    );
  };

  // #146：任务节点是实线圆环（PRD §11）——圆内任务编号、圆下 caption 显任务名；
  // 三态＝灰蓝／橙浅橙底／红浅红底＋「!」标记；生命周期状态与负责人不上画布
  // （详情面板与关系列表本就有）。
  const taskNode = (t: Task, posBase: NodePos) => {
    const pos = taskCircle(t.id, posBase);
    const risk = taskRiskLevel(t.id);
    const dimByFilter = hasFilter && !taskMatchesFilter(t);
    const dimBySelect = neighborIds != null && !neighborIds.has(t.id);
    const select = () => {
      setImpactMode(false);
      setSelectedEdge(null);
      if (mode.kind === "focus") {
        // CR-05：点相邻节点继续展开下一层，画布节点集合随之增长。
        setExpanded((prev) => new Set(prev).add(t.id));
        setSelectedTask(t.id);
        return;
      }
      setSelectedTask((prev) => (prev === t.id ? null : t.id));
    };
    return (
      <div
        key={t.id}
        role="button"
        tabIndex={0}
        className={`gnode gnode-task ${risk ? `risk-${risk}` : ""} ${selectedTask === t.id ? "selected" : ""} ${
          dimByFilter || dimBySelect ? "dimmed" : ""
        }`}
        style={{ left: pos.x, top: pos.y, width: pos.w, height: pos.h }}
        title={`${t.code} · ${t.name}`}
        onMouseDown={(ev) => startDrag(t.id, ev.clientX, ev.clientY)}
        onClick={() => {
          if (suppressClickRef.current) {
            suppressClickRef.current = false;
            return;
          }
          select();
        }}
        onKeyDown={pressAsClick(select)}
        onDoubleClick={() => setDrawerTaskId(t.id)}
      >
        {risk && (
          <span className={`gnode-risk-marker risk-${risk}`} aria-hidden>
            !
          </span>
        )}
        <b>{t.code}</b>
        <small>{t.name}</small>
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
        <div>关系类型：{selectedEdgeObj.edgeTypeLabel}</div>
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
            <Button size="small" onClick={() => setDrawerTaskId(selectedEdgeObj.sourceTaskId ?? null)}>
              打开来源任务
            </Button>
          )}
          <Button size="small" onClick={() => setDrawerTaskId(selectedEdgeObj.targetTaskId)}>
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
      {/* #128：面板完整展示任务内容；放不下的一律单行省略、悬停 title 看全文，不换行撑高。 */}
      <div className="graph-inspector-body">
        <div
          className="muted gi-row"
          title={`${inspectorDetail.objectiveTitle} / ${inspectorDetail.krDescription}`}
        >
          所属：{inspectorDetail.objectiveTitle} / {inspectorDetail.krDescription}
        </div>
        <div className="gi-row" title={inspectorDetail.task.ownerName}>
          负责人：{inspectorDetail.task.ownerName}
        </div>
        <div className="gi-row">
          状态：<span className="status-pill">{inspectorDetail.task.statusLabel}</span>
          {inspectorDetail.task.progress != null && ` · ${inspectorDetail.task.progress}%`}
        </div>
        <div className="gi-row">
          输入 {inspectorDetail.inputs.length} 条 / 输出 {inspectorDetail.outputs.length} 条
        </div>
        {/* 输入／输出行按 #101 读法：编号 · 任务名 · 类型 · 就绪；来源为成员时显成员名。 */}
        {inspectorDetail.inputs.map((e) => {
          const src =
            e.sourceTaskId != null
              ? `${taskById.get(e.sourceTaskId)?.code ?? ""} · ${e.sourceTaskName ?? ""}`
              : (e.inputRequest?.providerName ?? "");
          const line = `← ${src} · ${e.edgeTypeLabel} · ${e.ready ? "已就绪" : "未就绪"}`;
          return (
            <div key={e.id} className="muted gi-row" title={line}>
              {line}
            </div>
          );
        })}
        {inspectorDetail.outputs.map((e) => {
          const line = `→ ${taskById.get(e.targetTaskId)?.code ?? ""} · ${e.targetTaskName ?? ""} · ${e.edgeTypeLabel} · ${e.ready ? "已就绪" : "未就绪"}`;
          return (
            <div key={e.id} className="muted gi-row" title={line}>
              {line}
            </div>
          );
        })}
        {/* 卡点一行一条，按等级配色（与抽屉一致）。 */}
        {inspectorDetail.blockers.map((b) => {
          const line = `卡点 · ${b.kindLabel}：缺 ${b.missing}`;
          return (
            <div key={b.key} className={`gi-row gi-blocker risk-${b.level}`} title={line}>
              {line}
            </div>
          );
        })}
        <div
          className="gi-row"
          title={
            inspectorDetail.deliverables.filter((d) => d.current).length === 0
              ? undefined
              : inspectorDetail.deliverables
                  .filter((d) => d.current)
                  .map((d) => d.current!.fileName)
                  .join("、")
          }
        >
          当前交付物：
          {inspectorDetail.deliverables.filter((d) => d.current).length === 0
            ? "无"
            : inspectorDetail.deliverables
                .filter((d) => d.current)
                .map((d) => d.current!.fileName)
                .join("、")}
        </div>
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
          {/* AC-27：从当前节点向外逐层展开——进入聚焦层后画布节点集合随点击增长，
              而不是像层级模式那样整层替换。 */}
          {mode.kind !== "focus" && (
            <Button size="small" type="primary" onClick={() => enterFocus(selectedTask)}>
              逐层展开
            </Button>
          )}
          <Button size="small" type={impactMode ? "primary" : "default"} onClick={() => setImpactMode((v) => !v)}>
            {impactMode ? "退出影响路径" : "查看影响路径"}
          </Button>
          <Button size="small" onClick={() => setDrawerTaskId(selectedTask)}>
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
          {k.riskLevelLabel}
          {blockers > 0 ? ` · ${blockers} 个卡点` : ""}
        </small>
      </>
    );
  };

  // #145：舞台尺寸按当前层级取布局画布大小；平移缩放作用在整个舞台上。
  const stageSize =
    mode.kind === "kr" && krLayer
      ? { w: krLayer.width, h: krLayer.height }
      : mode.kind === "o" && oLayer
        ? { w: oLayer.width, h: oLayer.height }
        : mode.kind === "focus" && focusLayer
          ? { w: focusLayer.width, h: focusLayer.height }
          : mode.kind === "full" && full
            ? { w: full.width, h: full.height }
            : { w: tree.width, h: tree.height };

  // 小地图模型（原型 cp-minimap）：非归属关系边画线、节点画色点；数据量小，直接算不缓存。
  const mmDots: { x: number; y: number; cls: string }[] = [];
  const mmLines: { x1: number; y1: number; x2: number; y2: number }[] = [];
  // #146：任务／成员的小地图点位取圆心（id 非空按任务圆换算，成员由调用方先转圆）。
  const mmDot = (id: number | null, pos: NodePos, cls: string) => {
    const p = id != null ? taskCircle(id, pos) : pos;
    mmDots.push({ x: p.x + p.w / 2, y: p.y + p.h / 2, cls });
  };
  const mmEdgeLines = (
    edgeList: DeliverableEdge[],
    positions: Map<number, NodePos>,
  ) => {
    for (const e of edgeList) {
      const fromBase = e.sourceTaskId != null ? positions.get(e.sourceTaskId) : positions.get(-e.id);
      const toBase = positions.get(e.targetTaskId);
      if (!fromBase || !toBase) continue;
      const from = e.sourceTaskId != null ? taskCircle(e.sourceTaskId, fromBase) : memberCircle(fromBase);
      const to = taskCircle(e.targetTaskId, toBase);
      mmLines.push({ x1: from.x + from.w / 2, y1: from.y + from.h / 2, x2: to.x + to.w / 2, y2: to.y + to.h / 2 });
    }
  };
  if (mode.kind === "tree") {
    for (const n of tree.nodes) {
      mmDot(null, n.pos, n.kind === "o" ? "objective" : `kr ${krVisualState(n.id)}`);
    }
    for (const l of tree.lines) mmLines.push({ x1: l.x1, y1: l.y1, x2: l.x2, y2: l.y2 });
  } else if (mode.kind === "o" && oLayer) {
    mmDot(null, oLayer.oPos, "objective");
    for (const n of oLayer.krNodes) mmDot(null, n.pos, `kr ${krVisualState(n.id)}`);
    for (const l of oLayer.lines) mmLines.push({ x1: l.x1, y1: l.y1, x2: l.x2, y2: l.y2 });
  } else if (mode.kind === "kr" && krLayer) {
    mmDot(null, krLayer.krPos, `kr ${krVisualState(krLayer.kr.id)}`);
    for (const t of [...krLayer.inKr, ...krLayer.neighbors]) {
      const pos = krLayer.positions.get(t.id);
      if (pos) mmDot(t.id, pos, taskRiskLevel(t.id));
    }
    for (const m of krLayer.memberNodes) {
      const pos = krLayer.positions.get(-m.edgeId);
      if (pos) mmDot(null, memberCircle(pos), "member");
    }
    mmEdgeLines(krLayer.relevantEdges, krLayer.positions);
  } else if (mode.kind === "focus" && focusLayer) {
    for (const t of focusLayer.visibleTasks) {
      const pos = focusLayer.positions.get(t.id);
      if (pos) mmDot(t.id, pos, taskRiskLevel(t.id));
    }
    for (const m of focusLayer.memberNodes) {
      const pos = focusLayer.positions.get(-m.edgeId);
      if (pos) mmDot(null, memberCircle(pos), "member");
    }
    mmEdgeLines(focusLayer.relevantEdges, focusLayer.positions);
  } else if (mode.kind === "full" && full) {
    for (const n of full.oNodes) mmDot(null, n.pos, "objective");
    for (const n of full.krNodes) mmDot(null, n.pos, `kr ${krVisualState(n.id)}`);
    for (const t of full.visibleTasks) {
      const pos = full.positions.get(t.id);
      if (pos) mmDot(t.id, pos, taskRiskLevel(t.id));
    }
    for (const m of full.memberNodes) mmDot(null, memberCircle(m.pos), "member");
    mmEdgeLines(full.visibleEdges, full.positions);
  }
  const mmDotR = Math.max(10, stageSize.w / 110);

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
                <Button onClick={() => { setMode({ kind: "tree" }); setViewStack([]); setExpanded(new Set()); setSelectedTask(null); resetViewport(); }}>
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
              {/* CR-18（#144）：页面只有一个搜索框——列表复用顶部工具栏的搜索与筛选，
                  排序与只读说明并入表头（原型 cp-list-head 形态）。 */}
              <div className="list-card-head">
                <div>
                  <b>{listRows.length} 条任务关系</b>
                  <span>与图谱共享搜索、O／KR／人员筛选和已完成任务范围；只读呈现，业务处理从任务相关页面进入</span>
                </div>
                <label>
                  排序
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
                </label>
              </div>
              <div className="data-table-wrap">
                <table className="data-table">
                  <thead>
                    <tr>
                      <th>来源任务／成员</th>
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
                        <td colSpan={9}>
                          <div className="empty">没有匹配的协作关系</div>
                        </td>
                      </tr>
                    )}
                    {listRows.map((e) => (
                      <tr key={e.id}>
                        <td title={e.sourceTaskName ?? e.inputRequest?.providerName ?? "—"}>
                          {e.sourceTaskName ?? e.inputRequest?.providerName ?? "—"}
                        </td>
                        <td title={e.edgeTypeLabel}>{e.edgeTypeLabel}</td>
                        <td>{e.necessity === "required" ? "必要" : "参考"}</td>
                        {/* 裁决 J1（#142）：「当前交付物」列显示「类型 · 大小」（类型由服务端派生），
                            来源任务多项时显示「N 项」并悬停列出各项「文件名 · 大小」；
                            候选提示与内容同排一行（#91），行高不随内容变化。 */}
                        <td title={edgeCurrentTitle(e) + (e.hasCandidate ? " · 候选审核中" : "")}>
                          {edgeCurrentCell(e) ?? <span className="muted">暂无</span>}
                          {e.hasCandidate && <span className="muted"> · 候选审核中</span>}
                        </td>
                        <td title={e.targetTaskName}>{e.targetTaskName}</td>
                        <td title={e.sourceOwnerName ?? e.inputRequest?.providerName ?? "—"}>
                          {e.sourceOwnerName ?? e.inputRequest?.providerName ?? "—"}
                        </td>
                        <td>
                          <span className={`status-pill ${e.ready ? "completed" : "warning"}`}>
                            {e.ready ? "已就绪" : "未就绪"}
                          </span>
                        </td>
                        <td>{e.expectedDate ?? "—"}</td>
                        <td>
                          <Button type="link" size="small" onClick={() => setDrawerTaskId(e.targetTaskId)}>
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
          <div className="graph-layout">
            {/* #144：风险队列在全局展开下同样保留（PRD §5.2 无隐藏规定，原型两种模式恒在）。 */}
            <aside className="risk-queue">
                <div className="risk-queue-head">风险队列</div>
                {/* #122：按 KR 聚合，每 KR 一条——编号 · 等级 · 卡点数，副行只显最高风险卡点
                    （挑选规则在域层，前端只消费 topBlocker）；正常且无卡点的 KR 不出现。 */}
                {krList.filter((k) => k.riskLevel !== "normal" || (k.openBlockerCount ?? 0) > 0)
                  .length === 0 && (
                  <div className="muted" style={{ padding: 16, fontSize: 12 }}>
                    暂无需要关注的风险
                  </div>
                )}
                {krList
                  .filter((k) => k.riskLevel !== "normal" || (k.openBlockerCount ?? 0) > 0)
                  .map((k) => (
                    <button key={`rk-${k.id}`} type="button" className="risk-queue-item" onClick={() => enter({ kind: "kr", krId: k.id })}>
                      <b>
                        {k.code} · {k.riskLevelLabel}
                        {(k.openBlockerCount ?? 0) > 0 && ` · ${k.openBlockerCount} 个卡点`}
                      </b>
                      <small>
                        {k.topBlocker
                          ? `${k.topBlocker.taskCode} ${k.topBlocker.kindLabel}：${k.topBlocker.summary}`
                          : (k.riskNote ?? k.description)}
                      </small>
                    </button>
                  ))}
            </aside>
            <div className="graph-shell">
              {(mode.kind === "full" || mode.kind === "kr" || mode.kind === "focus") && (edgeInspector || inspector)}
              {/* #145：画布操作按原型三簇布置——返回左上、缩放簇顶部居中、重新布局右上，
                  全层级可用（PRD §5.1、§6.4）。 */}
              <div className="graph-ops graph-ops-left">
                <Button size="small" disabled={mode.kind === "tree"} onClick={back}>
                  ← 返回上一级
                </Button>
              </div>
              <div className="graph-ops graph-ops-center">
                <Button size="small" aria-label="缩小" onClick={() => setZoom((z) => Math.max(ZOOM_MIN, z - 0.15))}>
                  −
                </Button>
                <span className="muted" style={{ fontSize: 12 }}>{Math.round(zoom * 100)}%</span>
                <Button size="small" aria-label="放大" onClick={() => setZoom((z) => Math.min(ZOOM_MAX, z + 0.15))}>
                  ＋
                </Button>
                <Button size="small" onClick={resetViewport}>
                  适应
                </Button>
              </div>
              <div
                className={`graph-ops graph-ops-right${
                  (mode.kind === "full" || mode.kind === "kr" || mode.kind === "focus") &&
                  (edgeInspector || inspector)
                    ? " with-inspector"
                    : ""
                }`}
              >
                <Button size="small" onClick={relayout}>
                  重新布局
                </Button>
              </div>
              <div
                className={`graph-viewport${panning ? " dragging" : ""}`}
                ref={viewportRef}
                onPointerDown={onViewportPointerDown}
                onPointerMove={onViewportPointerMove}
                onPointerUp={onViewportPointerUp}
                onPointerCancel={onViewportPointerUp}
              >
                <div
                  className="graph-stage"
                  style={{
                    width: stageSize.w,
                    height: stageSize.h,
                    transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})`,
                  }}
                >
              {mode.kind === "o" && oLayer ? (
                <>
                  <svg className="graph-svg" width={oLayer.width} height={oLayer.height}>
                    {oLayer.lines.map((l, i) => (
                      <path key={i} d={l.d} fill="none" stroke="#b8c4ce" strokeWidth={1.6} opacity={l.dimmed ? 0.2 : 1} />
                    ))}
                  </svg>
                  <div
                    className="gnode gnode-o"
                    style={{ left: oLayer.oPos.x, top: oLayer.oPos.y, width: oLayer.oPos.w, height: oLayer.oPos.h }}
                  >
                    <b>{oLayer.o.title}</b>
                    <small>{oLayer.krNodes.length} 个 KR</small>
                  </div>
                  {oLayer.krNodes.map((n) => (
                    <div
                      key={`okr-${n.id}`}
                      role="button"
                      tabIndex={0}
                      className={`gnode gnode-kr ${n.dimmed ? "dimmed" : ""} ${
                        krVisualState(n.id) !== "normal" ? `risk-${krVisualState(n.id)}` : ""
                      }`}
                      style={{ left: n.pos.x, top: n.pos.y, width: n.pos.w, height: n.pos.h }}
                      onClick={() => enter({ kind: "kr", krId: n.id })}
                      onKeyDown={pressAsClick(() => enter({ kind: "kr", krId: n.id }))}
                    >
                      {krNodeContent(n.id)}
                    </div>
                  ))}
                </>
              ) : mode.kind === "tree" ? (
                <>
                  <svg className="graph-svg" width={tree.width} height={tree.height}>
                    {tree.lines.map((l, i) => (
                      <path key={i} d={l.d} fill="none" stroke="#b8c4ce" strokeWidth={1.6} opacity={l.dimmed ? 0.2 : 1} />
                    ))}
                  </svg>
                  {tree.nodes.map((n) =>
                    n.kind === "o" ? (
                      <div
                        key={n.key}
                        role="button"
                        tabIndex={0}
                        className={`gnode gnode-o ${n.dimmed ? "dimmed" : ""}`}
                        style={{ left: n.pos.x, top: n.pos.y, width: n.pos.w, height: n.pos.h }}
                        onClick={() => enter({ kind: "o", objectiveId: n.id })}
                        onKeyDown={pressAsClick(() => enter({ kind: "o", objectiveId: n.id }))}
                      >
                        <b>{objectives.find((o) => o.id === n.id)?.title}</b>
                        <small>{krList.filter((k) => k.objectiveId === n.id).length} 个 KR</small>
                      </div>
                    ) : (
                      <div
                        key={n.key}
                        role="button"
                        tabIndex={0}
                        className={`gnode gnode-kr ${n.dimmed ? "dimmed" : ""} ${
                          krVisualState(n.id) !== "normal" ? `risk-${krVisualState(n.id)}` : ""
                        }`}
                        style={{ left: n.pos.x, top: n.pos.y, width: n.pos.w, height: n.pos.h }}
                        onClick={() => enter({ kind: "kr", krId: n.id })}
                        onKeyDown={pressAsClick(() => enter({ kind: "kr", krId: n.id }))}
                      >
                        {krNodeContent(n.id)}
                      </div>
                    ),
                  )}
                </>
              ) : mode.kind === "kr" && krLayer ? (
                <>
                  <svg className="graph-svg" width={krLayer.width} height={krLayer.height}>
                    {arrowDefs}
                    {/* KR→任务 owns 直线（#148）：灰、无箭头，绘于关系边之下；
                        端点跟随拖拽后的任务圆心。 */}
                    {krLayer.inKr.map((t) => {
                      const base = krLayer.positions.get(t.id);
                      if (!base) return null;
                      const p = taskCircle(t.id, base);
                      return (
                        <path
                          key={`owns-${t.id}`}
                          d={`M ${krLayer.cx} ${krLayer.cy} L ${p.x + p.w / 2} ${p.y + p.h / 2}`}
                          fill="none"
                          stroke="#b2bdca"
                          strokeWidth={1.7}
                        />
                      );
                    })}
                    {krLayer.relevantEdges.map((e) => {
                      const fromBase =
                        e.sourceTaskId != null ? krLayer.positions.get(e.sourceTaskId) : krLayer.positions.get(-e.id);
                      const toBase = krLayer.positions.get(e.targetTaskId);
                      if (!fromBase || !toBase) return null;
                      const from = e.sourceTaskId != null ? taskCircle(e.sourceTaskId, fromBase) : memberCircle(fromBase);
                      const to = taskCircle(e.targetTaskId, toBase);
                      return renderEdge(e, from, to);
                    })}
                  </svg>
                  {/* 中心 KR 节点（#148；CR-21 视觉复用）：已在本层，点它不下钻；
                      空 KR 交给空态提示，不渲染孤零零的中心节点。 */}
                  {(krLayer.inKr.length > 0 || krLayer.neighbors.length > 0) && (
                    <div
                      className={`gnode gnode-kr ${
                        krVisualState(krLayer.kr.id) !== "normal" ? `risk-${krVisualState(krLayer.kr.id)}` : ""
                      }`}
                      style={{
                        left: krLayer.krPos.x,
                        top: krLayer.krPos.y,
                        width: krLayer.krPos.w,
                        height: krLayer.krPos.h,
                        cursor: "default",
                      }}
                    >
                      {krNodeContent(krLayer.kr.id)}
                    </div>
                  )}
                  {[...krLayer.inKr, ...krLayer.neighbors].map((t) => {
                    const pos = krLayer.positions.get(t.id);
                    return pos ? taskNode(t, pos) : null;
                  })}
                  {krLayer.memberNodes.map((m) => {
                    const posBase = krLayer.positions.get(-m.edgeId);
                    if (!posBase) return null;
                    // #146：成员节点是紫圆（原型 person），圆内姓名前两字、圆下 caption 显输入名。
                    const pos = memberCircle(posBase);
                    const select = () => {
                      setSelectedTask(null);
                      setImpactMode(false);
                      setSelectedEdge((prev) => (prev === m.edgeId ? null : m.edgeId));
                    };
                    return (
                      <div
                        key={`m-${m.edgeId}`}
                        role="button"
                        tabIndex={0}
                        className={`gnode gnode-member ${selectedEdge === m.edgeId ? "selected" : ""} ${
                          memberDimmed([m.edgeId]) ? "dimmed" : ""
                        }`}
                        style={{ left: pos.x, top: pos.y, width: pos.w, height: pos.h }}
                        title={`${m.label} · ${m.inputName || "输入提供成员"}`}
                        onClick={select}
                        onKeyDown={pressAsClick(select)}
                      >
                        <b>{m.label.slice(0, 2)}</b>
                        <small>{m.inputName || "输入提供成员"}</small>
                      </div>
                    );
                  })}
                </>
              ) : mode.kind === "focus" && focusLayer ? (
                <>
                  <svg className="graph-svg" width={focusLayer.width} height={focusLayer.height}>
                    {arrowDefs}
                    {focusLayer.relevantEdges.map((e) => {
                      const fromBase =
                        e.sourceTaskId != null
                          ? focusLayer.positions.get(e.sourceTaskId)
                          : focusLayer.positions.get(-e.id);
                      const toBase = focusLayer.positions.get(e.targetTaskId);
                      if (!fromBase || !toBase) return null;
                      const from = e.sourceTaskId != null ? taskCircle(e.sourceTaskId, fromBase) : memberCircle(fromBase);
                      const to = taskCircle(e.targetTaskId, toBase);
                      return renderEdge(e, from, to);
                    })}
                  </svg>
                  {focusLayer.visibleTasks.map((t) => {
                    const pos = focusLayer.positions.get(t.id);
                    return pos ? taskNode(t, pos) : null;
                  })}
                  {focusLayer.memberNodes.map((m) => {
                    const posBase = focusLayer.positions.get(-m.edgeId);
                    if (!posBase) return null;
                    const pos = memberCircle(posBase);
                    const select = () => {
                      setSelectedTask(null);
                      setImpactMode(false);
                      setSelectedEdge((prev) => (prev === m.edgeId ? null : m.edgeId));
                    };
                    return (
                      <div
                        key={`fx-m-${m.edgeId}`}
                        role="button"
                        tabIndex={0}
                        className={`gnode gnode-member ${selectedEdge === m.edgeId ? "selected" : ""} ${
                          memberDimmed([m.edgeId]) ? "dimmed" : ""
                        }`}
                        style={{ left: pos.x, top: pos.y, width: pos.w, height: pos.h }}
                        title={`${m.label} · ${m.inputName || "输入提供成员"}`}
                        onClick={select}
                        onKeyDown={pressAsClick(select)}
                      >
                        <b>{m.label.slice(0, 2)}</b>
                        <small>{m.inputName || "输入提供成员"}</small>
                      </div>
                    );
                  })}
                </>
              ) : mode.kind === "full" && full ? (
                <>
                    <svg className="graph-svg" width={full.width} height={full.height}>
                      {arrowDefs}
                      {/* O→KR→任务层级连线（#123）：owns 边灰色无箭头，绘于关系边之下。 */}
                      {full.ownsLines.map((l, i) => {
                        const dim =
                          hasFilter &&
                          (l.taskId != null
                            ? !taskMatchesFilter(taskById.get(l.taskId)!)
                            : !tasks.some((t) => t.keyResultId === l.krId && taskMatchesFilter(t)));
                        return (
                          <path
                            key={`owns-${i}`}
                            d={l.d}
                            fill="none"
                            stroke="#b8c4ce"
                            strokeWidth={1.6}
                            opacity={dim ? 0.15 : 1}
                          />
                        );
                      })}
                      {full.visibleEdges.map((e) => {
                        const fromBase = e.sourceTaskId != null ? full.positions.get(e.sourceTaskId) : full.positions.get(-e.id);
                        const toBase = full.positions.get(e.targetTaskId);
                        const from = fromBase
                          ? e.sourceTaskId != null
                            ? taskCircle(e.sourceTaskId, fromBase)
                            : memberCircle(fromBase)
                          : fromBase;
                        const to = toBase ? taskCircle(e.targetTaskId, toBase) : toBase;
                        if (!from || !to) return null;
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
                        return renderEdge(e, from, to, dim);
                      })}
                    </svg>
                    {/* O／KR 真实节点（#123）：视觉与层级树同一套（gnode-o／gnode-kr + krNodeContent），
                        点击下钻到对应层级；筛选下无匹配后代任务即淡化。 */}
                    {full.oNodes.map((n) => {
                      const dim =
                        hasFilter &&
                        !tasks.some(
                          (t) => krById.get(t.keyResultId)?.objectiveId === n.id && taskMatchesFilter(t),
                        );
                      return (
                        <div
                          key={`fo-${n.id}`}
                          role="button"
                          tabIndex={0}
                          className={`gnode gnode-o ${dim ? "dimmed" : ""}`}
                          style={{ left: n.pos.x, top: n.pos.y, width: n.pos.w, height: n.pos.h }}
                          onClick={() => enter({ kind: "o", objectiveId: n.id })}
                          onKeyDown={pressAsClick(() => enter({ kind: "o", objectiveId: n.id }))}
                        >
                          <b>{n.title}</b>
                          <small>{n.krCount} 个 KR</small>
                        </div>
                      );
                    })}
                    {full.krNodes.map((n) => {
                      const dim =
                        hasFilter && !tasks.some((t) => t.keyResultId === n.id && taskMatchesFilter(t));
                      return (
                        <div
                          key={`fk-${n.id}`}
                          role="button"
                          tabIndex={0}
                          className={`gnode gnode-kr ${dim ? "dimmed" : ""} ${
                            krVisualState(n.id) !== "normal" ? `risk-${krVisualState(n.id)}` : ""
                          }`}
                          style={{ left: n.pos.x, top: n.pos.y, width: n.pos.w, height: n.pos.h }}
                          onClick={() => enter({ kind: "kr", krId: n.id })}
                          onKeyDown={pressAsClick(() => enter({ kind: "kr", krId: n.id }))}
                        >
                          {krNodeContent(n.id)}
                        </div>
                      );
                    })}
                    {full.visibleTasks.slice(0, renderBudget).map((t) => {
                      const pos = full.positions.get(t.id);
                      return pos ? taskNode(t, pos) : null;
                    })}
                    {full.memberNodes.map((m) => {
                      const pos = memberCircle(m.pos);
                      const select = () => {
                        setSelectedTask(null);
                        setImpactMode(false);
                        const first = m.edgeIds[0];
                        setSelectedEdge((prev) => (prev != null && m.edgeIds.includes(prev) ? null : first));
                      };
                      return (
                        <div
                          key={`fm-${m.providerId}`}
                          role="button"
                          tabIndex={0}
                          className={`gnode gnode-member ${
                            selectedEdge != null && m.edgeIds.includes(selectedEdge) ? "selected" : ""
                          } ${memberDimmed(m.edgeIds) ? "dimmed" : ""}`}
                          style={{ left: pos.x, top: pos.y, width: pos.w, height: pos.h }}
                          title={`${m.name} · 关系相关项目成员`}
                          onClick={select}
                          onKeyDown={pressAsClick(select)}
                        >
                          <b>{m.name.slice(0, 2)}</b>
                          <small>关系相关项目成员</small>
                        </div>
                      );
                    })}
                </>
              ) : null}
                </div>
              </div>
              {/* 说明条与空态是画布覆盖层，不随平移缩放（#145）。 */}
              {mode.kind === "full" ? (
                full && renderBudget < full.visibleTasks.length ? (
                  <div className="graph-note">
                    正在补齐剩余 {full.visibleTasks.length - renderBudget} 个节点，画布已可缩放、拖动与返回；
                    数据量较大时可按 O、KR 或人员缩小范围
                  </div>
                ) : null
              ) : (
                <div className="graph-note">
                  {mode.kind === "kr" && krLayer ? (
                    `${krLayer.kr.code} 任务关系层：关系默认灰线、互锁红色虚线常驻；点击任务或关系后当前一层蓝色高亮，悬停边显示类型与交付物名`
                  ) : mode.kind === "focus" && focusLayer ? (
                    <>
                      以「{focusLayer.origin.name}」为起点逐层展开：点任一相邻节点继续展开下一层，
                      当前 {focusLayer.visibleTasks.length} 个任务在画布上
                      {expanded.size > 1 && (
                        <Button
                          size="small"
                          type="link"
                          style={{ padding: "0 6px" }}
                          onClick={() => {
                            // CR §6.2：已展开节点要有明确的「收起」控制，不靠双击这类隐藏手势。
                            setExpanded(new Set([focusLayer.origin.id]));
                            setSelectedTask(focusLayer.origin.id);
                            setSelectedEdge(null);
                          }}
                        >
                          收起到起点
                        </Button>
                      )}
                    </>
                  ) : mode.kind === "o" && oLayer ? (
                    `聚焦「${oLayer.o.title}」：只显示该 O 与下属 KR；点击 KR 进入任务关系层`
                  ) : (
                    "默认只显示 O、KR 与层级连线（不可点击）；点击 O 聚焦下属 KR，点击 KR 进入任务关系层"
                  )}
                </div>
              )}
              {mode.kind === "kr" && krLayer && krLayer.inKr.length === 0 && krLayer.neighbors.length === 0 && (
                <div className="graph-empty">
                  {krLayer.hiddenCompleted > 0
                    ? "该 KR 下的任务已全部完成，打开「显示已完成」查看"
                    : "该 KR 下还没有任务"}
                </div>
              )}
              {/* #147：左下角紧凑图例（原型 cp-legend）；只解释常驻视觉，
                  不解释临时焦点蓝色（§11）。 */}
              <div className="graph-legend" aria-hidden>
                <span><i className="lg-o" />O</span>
                <span><i className="lg-kr" />KR</span>
                <span><i className="lg-task" />任务</span>
                <span><i className="lg-warning" />预警</span>
                <span><i className="lg-risk" />高风险／卡点</span>
                <span><i className="lg-line" />普通关系</span>
                <span><i className="lg-feedback" />反馈</span>
                <span><i className="lg-interlock" />互锁</span>
              </div>
              {/* 小地图（PRD §6.4，原型 cp-minimap）：内容全貌＋当前视口框。 */}
              <div className="graph-minimap" aria-hidden>
                <svg viewBox={`0 0 ${stageSize.w} ${stageSize.h}`} preserveAspectRatio="xMidYMid meet">
                  {mmLines.map((l, i) => (
                    <line key={i} x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2} />
                  ))}
                  {mmDots.map((d, i) => (
                    <circle key={i} cx={d.x} cy={d.y} r={mmDotR} className={d.cls} />
                  ))}
                  <rect
                    className="mm-view"
                    x={-pan.x / zoom}
                    y={-pan.y / zoom}
                    width={viewportSize.w / zoom}
                    height={viewportSize.h / zoom}
                  />
                </svg>
              </div>
            </div>
          </div>
          )}
        </>
      )}
      {/* #121：任务抽屉在本页打开；动作落库后刷新图谱数据（边、卡点、任务状态）。
          抽屉内「在关系图谱中查看」在本页改为关闭抽屉并聚焦该任务，不再跳页。 */}
      <TaskDrawerHost
        projectId={projectId}
        taskId={drawerTaskId}
        onClose={() => setDrawerTaskId(null)}
        onChanged={load}
        onOpenInGraph={(id) => {
          setDrawerTaskId(null);
          const t = taskById.get(id);
          if (t) {
            setMode({ kind: "kr", krId: t.keyResultId });
            setViewStack([]);
            setSelectedTask(id);
            setSelectedEdge(null);
            setImpactMode(false);
          }
        }}
      />
    </ProjectShell>
  );
}

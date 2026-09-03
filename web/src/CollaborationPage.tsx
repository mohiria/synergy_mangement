import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { Alert, AutoComplete, Button, Input, Select, Spin, Switch, message } from "antd";
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

// #146：任务节点是实线圆环（PRD §11；原型 nodeMarkup r=29、聚焦起点 r=35）。
// #159 裁决：图谱节点只有 O／KR／任务三类，成员节点及其连线不再渲染。
const TASK_R = 29;
const TASK_R_FOCUS = 35;

// #124 同口径（ArtifactsPage）：这些类型走浏览器内联预览，其余只下载。
const PREVIEWABLE = new Set(["pdf", "png", "jpg", "jpeg", "gif", "webp", "txt"]);

// #149：图谱选中对象——任务与边沿用独立 state（联动淡化），O／KR 是纯详情选中
// （#159 后成员不再是图谱节点）。
type NodeSelection = { kind: "o" | "kr"; id: number };

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

// 关系列表「当前交付物」列（裁决 J1、#163）：来源任务全部当前文件，显示「类型 · 大小」，
// 类型文案由服务端派生；多项当前内容时显示「N 项」，悬停列出各项「文件名 · 大小」。
function edgeCurrentFiles(e: DeliverableEdge): { fileName: string; fileTypeLabel: string; fileSize: number }[] {
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
  // #149：O／KR／成员节点的详情选中（§8.1）；点 O／KR 下钻的同时打开节点详情。
  const [selectedNode, setSelectedNode] = useState<NodeSelection | null>(null);
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

  // 拖拽固定的缓存 key：不同层级布局坐标系不同，偏移只在本视图内生效（CR-19）。
  // #151：key 从任务 id 泛化为节点 key（t:／o:／kr:／m:边／mp:成员），全部节点可拖。
  const modeKey =
    mode.kind === "kr" ? `kr:${mode.krId}` : mode.kind === "focus" ? `focus:${mode.taskId}` : mode.kind;
  const offsetKey = (nodeKey: string) => `${projectId}:${modeKey}:${nodeKey}`;

  const resetViewport = () => {
    setZoom(1);
    setPan({ x: 0, y: 0 });
  };

  const enter = (next: Mode) => {
    setViewStack((v) => [...v, { oFilter, krFilter, personFilter, zoom, pan }]);
    setMode(next);
    setSelectedTask(null);
    setSelectedNode(null);
    setExpanded(new Set());
    resetViewport();
  };

  // #149：点 O／KR 在进入对应层级的同时选中该节点并打开详情（原型行为、§8.1）。
  const enterO = (id: number) => {
    enter({ kind: "o", objectiveId: id });
    setSelectedNode({ kind: "o", id });
  };
  const enterKr = (id: number) => {
    enter({ kind: "kr", krId: id });
    setSelectedNode({ kind: "kr", id });
  };

  // 进入任务聚焦层：以该任务为起点，先展开它自己的一层邻居（AC-27）。
  const enterFocus = (taskId: number) => {
    setViewStack((v) => [...v, { oFilter, krFilter, personFilter, zoom, pan }]);
    setMode({ kind: "focus", taskId });
    setExpanded(new Set([taskId]));
    setSelectedTask(taskId);
    setSelectedEdge(null);
    setSelectedNode(null);
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
    setSelectedNode(null);
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
  // #159：人员筛选语义＝按任务负责人或参与人过滤任务节点（图谱已无成员节点）。
  const matchesPerson = useCallback(
    (t: Task) =>
      personFilter === "all" ||
      t.ownerId === personFilter ||
      (t.participants ?? []).some((p) => p.userId === personFilter),
    [personFilter],
  );
  const krPass = useCallback(
    (k: (typeof krList)[number]) =>
      (oFilter === "all" || k.objectiveId === oFilter) &&
      (krFilter === "all" || k.id === krFilter) &&
      (personFilter === "all" || tasks.some((t) => t.keyResultId === k.id && matchesPerson(t))),
    [oFilter, krFilter, personFilter, tasks, matchesPerson],
  );

  // —— O／KR 层级树布局（#148 对齐原型 aggregateModel）：横向——O 排上方，
  // 下属 KR 在其下方 2 列网格（单 KR 1 列），O→KR 走 V-H-V 正交线。 ——
  const tree = useMemo(() => {
    const nodes: { key: string; kind: "o" | "kr"; id: number; pos: NodePos; dimmed: boolean }[] = [];
    // #151：连线只记两端节点，路径在渲染时按拖拽偏移后的位置重算。
    const links: { oId: number; krId: number; dimmed: boolean }[] = [];
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
      krs.forEach((k, i) => {
        const kx = x + (i % columns) * (krW + gapX);
        const ky = 160 + Math.floor(i / columns) * (krH + rowGap);
        const krDimmed = filtering && !krPass(k);
        nodes.push({ key: `kr-${k.id}`, kind: "kr", id: k.id, pos: { x: kx, y: ky, w: krW, h: krH }, dimmed: krDimmed });
        links.push({ oId: o.id, krId: k.id, dimmed: oDimmed || krDimmed });
        maxY = Math.max(maxY, ky + krH);
      });
      x += colW + colGap;
    }
    return { nodes, links, width: Math.max(700, x - colGap + 24), height: maxY + 40 };
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
    const krNodes = krs.map((k, i) => {
      const kx = startX + i * (krW + gapX);
      return { id: k.id, pos: { x: kx, y: krY, w: krW, h: krH }, dimmed: filtering && !krPass(k) };
    });
    return { o, oPos, krNodes, width, height: krY + krH + 60 };
  }, [mode, objectives, krList, filtering, krPass]);

  // —— KR 任务关系层布局（#152 用户裁定，偏离原型放射形态）：左→右流向——
  // KR 固定画布最左，本 KR 任务按 KR 内依赖链深度分列、自左向右展开；
  // 外部相邻任务放最右一列（#159：成员输入不上画布，只在详情面板表达）。 ——
  const krLayer = useMemo(() => {
    if (mode.kind !== "kr") return null;
    const kr = krById.get(mode.krId);
    if (!kr) return null;
    const inKr = tasks.filter((t) => t.keyResultId === mode.krId && isTaskVisible(t));
    const inKrIds = new Set(inKr.map((t) => t.id));
    // #159：只保留任务↔任务边；成员来源（sourceTaskId 为空）的输入边不绘制。
    const relevantEdges = edges.filter(
      (e) => e.sourceTaskId != null && (inKrIds.has(e.sourceTaskId) || inKrIds.has(e.targetTaskId)),
    );
    const neighborIds = new Set<number>();
    for (const e of relevantEdges) {
      if (e.sourceTaskId != null && !inKrIds.has(e.sourceTaskId)) neighborIds.add(e.sourceTaskId);
      if (!inKrIds.has(e.targetTaskId)) neighborIds.add(e.targetTaskId);
    }
    const neighbors = tasks.filter((t) => neighborIds.has(t.id) && isTaskVisible(t));
    // 槽位存圆心对齐的包围盒（circleBounds 换算后圆心正落 (X,Y)）。
    const slot = (X: number, Y: number, r = TASK_R): NodePos => ({ x: X - r, y: Y - r, w: r * 2, h: r * 2 });
    // 列号＝KR 内依赖深度：没有 KR 内上游的任务在第 0 列，下游任务列号取上游最大值＋1；
    // 互锁环内的任务按先到先得截断，不死循环。
    const depth = new Map<number, number>();
    const upstreamIn = (id: number) =>
      relevantEdges
        .filter((e) => e.targetTaskId === id && e.sourceTaskId != null && inKrIds.has(e.sourceTaskId))
        .map((e) => e.sourceTaskId!);
    const calcDepth = (id: number, seen: Set<number>): number => {
      const cached = depth.get(id);
      if (cached != null) return cached;
      if (seen.has(id)) return 0;
      seen.add(id);
      const ups = upstreamIn(id);
      const d = ups.length === 0 ? 0 : Math.max(...ups.map((u) => calcDepth(u, seen))) + 1;
      depth.set(id, d);
      return d;
    };
    inKr.forEach((t) => calcDepth(t.id, new Set()));
    const byCol = new Map<number, Task[]>();
    inKr.forEach((t) => {
      const c = depth.get(t.id) ?? 0;
      byCol.set(c, [...(byCol.get(c) ?? []), t]);
    });
    const colCount = byCol.size === 0 ? 0 : Math.max(...byCol.keys()) + 1;
    const krW = 300;
    const krH = 62;
    const krX = 40;
    const colStart = krX + krW + 150; // 第一列任务圆心 x
    const colGap = 190;
    const rowGap = 145;
    const topY = 130; // 首行任务圆心 y
    const positions = new Map<number, NodePos>();
    let maxRows = inKr.length > 0 ? 1 : 0;
    for (const [c, list] of byCol) {
      list.sort((a, b) => a.id - b.id);
      maxRows = Math.max(maxRows, list.length);
      list.forEach((t, i) => positions.set(t.id, slot(colStart + c * colGap, topY + i * rowGap)));
    }
    // 外部相邻任务：最右一列（跨 KR 的边由 edgePath 的反向曲率区分方向）。
    const neighborX = colStart + colCount * colGap + 60;
    neighbors.forEach((t, i) => positions.set(t.id, slot(neighborX, topY + i * rowGap)));
    // KR 节点（CR-21 白底三态复用；已在本层，点它不再下钻）：最左、对任务行竖向居中。
    const krCy = maxRows > 0 ? topY + ((maxRows - 1) * rowGap) / 2 : topY;
    const krPos = { x: krX, y: krCy - krH / 2, w: krW, h: krH };
    // 「显示已完成」关闭时被藏起来的本 KR 任务数：全完成的 KR 点进来是空画布，
    // 空态要能指出「打开开关就看得到」（Q-10、AC-45）。
    const hiddenCompleted = tasks.filter(
      (t) => t.keyResultId === mode.krId && t.status === "completed" && !isTaskVisible(t),
    ).length;
    const rows = Math.max(maxRows, neighbors.length);
    const height = Math.max(topY + rows * rowGap + 80, krPos.y + krH + 80);
    const width = Math.max(700, neighborX + 130);
    return { kr, krPos, inKr, neighbors, relevantEdges, positions, hiddenCompleted, width, height };
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
    // #159：成员来源的输入不上画布，只保留任务↔任务边（成员输入在任务详情面板表达）。
    const relevantEdges = edges.filter(
      (e) => e.sourceTaskId != null && visible.has(e.sourceTaskId) && visible.has(e.targetTaskId),
    );
    const width = cx + maxRx + 160;
    const height = cy + maxRy + 140;
    const visibleTasks = [...visible]
      .map((id) => taskById.get(id))
      .filter((t): t is Task => !!t);
    return { origin, visibleTasks, relevantEdges, positions, height, width };
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
    // #151：owns 线只记两端与列脊线位置，路径在渲染时按拖拽偏移后的位置重算。
    const ownsLinks: { oId?: number; krId: number; taskId?: number; spineX?: number }[] = [];
    let maxY = 0;
    objectives.forEach((o, oi) => {
      const colX = 24 + oi * (colW + colGap);
      const innerX = colX + colPad;
      const krs = krList.filter((k) => k.objectiveId === o.id);
      const oPos = { x: colX + (colW - oW) / 2, y: 16, w: oW, h: oH };
      oNodes.push({ id: o.id, title: o.title, krCount: krs.length, pos: oPos });
      const spineX = colX + 5;
      let y = 16 + oH + 30;
      for (const k of krs) {
        const krTasks = visibleTasks.filter((t) => t.keyResultId === k.id);
        const krPos = { x: colX + (colW - krW) / 2, y, w: krW, h: krH };
        krNodes.push({ id: k.id, pos: krPos });
        // O→KR：原型的正交连线（V-H-V），沿列左侧走线不穿下方节点。
        ownsLinks.push({ oId: o.id, krId: k.id, spineX });
        krTasks.forEach((t, i) => {
          const tx = innerX + (i % 2) * (nodeW + gapX);
          const ty = y + krH + 18 + Math.floor(i / 2) * (nodeH + 12);
          positions.set(t.id, { x: tx, y: ty, w: nodeW, h: nodeH });
          // KR→任务：owns 直线绘于节点层之下，端点隐入任务圆心（#146 槽位顶部是圆环）。
          ownsLinks.push({ krId: k.id, taskId: t.id });
        });
        const rows = Math.ceil(krTasks.length / 2);
        y += krH + (rows > 0 ? 18 + rows * (nodeH + 12) : 0) + 26;
      }
      maxY = Math.max(maxY, y);
    });
    // #159：成员不再生成节点，协作线只连接任务↔任务。
    const visibleEdges = edges.filter(
      (e) => e.sourceTaskId != null && visibleIds.has(e.sourceTaskId) && visibleIds.has(e.targetTaskId),
    );
    const height = maxY + 40;
    const width = 24 + objectives.length * (colW + colGap) + 20;
    return { visibleTasks, positions, oNodes, krNodes, ownsLinks, visibleEdges, height, width };
  }, [mode, tasks, edges, objectives, krList, isTaskVisible]);

  // 筛选淡化（AC-09；细化 AC-45 随 #20）：O/KR/人员不匹配 → 淡化保留上下文。
  const taskMatchesFilter = (t: Task) => {
    if (krFilter !== "all" && t.keyResultId !== krFilter) return false;
    if (oFilter !== "all" && krById.get(t.keyResultId)?.objectiveId !== oFilter) return false;
    if (!matchesPerson(t)) return false;
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
      // 影响路径（AC-42；#173 裁决改沿必要边）：沿下游必要边可达的连续链路。
      const queue = [selectedTask];
      while (queue.length > 0) {
        const cur = queue.shift()!;
        for (const e of edges) {
          if (e.necessity === "required" && e.sourceTaskId === cur && !set.has(e.targetTaskId)) {
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
        // 裁决 J1（#142）：「交付物边」列已删，搜索匹配不再含边名，保留任务名。
        const hay = `${e.sourceTaskName ?? ""}${e.targetTaskName ?? ""}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
    rows = [...rows];
    if (listSort === "ready") {
      rows.sort((a, b) => Number(a.ready) - Number(b.ready));
    } else if (listSort === "type") {
      // #173：类型列改为必要性。
      rows.sort((a, b) => a.necessity.localeCompare(b.necessity));
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

  // 该必要边的「上游未就绪」卡点等级（裁决 15，#185）：构成卡点的未就绪边随卡点等级着色。
  const upstreamBlockerLevel = (edgeId: number): string | null =>
    openBlockers.find((b) => b.key === `upstream_unready:edge:${edgeId}`)?.level ?? null;

  // #147 边默认灰色系（PRD §11）；#173 裁决：样式只分三类——必要灰实线、参考灰虚线、
  // 互锁红虚线常驻；裁决 15（#185）：橙色异常提示只对必要边——必要未就绪构成卡点时
  // 随卡点等级着色（预警橙／高风险红），未构成卡点为橙；参考未就绪改中性灰，只作「提醒」；
  // 关键路径只加粗不上色（§6.3）。
  const edgeStroke = (e: DeliverableEdge) => {
    if (e.interlockRisk) return { stroke: "#bd3e49", width: 2.5, dash: "6 4" };
    const dash = e.necessity === "reference" ? "6 4" : undefined;
    if (!e.ready && e.necessity === "required") {
      const level = upstreamBlockerLevel(e.id);
      if (level === "high_risk") return { stroke: "#bd3e49", width: 2, dash };
      return { stroke: "#a86917", width: 2, dash };
    }
    return {
      stroke: "#929dad",
      width: e.onCriticalPath ? 2.4 : 1.6,
      dash,
    };
  };

  // #145：节点拖拽对所有层级生效；#151：O／KR／成员节点同样可拖，连线实时跟随。
  const startDrag = (nodeKey: string, startX: number, startY: number) => {
    const key = offsetKey(nodeKey);
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

  const withOffset = (nodeKey: string, pos: NodePos): NodePos => {
    const off = dragOffsets.get(offsetKey(nodeKey));
    if (!off) return pos;
    return { ...pos, x: pos.x + off.dx, y: pos.y + off.dy };
  };

  // 拖拽超过阈值后吞掉随后的 click（原型 suppressClick）；所有可拖节点的 onClick 共用。
  const guardClick = (fn: () => void) => () => {
    if (suppressClickRef.current) {
      suppressClickRef.current = false;
      return;
    }
    fn();
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
  const taskCircle = (id: number, base: NodePos): NodePos => circleBounds(withOffset(`t:${id}`, base), taskRadius(id));

  // #151：O→KR 的 V-H-V 正交线（原型 edgePath owns 分支，46% 处转水平）——
  // 在渲染时按偏移后的两端位置重算，拖动任一端连线跟随。
  const vhvPath = (o: NodePos, k: NodePos) => {
    const ocx = o.x + o.w / 2;
    const oby = o.y + o.h;
    const kcx = k.x + k.w / 2;
    const ky = k.y;
    const branchY = oby + (ky - oby) * 0.46;
    return { d: `M ${ocx} ${oby} V ${branchY} H ${kcx} V ${ky}`, x1: ocx, y1: oby, x2: kcx, y2: ky };
  };

  // 层级树／O 聚焦／全局展开的偏移后视图：节点位置与层级线都按拖拽偏移重算（#151）。
  const treeNodes = tree.nodes.map((n) => ({ ...n, pos: withOffset(`${n.kind}:${n.id}`, n.pos) }));
  const treeNodeByKey = new Map(treeNodes.map((n) => [n.key, n]));
  const treeLines = tree.links.map((l) => {
    const o = treeNodeByKey.get(`o-${l.oId}`)!;
    const k = treeNodeByKey.get(`kr-${l.krId}`)!;
    return { ...vhvPath(o.pos, k.pos), dimmed: l.dimmed };
  });
  const oLayerOPos = oLayer ? withOffset(`o:${oLayer.o.id}`, oLayer.oPos) : null;
  const oLayerKrNodes = oLayer
    ? oLayer.krNodes.map((n) => ({ ...n, pos: withOffset(`kr:${n.id}`, n.pos) }))
    : [];
  const oLayerLines = oLayerOPos
    ? oLayerKrNodes.map((n) => ({ ...vhvPath(oLayerOPos, n.pos), dimmed: n.dimmed }))
    : [];
  const krCenterPos = krLayer ? withOffset(`kr:${krLayer.kr.id}`, krLayer.krPos) : null;
  const fullONodes = full ? full.oNodes.map((n) => ({ ...n, pos: withOffset(`o:${n.id}`, n.pos) })) : [];
  const fullKrNodes = full ? full.krNodes.map((n) => ({ ...n, pos: withOffset(`kr:${n.id}`, n.pos) })) : [];
  const fullOPosById = new Map(fullONodes.map((n) => [n.id, n.pos]));
  const fullKrPosById = new Map(fullKrNodes.map((n) => [n.id, n.pos]));
  const fullOwnsD = (l: { oId?: number; krId: number; taskId?: number; spineX?: number }): string | null => {
    const kr = fullKrPosById.get(l.krId);
    if (!kr) return null;
    const krCx = kr.x + kr.w / 2;
    const krCy = kr.y + kr.h / 2;
    if (l.taskId != null) {
      const base = full?.positions.get(l.taskId);
      if (!base) return null;
      const p = taskCircle(l.taskId, base);
      return `M ${krCx} ${krCy} L ${p.x + p.w / 2} ${p.y + p.h / 2}`;
    }
    const o = l.oId != null ? fullOPosById.get(l.oId) : undefined;
    if (!o || l.spineX == null) return null;
    const oCx = o.x + o.w / 2;
    const oBy = o.y + o.h;
    return `M ${oCx} ${oBy} V ${oBy + 12} H ${l.spineX} V ${krCy} H ${kr.x}`;
  };

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
      setSelectedNode(null);
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

  // 任务风险三态（裁决 14，#184）：消费 API 派生的任务级 riskLevel，
  // 不再按卡点在前端自行取最大值；小地图与节点样式共用一份口径。
  const taskRiskLevel = (taskId: number): "high_risk" | "warning" | "" => {
    const level = tasks.find((t) => t.id === taskId)?.riskLevel;
    return level === "high_risk" || level === "warning" ? level : "";
  };

  // #147：选中边，或选中任务的当前一层边（影响路径模式下为链路内的硬前置边），
  // 临时蓝色粗线＋光晕（PRD §11、CR-11「高亮两端」；节点端淡化沿用 neighborIds）。
  const isEdgeHighlighted = (e: DeliverableEdge) => {
    if (selectedEdge != null) return selectedEdge === e.id;
    if (selectedTask == null) return false;
    if (impactMode) {
      return (
        e.necessity === "required" &&
        e.sourceTaskId != null &&
        neighborIds != null &&
        neighborIds.has(e.sourceTaskId) &&
        neighborIds.has(e.targetTaskId)
      );
    }
    return e.sourceTaskId === selectedTask || e.targetTaskId === selectedTask;
  };

  // #147：三个层级共用一份边渲染——可见线、透明命中层、默认隐藏的文字标签。
  // 裁决 15（#185）标签契约「必要性 · 异常名」：互锁→「必要 · 互锁」（红，常驻）；
  // 必要未就绪构成卡点→「必要 · 上游未就绪」（随卡点等级着色，常驻）；
  // 必要未就绪未构成卡点→「必要 · 未就绪」（橙，常驻）；参考未就绪→「参考 · 上游未就绪」
  // （中性灰，常驻）；就绪的边仅悬停显示必要性；来源任务全称保留在悬停 title。
  const renderEdge = (e: DeliverableEdge, from: NodePos, to: NodePos, dim = false) => {
    const st = edgeStroke(e);
    const d = edgePath(from, to);
    const highlighted = isEdgeHighlighted(e);
    const requiredUnready = !e.interlockRisk && !e.ready && e.necessity === "required";
    const referenceUnready = !e.interlockRisk && !e.ready && e.necessity === "reference";
    const blockerLevel = requiredUnready ? upstreamBlockerLevel(e.id) : null;
    const cls = `edge-g${highlighted ? " highlighted" : ""}${e.interlockRisk ? " interlock anomaly" : ""}${
      requiredUnready ? " unready anomaly" : ""
    }${blockerLevel === "high_risk" ? " blocker-high" : ""}${referenceUnready ? " reminder" : ""}`;
    const mx = (from.x + from.w / 2 + to.x + to.w / 2) / 2;
    const my = (from.y + from.h / 2 + to.y + to.h / 2) / 2;
    let anomaly: string | null = null;
    if (e.interlockRisk) anomaly = "互锁";
    else if (requiredUnready) anomaly = blockerLevel ? "上游未就绪" : "未就绪";
    else if (referenceUnready) anomaly = "上游未就绪";
    const shortLabel = anomaly ? `${e.necessityLabel} · ${anomaly}` : e.necessityLabel;
    const label = `${e.necessityLabel} · ${e.sourceTaskName ?? e.name}`;
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
            setSelectedNode(null);
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
      setSelectedNode(null);
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
        onMouseDown={(ev) => startDrag(`t:${t.id}`, ev.clientX, ev.clientY)}
        onClick={guardClick(select)}
        onKeyDown={pressAsClick(select)}
        // #177 裁决：双击任务节点进入逐层展开（不再打开任务抽屉）；
        // 聚焦层内单击已按 CR-05 继续展开，双击不再另起动作。
        onDoubleClick={() => {
          if (mode.kind !== "focus") enterFocus(t.id);
        }}
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

  // #149 边详情预览／下载（§8.2）：复用交付物内容的预签名地址口径（#124）——
  // PDF／图片／纯文本内联预览，其余走 attachment 下载；权限由服务端判定（§13）。
  const openEdgeFile = async (fileId: number, fileName: string | undefined, wantPreview: boolean) => {
    const ext = fileName?.split(".").pop()?.toLowerCase();
    const preview = wantPreview && !!ext && PREVIEWABLE.has(ext);
    const res = await client.GET("/projects/{projectId}/files/{fileId}/download-url", {
      params: { path: { projectId, fileId }, query: preview ? { disposition: "inline" } : {} },
    });
    if (!res.data) {
      message.error(res.error?.message ?? "获取下载地址失败");
      return;
    }
    if (preview) window.open(res.data.url, "_blank");
    else window.location.assign(res.data.url);
  };

  const previewable = (fileName?: string) => {
    const ext = fileName?.split(".").pop()?.toLowerCase();
    return !!ext && PREVIEWABLE.has(ext);
  };

  // 任务风险徽章（§8.1；裁决 14 #184）：等级与文案消费任务派生字段 riskLevel／riskLevelLabel，
  // 与节点圆环三态同口径（taskRiskLevel）。
  const taskRiskBadge = (taskId: number): { level: string; label: string } | null => {
    const t = tasks.find((x) => x.id === taskId);
    if (!t || t.riskLevel === "normal") return null;
    return { level: t.riskLevel, label: t.riskLevelLabel };
  };

  const blockerDays = (since: string) =>
    Math.max(0, Math.floor((Date.now() - new Date(since).getTime()) / 86400000));

  const selectedEdgeObj = selectedEdge != null ? edges.find((e) => e.id === selectedEdge) : null;
  // #149 边详情（§8.2）：eyebrow＋标题＋就绪徽章、来源→目标 flow-card（两端可点跳聚焦）、
  // 属性网格、CR-12 候选提示、当前交付物摘要＋预览／下载入口。
  const edgeInspector = selectedEdgeObj && (
    <aside className="graph-inspector">
      <div className="graph-inspector-head">
        <h2>关系详情</h2>
        <button type="button" aria-label="关闭详情" onClick={() => setSelectedEdge(null)}>
          ✕
        </button>
      </div>
      <div className="graph-inspector-body">
        <div className="gi-title">
          <span className="gi-eyebrow">交付物边</span>
          <h2 title={selectedEdgeObj.name}>{selectedEdgeObj.name}</h2>
          {/* 裁决 15（#185）：参考未就绪只是「提醒」，徽标改中性灰。 */}
          <span
            className={`gi-badge ${
              selectedEdgeObj.ready
                ? "ready"
                : selectedEdgeObj.necessity === "reference"
                  ? ""
                  : "risk-warning"
            }`}
          >
            {selectedEdgeObj.ready ? "已就绪" : "未就绪"}
          </span>
        </div>
        <div className="gi-flow">
          <button
            type="button"
            onClick={() => {
              if (selectedEdgeObj.sourceTaskId != null) enterFocus(selectedEdgeObj.sourceTaskId);
            }}
            title={selectedEdgeObj.sourceTaskName}
          >
            <span>{selectedEdgeObj.sourceTaskCode ?? ""}</span>
            <b>{selectedEdgeObj.sourceTaskName ?? "—"}</b>
          </button>
          <i>→</i>
          <button
            type="button"
            onClick={() => enterFocus(selectedEdgeObj.targetTaskId)}
            title={selectedEdgeObj.targetTaskName}
          >
            <span>{taskById.get(selectedEdgeObj.targetTaskId)?.code ?? ""}</span>
            <b>{selectedEdgeObj.targetTaskName ?? "—"}</b>
          </button>
        </div>
        {/* #173 裁决：关系类型删除，边详情只显必要性。 */}
        <div className="gi-grid">
          <div className="gi-prop">
            <span>必要性</span>
            <strong>{selectedEdgeObj.necessityLabel}</strong>
          </div>
          <div className="gi-prop">
            <span>提供方</span>
            <strong>{selectedEdgeObj.sourceOwnerName ?? "—"}</strong>
          </div>
          <div className="gi-prop">
            <span>接收方</span>
            <strong>{taskById.get(selectedEdgeObj.targetTaskId)?.ownerName ?? "—"}</strong>
          </div>
          <div className="gi-prop">
            <span>期望时间</span>
            <strong>{selectedEdgeObj.expectedDate ?? "—"}</strong>
          </div>
          <div className="gi-prop">
            <span>关键路径</span>
            <strong>
              {selectedEdgeObj.interlockRisk
                ? "互锁，暂停计算"
                : selectedEdgeObj.onCriticalPath
                  ? "关键路径"
                  : selectedEdgeObj.necessity === "required"
                    ? "依赖链"
                    : "不参与"}
            </strong>
          </div>
        </div>
        {/* 互锁解释与关键路径降级提示（PRD §4.4、AC-10）：等级本身不说明问题出在哪，
            这里把「为什么算互锁」和「为什么没有关键路径」讲清，否则用户只看到一条红虚线。 */}
        {selectedEdgeObj.interlockRisk && (
          <div className="gi-fact risk-high_risk">
            <span>必要输入循环 · 互锁风险</span>
            <small>
              两端任务互相把对方的交付当作必要输入，谁都无法先开始；循环内的边暂停参与关键路径计算，
              需由环内各任务负责人协商拆环。
            </small>
          </div>
        )}
        {selectedEdgeObj.necessity === "required" &&
          !selectedEdgeObj.interlockRisk &&
          selectedEdgeObj.onCriticalPath == null && (
            <div className="muted gi-row" title="相关任务缺少完整的开始／截止时间">
              关键路径未计算：系统只确认依赖链，不宣称关键路径。
            </div>
          )}
        <div className="gi-block">
          <div className="gi-block-head">
            <b>当前交付物</b>
            <span>{selectedEdgeObj.ready ? "来源任务已完成" : "来源任务未完成"}</span>
          </div>
          {(selectedEdgeObj.sourceCurrentFiles ?? []).length > 0 ? (
            (selectedEdgeObj.sourceCurrentFiles ?? []).map((f) => (
              <div key={f.fileId} className="gi-file">
                <span title={f.fileName}>
                  <b>{f.fileName}</b>
                  <small>
                    {f.fileTypeLabel}
                    {f.fileSize > 0 ? ` · ${formatFileSize(f.fileSize)}` : ""}
                  </small>
                </span>
                <span>
                  {previewable(f.fileName) && (
                    <Button size="small" onClick={() => openEdgeFile(f.fileId, f.fileName, true)}>
                      预览
                    </Button>
                  )}
                  <Button size="small" onClick={() => openEdgeFile(f.fileId, f.fileName, false)}>
                    下载
                  </Button>
                </span>
              </div>
            ))
          ) : (
            <p className="gi-empty">当前没有已生效内容。</p>
          )}
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
        <div className="muted">关系详情为只读；关系维护从任务详情的「选择输入源」进入。</div>
      </div>
    </aside>
  );

  // #149 任务详情（§8.1）：eyebrow＋标题＋状态/风险徽章、双列属性网格（含参与人、计划时间）、
  // 结构化卡点风险事实卡、输入就绪（点击选中对应边）、当前交付物（预览／下载）、
  // 受影响 O／KR 影响块（CR-17 与所属分开；裁决 F1 只移出任务抽屉，图谱侧保留）。
  const taskInspector = selectedTask != null && inspectorDetail && (
    <aside className="graph-inspector">
      <div className="graph-inspector-head">
        <h2>节点详情</h2>
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
      {/* #177 裁决：面板按任务抽屉「任务概况」重做为只读版——基础信息、输入源、
          交付物、过程文件与外部材料、交付物接收方、当前卡点；空区块不占位、无底部按钮。
          放不下的一律单行省略、悬停 title 看全文，不换行撑高（#128 口径保留）。 */}
      <div className="graph-inspector-body">
        <div className="gi-title">
          <span className="gi-eyebrow">任务节点 · {inspectorDetail.task.code}</span>
          <h2 title={inspectorDetail.task.name}>{inspectorDetail.task.name}</h2>
          <span className="gi-badge">{inspectorDetail.task.statusLabel}</span>
          {(() => {
            const rb = taskRiskBadge(inspectorDetail.task.id);
            return rb ? <span className={`gi-badge risk-${rb.level}`}>{rb.label}</span> : null;
          })()}
        </div>
        <div className="gi-grid">
          <div
            className="gi-prop"
            title={`${inspectorDetail.objectiveTitle} / ${inspectorDetail.krDescription}`}
          >
            <span>所属 O / KR</span>
            <strong>
              {inspectorDetail.objectiveTitle} / {inspectorDetail.krDescription}
            </strong>
          </div>
          <div className="gi-prop" title={inspectorDetail.task.ownerName}>
            <span>负责人</span>
            <strong>{inspectorDetail.task.ownerName}</strong>
          </div>
          {(inspectorDetail.task.participants ?? []).length > 0 && (
            <div
              className="gi-prop"
              title={(inspectorDetail.task.participants ?? []).map((p) => p.displayName).join("、")}
            >
              <span>参与人</span>
              <strong>
                {(inspectorDetail.task.participants ?? []).map((p) => p.displayName).join("、")}
              </strong>
            </div>
          )}
          <div className="gi-prop">
            <span>周期</span>
            <strong>
              {inspectorDetail.task.startDate} — {inspectorDetail.task.endDate}
            </strong>
          </div>
          {inspectorDetail.task.progress != null && (
            <div className="gi-prop">
              <span>进度</span>
              <strong>{inspectorDetail.task.progress}%</strong>
            </div>
          )}
          {inspectorDetail.task.completionCriteria && (
            <div className="gi-prop" title={inspectorDetail.task.completionCriteria}>
              <span>量化标准</span>
              <strong>{inspectorDetail.task.completionCriteria}</strong>
            </div>
          )}
          {inspectorDetail.task.description && (
            <div className="gi-prop" title={inspectorDetail.task.description}>
              <span>任务说明</span>
              <strong>{inspectorDetail.task.description}</strong>
            </div>
          )}
        </div>
        {inspectorDetail.inputs.length > 0 && (
          <div className="gi-block">
            <div className="gi-block-head">
              <b>输入源</b>
              <span>
                {inspectorDetail.inputs.filter((e) => e.ready).length}/{inspectorDetail.inputs.length} 已就绪
              </span>
            </div>
            {inspectorDetail.inputs.map((e) => {
              const src = `${e.sourceTaskCode ?? ""} ${e.sourceTaskName ?? ""}`;
              return (
                <button
                  key={e.id}
                  type="button"
                  className="gi-mini"
                  title={`${src} → ${inspectorDetail.task.code}`}
                  onClick={() => {
                    // 点击输入行选中对应边（原型 cp-relation-mini → cp-edge）。
                    setSelectedTask(null);
                    setImpactMode(false);
                    setSelectedNode(null);
                    setSelectedEdge(e.id);
                  }}
                >
                  <span>
                    <b>
                      {src} → {inspectorDetail.task.code}
                    </b>
                    <small>{e.necessityLabel}</small>
                  </span>
                  {/* 裁决 15（#185）：参考未就绪改中性灰。 */}
                  <span
                    className={`gi-badge ${
                      e.ready ? "ready" : e.necessity === "reference" ? "" : "risk-warning"
                    }`}
                  >
                    {e.ready ? "已就绪" : "未就绪"}
                  </span>
                </button>
              );
            })}
          </div>
        )}
        {inspectorDetail.deliverables.length > 0 && (
          <div className="gi-block">
            <div className="gi-block-head">
              <b>交付物</b>
              <span>{inspectorDetail.deliverables.length} 项</span>
            </div>
            {inspectorDetail.deliverables.map((d) => (
              <div key={d.id} className="gi-file">
                <span title={`${d.name} · ${d.contentStateLabel}`}>
                  <b>{d.name}</b>
                  <small>
                    {d.contentStateLabel}
                    {d.current?.fileSize ? ` · ${formatFileSize(d.current.fileSize)}` : ""}
                  </small>
                </span>
                {d.current && (
                  <span>
                    {previewable(d.current.fileName) && (
                      <Button size="small" onClick={() => openEdgeFile(d.current!.id, d.current!.fileName, true)}>
                        预览
                      </Button>
                    )}
                    <Button size="small" onClick={() => openEdgeFile(d.current!.id, d.current!.fileName, false)}>
                      下载
                    </Button>
                  </span>
                )}
              </div>
            ))}
          </div>
        )}
        {(inspectorDetail.files ?? []).length > 0 && (
          <div className="gi-block">
            <div className="gi-block-head">
              <b>过程文件与外部材料</b>
              <span>{(inspectorDetail.files ?? []).length} 项</span>
            </div>
            {(inspectorDetail.files ?? []).map((f) => (
              <div key={f.id} className="gi-file">
                <span title={f.fileName}>
                  <b>{f.fileName}</b>
                  <small>
                    {f.kindLabel}
                    {f.fileSize ? ` · ${formatFileSize(f.fileSize)}` : ""}
                  </small>
                </span>
              </div>
            ))}
          </div>
        )}
        {inspectorDetail.receipts.length > 0 && (
          <div className="gi-block">
            <div className="gi-block-head">
              <b>交付物接收方</b>
              <span>
                {inspectorDetail.receipts.filter((r) => r.confirmedAt).length}/
                {inspectorDetail.receipts.length} 已接收
              </span>
            </div>
            {inspectorDetail.receipts.map((r) => (
              <div key={r.id} className="gi-file">
                <span title={r.displayName}>
                  <b>{r.displayName}</b>
                </span>
                <span className={`gi-badge ${r.confirmedAt ? "ready" : "risk-warning"}`}>
                  {r.confirmedAt ? "已接收" : "待接收"}
                </span>
              </div>
            ))}
          </div>
        )}
        {/* 当前卡点（§8.1）：等级、原因、待行动人、已持续天数、影响。 */}
        {inspectorDetail.blockers.map((b) => (
          <div key={b.key} className={`gi-fact risk-${b.level}`}>
            <span>
              当前卡点 · {b.kindLabel}
              {b.levelLabel ? ` · ${b.levelLabel}` : ""}
            </span>
            <b>{b.reason}</b>
            <small>
              待行动人：{b.actionOwnerNames.join("、") || "—"} · 已持续 {blockerDays(b.since)} 天
            </small>
            {b.impactNote && <small>{b.impactNote}</small>}
          </div>
        ))}
      </div>
    </aside>
  );

  // #149 O 节点详情（§8.1：O 说明、下属 KR、总体风险）。
  const selectedO = selectedNode?.kind === "o" ? objectives.find((o) => o.id === selectedNode.id) : null;
  const oInspector = selectedO && (
    <aside className="graph-inspector">
      <div className="graph-inspector-head">
        <h2>节点详情</h2>
        <button type="button" aria-label="关闭详情" onClick={() => setSelectedNode(null)}>
          ✕
        </button>
      </div>
      <div className="graph-inspector-body">
        <div className="gi-title">
          <span className="gi-eyebrow">目标 O · {selectedO.code}</span>
          <h2 title={selectedO.title}>{selectedO.title}</h2>
          <span
            className={`gi-badge ${selectedO.riskLevel !== "normal" ? `risk-${selectedO.riskLevel}` : ""}`}
          >
            {selectedO.riskLevelLabel}
          </span>
        </div>
        <div className="gi-grid">
          <div className="gi-prop">
            <span>下属 KR</span>
            <strong>{selectedO.keyResults.length} 个</strong>
          </div>
          <div className="gi-prop">
            <span>预警／高风险 KR</span>
            <strong>{selectedO.keyResults.filter((k) => k.riskLevel !== "normal").length} 个</strong>
          </div>
        </div>
        {selectedO.riskNote && (
          <div className={`gi-fact risk-${selectedO.riskLevel}`}>
            <span>{selectedO.riskLevelLabel}原因</span>
            <b>{selectedO.riskNote}</b>
          </div>
        )}
        <div className="gi-block">
          <div className="gi-block-head">
            <b>目标说明</b>
          </div>
          {selectedO.description ? <div>{selectedO.description}</div> : <p className="gi-empty">未填写目标说明。</p>}
        </div>
      </div>
    </aside>
  );

  // #149 KR 节点详情（§8.1：负责人、任务数量、卡点数量、风险原因）。
  const selectedKr = selectedNode?.kind === "kr" ? krById.get(selectedNode.id) : null;
  const krInspector = selectedKr && (
    <aside className="graph-inspector">
      <div className="graph-inspector-head">
        <h2>节点详情</h2>
        <button type="button" aria-label="关闭详情" onClick={() => setSelectedNode(null)}>
          ✕
        </button>
      </div>
      <div className="graph-inspector-body">
        <div className="gi-title">
          <span className="gi-eyebrow">KR 节点 · {selectedKr.code}</span>
          <h2 title={selectedKr.description}>{selectedKr.description}</h2>
          <span
            className={`gi-badge ${selectedKr.riskLevel !== "normal" ? `risk-${selectedKr.riskLevel}` : ""}`}
          >
            {selectedKr.riskLevelLabel}
          </span>
        </div>
        {/* 裁决 12（#183）：KR 无负责人与周期，改示创建人与创建时间。 */}
        <div className="gi-grid">
          <div className="gi-prop" title={selectedKr.createdByName}>
            <span>创建人</span>
            <strong>{selectedKr.createdByName ?? "—"}</strong>
          </div>
          <div className="gi-prop">
            <span>任务数量</span>
            <strong>{selectedKr.taskCount ?? 0} 项</strong>
          </div>
          <div className="gi-prop">
            <span>卡点数量</span>
            <strong>{selectedKr.openBlockerCount ?? 0} 项</strong>
          </div>
          <div className="gi-prop">
            <span>创建时间</span>
            <strong>{selectedKr.createdAt ? selectedKr.createdAt.slice(0, 10) : "—"}</strong>
          </div>
        </div>
        {selectedKr.riskNote && (
          <div className={`gi-fact risk-${selectedKr.riskLevel}`}>
            <span>{selectedKr.riskLevelLabel}原因</span>
            <b>{selectedKr.riskNote}</b>
            {selectedKr.topBlocker && (
              <small>
                {selectedKr.topBlocker.taskCode} {selectedKr.topBlocker.kindLabel}：{selectedKr.topBlocker.summary}
              </small>
            )}
          </div>
        )}
        <div className="gi-block">
          <div className="gi-block-head">
            <b>量化指标</b>
          </div>
          {selectedKr.metric ? <div>{selectedKr.metric}</div> : <p className="gi-empty">未填写量化指标。</p>}
        </div>
      </div>
    </aside>
  );

  // #159 裁决：成员不再是图谱节点，无成员详情面板；成员提供的输入从任务详情的
  // 输入源或关系列表进入对应关系详情查看。
  const inspector = taskInspector || oInspector || krInspector;

  // CR-21 KR 节点三态：高风险归红态，预警归橙态，其余灰态。
  // riskLevel 本身已由后端读时派生（卡点等级、超期、临期取最大值），前端不再叠加卡点数
  // 重算——否则预警级卡点会被画成红色描边，与文字标签自相矛盾。
  const krVisualState = (krId: number): "normal" | "warning" | "high_risk" =>
    krById.get(krId)?.riskLevel ?? "normal";

  // #153：节点标签截断后悬停显示完整名称（原生 title），各层 O／KR 节点统一。
  const krNodeTitle = (krId: number) => {
    const k = krById.get(krId);
    return k ? `${k.code} ${k.description}` : undefined;
  };

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
  // #146：任务的小地图点位取圆心（id 非空按任务圆换算）。
  const mmDot = (id: number | null, pos: NodePos, cls: string) => {
    const p = id != null ? taskCircle(id, pos) : pos;
    mmDots.push({ x: p.x + p.w / 2, y: p.y + p.h / 2, cls });
  };
  // #159：小地图只画任务↔任务边（成员边不再上画布）。
  const mmEdgeLines = (
    edgeList: DeliverableEdge[],
    positions: Map<number, NodePos>,
  ) => {
    for (const e of edgeList) {
      if (e.sourceTaskId == null) continue;
      const fromBase = positions.get(e.sourceTaskId);
      const toBase = positions.get(e.targetTaskId);
      if (!fromBase || !toBase) continue;
      const from = taskCircle(e.sourceTaskId, fromBase);
      const to = taskCircle(e.targetTaskId, toBase);
      mmLines.push({ x1: from.x + from.w / 2, y1: from.y + from.h / 2, x2: to.x + to.w / 2, y2: to.y + to.h / 2 });
    }
  };
  if (mode.kind === "tree") {
    for (const n of treeNodes) {
      mmDot(null, n.pos, n.kind === "o" ? "objective" : `kr ${krVisualState(n.id)}`);
    }
    for (const l of treeLines) mmLines.push({ x1: l.x1, y1: l.y1, x2: l.x2, y2: l.y2 });
  } else if (mode.kind === "o" && oLayer && oLayerOPos) {
    mmDot(null, oLayerOPos, "objective");
    for (const n of oLayerKrNodes) mmDot(null, n.pos, `kr ${krVisualState(n.id)}`);
    for (const l of oLayerLines) mmLines.push({ x1: l.x1, y1: l.y1, x2: l.x2, y2: l.y2 });
  } else if (mode.kind === "kr" && krLayer && krCenterPos) {
    mmDot(null, krCenterPos, `kr ${krVisualState(krLayer.kr.id)}`);
    for (const t of [...krLayer.inKr, ...krLayer.neighbors]) {
      const pos = krLayer.positions.get(t.id);
      if (pos) mmDot(t.id, pos, taskRiskLevel(t.id));
    }
    mmEdgeLines(krLayer.relevantEdges, krLayer.positions);
  } else if (mode.kind === "focus" && focusLayer) {
    for (const t of focusLayer.visibleTasks) {
      const pos = focusLayer.positions.get(t.id);
      if (pos) mmDot(t.id, pos, taskRiskLevel(t.id));
    }
    mmEdgeLines(focusLayer.relevantEdges, focusLayer.positions);
  } else if (mode.kind === "full" && full) {
    for (const n of fullONodes) mmDot(null, n.pos, "objective");
    for (const n of fullKrNodes) mmDot(null, n.pos, `kr ${krVisualState(n.id)}`);
    for (const t of full.visibleTasks) {
      const pos = full.positions.get(t.id);
      if (pos) mmDot(t.id, pos, taskRiskLevel(t.id));
    }
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
                  // #159：人员筛选按任务负责人／参与人过滤任务节点，候选也覆盖两者。
                  ...[
                    ...new Map(
                      tasks.flatMap((t) => [
                        [t.ownerId, t.ownerName] as [number, string],
                        ...(t.participants ?? []).map(
                          (p) => [p.userId, p.displayName] as [number, string],
                        ),
                      ]),
                    ).entries(),
                  ].map(([id, name]) => ({ value: id, label: name })),
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
                      { value: "type", label: "按必要性" },
                    ]}
                  />
                </label>
              </div>
              <div className="data-table-wrap">
                <table className="data-table">
                  <thead>
                    <tr>
                      <th>来源任务／成员</th>
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
                        <td colSpan={8}>
                          <div className="empty">没有匹配的协作关系</div>
                        </td>
                      </tr>
                    )}
                    {listRows.map((e) => (
                      <tr key={e.id}>
                        <td title={e.sourceTaskName ?? "—"}>{e.sourceTaskName ?? "—"}</td>
                        <td>{e.necessityLabel}</td>
                        {/* 裁决 J1（#142）＋#163：「当前交付物」列显示来源任务当前文件的「类型 · 大小」
                            （类型由服务端派生），多项时显示「N 项」并悬停列出各项「文件名 · 大小」。 */}
                        <td title={edgeCurrentTitle(e)}>
                          {edgeCurrentCell(e) ?? <span className="muted">暂无</span>}
                        </td>
                        <td title={e.targetTaskName}>{e.targetTaskName}</td>
                        <td title={e.sourceOwnerName ?? "—"}>{e.sourceOwnerName ?? "—"}</td>
                        <td>
                          {/* 裁决 15（#185）：参考未就绪改中性灰。 */}
                          <span
                            className={`status-pill ${
                              e.ready ? "completed" : e.necessity === "reference" ? "" : "warning"
                            }`}
                          >
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
                {/* #158（裁决；裁决 15 #185 修订）：进入条件与排序仍只认卡点、风险与必要未就绪
                    （notReadyCount 已只计必要边，参考不作为进入条件；权重＝卡点×3＋必要未就绪）。
                    条目三档显示——有卡点：「KR 编号 · 最高等级卡点」＋卡点数量；
                    无卡点但有上游未就绪提醒（含参考，reminderCount）：「KR 编号 · 上游未就绪」
                    （显示具体提醒名而非「提醒」二字，与边标签异常名同词）；
                    均无（因风险等级进入）：仅「KR 编号」。 */}
                {/* #154：条目列表区独立滚动，底部「回到 O／KR 层级树」不随列表滚走。 */}
                <div className="risk-queue-list">
                {(() => {
                  const weight = (k: (typeof krList)[number]) =>
                    (k.openBlockerCount ?? 0) * 3 + (k.notReadyCount ?? 0);
                  const queue = krList
                    .filter(
                      (k) =>
                        k.riskLevel !== "normal" ||
                        (k.openBlockerCount ?? 0) > 0 ||
                        (k.notReadyCount ?? 0) > 0,
                    )
                    .sort((a, b) => weight(b) - weight(a));
                  if (queue.length === 0) {
                    return (
                      <div className="muted" style={{ padding: 16, fontSize: 12 }}>
                        暂无需要关注的风险
                      </div>
                    );
                  }
                  return queue.map((k) => (
                    <button
                      key={`rk-${k.id}`}
                      type="button"
                      className={`risk-queue-item${mode.kind === "kr" && mode.krId === k.id ? " active" : ""}`}
                      onClick={() => enter({ kind: "kr", krId: k.id })}
                    >
                      <span className="risk-queue-main">
                        <b>{k.code}</b>
                        {(k.openBlockerCount ?? 0) > 0 && k.topBlocker ? (
                          <span> · {k.topBlocker.kindLabel}</span>
                        ) : (k.reminderCount ?? 0) > 0 ? (
                          <span className="muted"> · 上游未就绪</span>
                        ) : null}
                      </span>
                      <span className="risk-queue-counts">
                        {(k.openBlockerCount ?? 0) > 0 && <i>{k.openBlockerCount} 个卡点</i>}
                      </span>
                    </button>
                  ));
                })()}
                </div>
                {/* 原型 cp-lens-foot：语义说明＋回层级树入口。 */}
                <div className="risk-queue-foot">
                  <span>红色仅表示真实阻塞或冲突</span>
                  <button
                    type="button"
                    onClick={() => {
                      setMode({ kind: "tree" });
                      setViewStack([]);
                      setExpanded(new Set());
                      setSelectedTask(null);
                      setSelectedEdge(null);
                      setSelectedNode(null);
                      resetViewport();
                    }}
                  >
                    回到 O／KR 层级树
                  </button>
                </div>
            </aside>
            <div className="graph-shell">
              {/* #149：详情面板在全部层级可见——层级树／O 层选中 O、KR 同样打开节点详情。 */}
              {edgeInspector || inspector}
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
                className={`graph-ops graph-ops-right${edgeInspector || inspector ? " with-inspector" : ""}`}
              >
                {/* #177 裁决：影响路径入口移到画布操作区（选中任务时可用）。 */}
                {selectedTask != null && (
                  <Button
                    size="small"
                    type={impactMode ? "primary" : "default"}
                    onClick={() => setImpactMode((v) => !v)}
                  >
                    {impactMode ? "退出影响路径" : "查看影响路径"}
                  </Button>
                )}
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
              {mode.kind === "o" && oLayer && oLayerOPos ? (
                <>
                  <svg className="graph-svg" width={oLayer.width} height={oLayer.height}>
                    {oLayerLines.map((l, i) => (
                      <path key={i} d={l.d} fill="none" stroke="#b8c4ce" strokeWidth={1.6} opacity={l.dimmed ? 0.2 : 1} />
                    ))}
                  </svg>
                  <div
                    className="gnode gnode-o"
                    style={{ left: oLayerOPos.x, top: oLayerOPos.y, width: oLayerOPos.w, height: oLayerOPos.h }}
                    title={oLayer.o.title}
                    onMouseDown={(ev) => startDrag(`o:${oLayer.o.id}`, ev.clientX, ev.clientY)}
                  >
                    <b>{oLayer.o.title}</b>
                    <small>{oLayerKrNodes.length} 个 KR</small>
                  </div>
                  {oLayerKrNodes.map((n) => (
                    <div
                      key={`okr-${n.id}`}
                      role="button"
                      tabIndex={0}
                      className={`gnode gnode-kr ${n.dimmed ? "dimmed" : ""} ${
                        krVisualState(n.id) !== "normal" ? `risk-${krVisualState(n.id)}` : ""
                      }`}
                      style={{ left: n.pos.x, top: n.pos.y, width: n.pos.w, height: n.pos.h }}
                      title={krNodeTitle(n.id)}
                      onMouseDown={(ev) => startDrag(`kr:${n.id}`, ev.clientX, ev.clientY)}
                      onClick={guardClick(() => enterKr(n.id))}
                      onKeyDown={pressAsClick(() => enterKr(n.id))}
                    >
                      {krNodeContent(n.id)}
                    </div>
                  ))}
                </>
              ) : mode.kind === "tree" ? (
                <>
                  <svg className="graph-svg" width={tree.width} height={tree.height}>
                    {treeLines.map((l, i) => (
                      <path key={i} d={l.d} fill="none" stroke="#b8c4ce" strokeWidth={1.6} opacity={l.dimmed ? 0.2 : 1} />
                    ))}
                  </svg>
                  {treeNodes.map((n) =>
                    n.kind === "o" ? (
                      <div
                        key={n.key}
                        role="button"
                        tabIndex={0}
                        className={`gnode gnode-o ${n.dimmed ? "dimmed" : ""}`}
                        style={{ left: n.pos.x, top: n.pos.y, width: n.pos.w, height: n.pos.h }}
                        title={objectives.find((o) => o.id === n.id)?.title}
                        onMouseDown={(ev) => startDrag(`o:${n.id}`, ev.clientX, ev.clientY)}
                        onClick={guardClick(() => enterO(n.id))}
                        onKeyDown={pressAsClick(() => enterO(n.id))}
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
                        title={krNodeTitle(n.id)}
                        onMouseDown={(ev) => startDrag(`kr:${n.id}`, ev.clientX, ev.clientY)}
                        onClick={guardClick(() => enterKr(n.id))}
                        onKeyDown={pressAsClick(() => enterKr(n.id))}
                      >
                        {krNodeContent(n.id)}
                      </div>
                    ),
                  )}
                </>
              ) : mode.kind === "kr" && krLayer && krCenterPos ? (
                <>
                  <svg className="graph-svg" width={krLayer.width} height={krLayer.height}>
                    {arrowDefs}
                    {/* KR→任务 owns 直线（#148）：灰、无箭头，绘于关系边之下；
                        两端都跟随拖拽（#151：KR 中心节点同样可拖）。 */}
                    {krLayer.inKr.map((t) => {
                      const base = krLayer.positions.get(t.id);
                      if (!base) return null;
                      const p = taskCircle(t.id, base);
                      return (
                        <path
                          key={`owns-${t.id}`}
                          d={`M ${krCenterPos.x + krCenterPos.w / 2} ${krCenterPos.y + krCenterPos.h / 2} L ${
                            p.x + p.w / 2
                          } ${p.y + p.h / 2}`}
                          fill="none"
                          stroke="#b2bdca"
                          strokeWidth={1.7}
                        />
                      );
                    })}
                    {krLayer.relevantEdges.map((e) => {
                      if (e.sourceTaskId == null) return null;
                      const fromBase = krLayer.positions.get(e.sourceTaskId);
                      const toBase = krLayer.positions.get(e.targetTaskId);
                      if (!fromBase || !toBase) return null;
                      const from = taskCircle(e.sourceTaskId, fromBase);
                      const to = taskCircle(e.targetTaskId, toBase);
                      return renderEdge(e, from, to);
                    })}
                  </svg>
                  {/* 中心 KR 节点（#148；CR-21 视觉复用）：已在本层，点它不下钻、只开详情（#149）；
                      空 KR 交给空态提示，不渲染孤零零的中心节点。 */}
                  {(krLayer.inKr.length > 0 || krLayer.neighbors.length > 0) &&
                    (() => {
                      const krId = krLayer.kr.id;
                      const selectKr = () => {
                        setSelectedTask(null);
                        setSelectedEdge(null);
                        setImpactMode(false);
                        setSelectedNode((prev) =>
                          prev?.kind === "kr" && prev.id === krId ? null : { kind: "kr", id: krId },
                        );
                      };
                      return (
                        <div
                          role="button"
                          tabIndex={0}
                          className={`gnode gnode-kr ${
                            krVisualState(krId) !== "normal" ? `risk-${krVisualState(krId)}` : ""
                          } ${selectedNode?.kind === "kr" && selectedNode.id === krId ? "selected" : ""}`}
                          style={{
                            left: krCenterPos.x,
                            top: krCenterPos.y,
                            width: krCenterPos.w,
                            height: krCenterPos.h,
                          }}
                          title={krNodeTitle(krId)}
                          onMouseDown={(ev) => startDrag(`kr:${krId}`, ev.clientX, ev.clientY)}
                          onClick={guardClick(selectKr)}
                          onKeyDown={pressAsClick(selectKr)}
                        >
                          {krNodeContent(krId)}
                        </div>
                      );
                    })()}
                  {[...krLayer.inKr, ...krLayer.neighbors].map((t) => {
                    const pos = krLayer.positions.get(t.id);
                    return pos ? taskNode(t, pos) : null;
                  })}
                </>
              ) : mode.kind === "focus" && focusLayer ? (
                <>
                  <svg className="graph-svg" width={focusLayer.width} height={focusLayer.height}>
                    {arrowDefs}
                    {focusLayer.relevantEdges.map((e) => {
                      if (e.sourceTaskId == null) return null;
                      const fromBase = focusLayer.positions.get(e.sourceTaskId);
                      const toBase = focusLayer.positions.get(e.targetTaskId);
                      if (!fromBase || !toBase) return null;
                      const from = taskCircle(e.sourceTaskId, fromBase);
                      const to = taskCircle(e.targetTaskId, toBase);
                      return renderEdge(e, from, to);
                    })}
                  </svg>
                  {focusLayer.visibleTasks.map((t) => {
                    const pos = focusLayer.positions.get(t.id);
                    return pos ? taskNode(t, pos) : null;
                  })}
                </>
              ) : mode.kind === "full" && full ? (
                <>
                    <svg className="graph-svg" width={full.width} height={full.height}>
                      {arrowDefs}
                      {/* O→KR→任务层级连线（#123）：owns 边灰色无箭头，绘于关系边之下；
                          路径按拖拽偏移后的两端位置重算（#151）。 */}
                      {full.ownsLinks.map((l, i) => {
                        const d = fullOwnsD(l);
                        if (!d) return null;
                        const dim =
                          hasFilter &&
                          (l.taskId != null
                            ? !taskMatchesFilter(taskById.get(l.taskId)!)
                            : !tasks.some((t) => t.keyResultId === l.krId && taskMatchesFilter(t)));
                        return (
                          <path
                            key={`owns-${i}`}
                            d={d}
                            fill="none"
                            stroke="#b8c4ce"
                            strokeWidth={1.6}
                            opacity={dim ? 0.15 : 1}
                          />
                        );
                      })}
                      {full.visibleEdges.map((e) => {
                        if (e.sourceTaskId == null) return null;
                        const fromBase = full.positions.get(e.sourceTaskId);
                        const toBase = full.positions.get(e.targetTaskId);
                        const from = fromBase ? taskCircle(e.sourceTaskId, fromBase) : fromBase;
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
                    {fullONodes.map((n) => {
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
                          title={n.title}
                          onMouseDown={(ev) => startDrag(`o:${n.id}`, ev.clientX, ev.clientY)}
                          onClick={guardClick(() => enterO(n.id))}
                          onKeyDown={pressAsClick(() => enterO(n.id))}
                        >
                          <b>{n.title}</b>
                          <small>{n.krCount} 个 KR</small>
                        </div>
                      );
                    })}
                    {fullKrNodes.map((n) => {
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
                          title={krNodeTitle(n.id)}
                          onMouseDown={(ev) => startDrag(`kr:${n.id}`, ev.clientX, ev.clientY)}
                          onClick={guardClick(() => enterKr(n.id))}
                          onKeyDown={pressAsClick(() => enterKr(n.id))}
                        >
                          {krNodeContent(n.id)}
                        </div>
                      );
                    })}
                    {full.visibleTasks.slice(0, renderBudget).map((t) => {
                      const pos = full.positions.get(t.id);
                      return pos ? taskNode(t, pos) : null;
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
                {/* #173 裁决：边图例只留三类——必要（实线）、参考（虚线）、互锁异常（红虚线常驻）。
                    裁决 15（#185）：补必要性语义解释。 */}
                <span><i className="lg-line" />必要</span>
                <span><i className="lg-feedback" />参考</span>
                <span><i className="lg-interlock" />互锁</span>
                <span className="muted">参考输入不参与风险识别，仅作提示</span>
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
            // 从关系列表进入时也要切回图谱视图，否则选中态没有画布可落。
            setViewMode("graph");
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

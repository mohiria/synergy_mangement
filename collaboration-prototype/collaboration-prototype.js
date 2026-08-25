(function () {
  "use strict";

  // PROTOTYPE — Winning collaboration workspace on #graph: O→KR hierarchy first, real task relations only after drill-down.
  const STORAGE_KEY = "collaboration-prototype-v4.4-prd-r3";
  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const esc = (value) => String(value ?? "").replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[char]));
  const state = {
    view: "graph",
    mode: "aggregate",
    focusId: "",
    selection: null,
    history: [],
    query: "",
    searchOpen: false,
    displayMode: "dim",
    zoom: 1,
    pan: { x: 0, y: 0 },
    fixed: {},
    sort: "planned",
    filters: { o: "all", kr: "all", person: "all", risk: "all", relation: "all", includeCompleted: false },
    suppressClick: "",
  };

  const icons = {
    search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>',
    back: '<path d="m15 18-6-6 6-6"/>',
    expand: '<path d="M8 3H3v5M16 3h5v5M8 21H3v-5M16 21h5v-5"/>',
    fit: '<path d="M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5"/>',
    reset: '<path d="M4 12a8 8 0 1 0 2-5.3L4 9M4 4v5h5"/>',
    close: '<path d="m6 6 12 12M18 6 6 18"/>',
    chevron: '<path d="m9 6 6 6-6 6"/>',
    download: '<path d="M12 3v12m0 0 5-5m-5 5-5-5M4 20h16"/>',
    graph: '<circle cx="5" cy="12" r="2.5"/><circle cx="19" cy="5" r="2.5"/><circle cx="19" cy="19" r="2.5"/><path d="m7.2 10.9 9.5-4.8M7.2 13.1l9.5 4.8"/>',
    list: '<path d="M8 6h13M8 12h13M8 18h13"/><circle cx="4" cy="6" r="1"/><circle cx="4" cy="12" r="1"/><circle cx="4" cy="18" r="1"/>',
    layers: '<path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 12 9 5 9-5M3 16l9 5 9-5"/>',
    filter: '<path d="M3 5h18l-7 8v6l-4 2v-8L3 5Z"/>',
    lock: '<rect x="5" y="10" width="14" height="10" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/>',
    alert: '<path d="M12 3 2.8 20h18.4L12 3Z"/><path d="M12 9v5M12 17.5v.1"/>',
  };
  function icon(name) {
    return `<svg class="cp-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name] || icons.graph}</svg>`;
  }

  function getData() {
    try { return JSON.parse(localStorage.getItem(STORAGE_KEY)) || window.PROTOTYPE_SEED; }
    catch (_) { return window.PROTOTYPE_SEED; }
  }
  const member = (data, id) => data.members.find((item) => item.id === id) || { id, displayName: id, role: "项目成员", team: "" };
  const task = (data, id) => data.tasks.find((item) => item.id === id);
  const kr = (data, id) => data.krs.find((item) => item.id === id);
  const objective = (data, id) => data.objectives.find((item) => item.id === id);
  const risk = (data, id) => data.risks.find((item) => item.id === id);
  const relation = (data, id) => data.relations.find((item) => item.id === id);
  const currentDeliverable = (data, rel) => data.deliverables.find((item) => item.relationId === rel.id && item.state === "已生效");
  const candidateDeliverable = (data, rel) => data.deliverables.find((item) => item.relationId === rel.id && item.state === "审核中");
  const nameOf = (data, id) => task(data, id)?.title || kr(data, id)?.title || objective(data, id)?.title || member(data, id)?.displayName || risk(data, id)?.reason || id;
  const relationType = (rel) => rel.type === "interlock" ? "hard" : rel.type;
  const relationTypeLabel = (rel) => ({ hard: "硬前置交付", input: "信息输入", result: "正式成果接收", feedback: "迭代／反馈" })[relationType(rel)] || "信息输入";
  const relationReadiness = (data, rel) => {
    const current = currentDeliverable(data, rel), candidate = candidateDeliverable(data, rel);
    if (current && candidate) return { key: "updating", label: "有更新审核中", ready: true };
    if (current) return { key: "ready", label: "已就绪", ready: true };
    if (candidate) return { key: "reviewing", label: "首次内容审核中", ready: false };
    if (rel.state === "已就绪") return { key: "ready", label: "已就绪", ready: true };
    if (Number(rel.id.replace(/\D/g, "")) % 4 === 0) return { key: "overdue", label: "已逾期", ready: false };
    return { key: "waiting", label: "等待提供", ready: false };
  };
  const riskKeyForTask = (data, item) => {
    const itemRisk = data.risks.find((entry) => entry.taskId === item.id);
    if (itemRisk?.level === "高风险") return "risk";
    if (itemRisk?.level === "预警") return "warning";
    return "normal";
  };
  const taskStatusKey = (value) => {
    if (/完成|通过|生效|已提供|已接收/.test(value)) return "done";
    if (/进行中/.test(value)) return "active";
    if (/审核|审批|终审/.test(value)) return "reviewing";
    if (/等待|逾期/.test(value)) return "warning";
    return "neutral";
  };
  const riskLabel = (key) => ({ risk: "高风险", warning: "预警", normal: "正常" })[key] || "正常";
  const statusBadge = (label, key = "") => `<span class="cp-badge ${key}"><i></i>${esc(label)}</span>`;

  function matchesTask(data, item) {
    const itemKr = kr(data, item.krId), f = state.filters;
    if (!f.includeCompleted && item.status === "已完成") return false;
    if (f.o !== "all" && itemKr?.objectiveId !== f.o) return false;
    if (f.kr !== "all" && item.krId !== f.kr) return false;
    if (f.person !== "all" && item.owner !== f.person && !item.participants.includes(f.person)) return false;
    const rk = riskKeyForTask(data, item);
    if (f.risk !== "all" && rk !== f.risk) return false;
    return true;
  }
  function matchesRelation(data, rel) {
    const f = state.filters, from = task(data, rel.from), to = task(data, rel.to);
    if (f.relation !== "all" && relationType(rel) !== f.relation && !(f.relation === "interlock" && rel.type === "interlock")) return false;
    if (!from || !to) return true;
    return matchesTask(data, from) || matchesTask(data, to);
  }

  function snapshot() {
    return { mode: state.mode, focusId: state.focusId, selection: state.selection ? { ...state.selection } : null, zoom: state.zoom, pan: { ...state.pan } };
  }
  function remember() {
    state.history.push(snapshot());
    if (state.history.length > 20) state.history.shift();
  }
  function restore(previous) {
    if (!previous) return;
    state.mode = previous.mode; state.focusId = previous.focusId; state.selection = previous.selection; state.zoom = previous.zoom; state.pan = previous.pan;
  }

  function visibleRelations(data) {
    return data.relations
      .filter((rel) => state.filters.includeCompleted || (task(data, rel.from)?.status !== "已完成" && task(data, rel.to)?.status !== "已完成"))
      .map((rel) => ({ ...rel, normalizedType: relationType(rel), readiness: relationReadiness(data, rel), matched: matchesRelation(data, rel) }));
  }
  function modelNode(data, id, type, x, y, extra = {}) {
    const fixed = state.fixed[id], itemTask = type === "task" ? task(data, id) : null;
    return { id, type, x: fixed?.x ?? x, y: fixed?.y ?? y, label: nameOf(data, id), matched: true, completed: itemTask?.status === "已完成", ...extra };
  }
  function applyDisplayMode(model) {
    const scopedNodes = state.filters.includeCompleted ? model.nodes : model.nodes.filter((node) => !node.completed);
    const scopedIds = new Set(scopedNodes.map((node) => node.id));
    const scopedModel = { nodes: scopedNodes, edges: model.edges.filter((edge) => scopedIds.has(edge.from) && scopedIds.has(edge.to)) };
    if (state.displayMode !== "hide") return scopedModel;
    const keep = new Set(scopedModel.nodes.filter((node) => node.matched !== false || node.type === "objective").map((node) => node.id));
    scopedModel.edges.forEach((edge) => { if (edge.matched !== false && keep.has(edge.from) && keep.has(edge.to)) { keep.add(edge.from); keep.add(edge.to); } });
    return {
      nodes: scopedModel.nodes.filter((node) => keep.has(node.id)),
      edges: scopedModel.edges.filter((edge) => keep.has(edge.from) && keep.has(edge.to) && edge.matched !== false),
    };
  }

  function krRiskReason(data, itemKr) {
    const ids = new Set(data.tasks.filter((item) => item.krId === itemKr.id).map((item) => item.id));
    return data.risks.find((item) => ids.has(item.taskId))?.reason || "";
  }

  function aggregateModel(data) {
    const nodes = [], edges = [], centers = { O1: [210, 120], O2: [650, 120], O3: [1090, 120] };
    data.objectives.forEach((o, oi) => {
      const [cx, cy] = centers[o.id] || [210 + oi * 440, 120];
      const childKrs = data.krs.filter((item) => item.objectiveId === o.id);
      const matched = childKrs.some((item) => data.tasks.some((t) => t.krId === item.id && matchesTask(data, t)));
      nodes.push(modelNode(data, o.id, "objective", cx, cy, { matched, subtitle: o.note }));
      childKrs.forEach((item, index) => {
        const columns = childKrs.length === 1 ? 1 : 2;
        const row = Math.floor(index / columns), column = index % columns;
        const x = columns === 1 ? cx : cx + (column === 0 ? -105 : 105);
        const y = 335 + row * 165;
        const tasks = data.tasks.filter((t) => t.krId === item.id);
        const itemMatched = tasks.some((t) => matchesTask(data, t));
        const blockerCount = data.risks.filter((r) => tasks.some((t) => t.id === r.taskId)).length;
        nodes.push(modelNode(data, item.id, "kr", x, y, { matched: itemMatched, risk: item.risk, count: tasks.length, blockerCount, riskReason: krRiskReason(data, item), subtitle: item.title }));
        edges.push({ id: `owns-${item.id}`, from: o.id, to: item.id, kind: "owns", type: "owns", matched: itemMatched });
      });
    });
    return applyDisplayMode({ nodes, edges });
  }

  function focusModel(data, relations) {
    const focusId = state.focusId || state.selection?.id || "KR1", nodes = [], edges = [];
    const addRelationEdges = (ids) => relations.filter((rel) => ids.has(rel.from) && ids.has(rel.to)).forEach((rel) => edges.push({ ...rel, kind: "relation" }));
    const focusO = objective(data, focusId), focusKr = kr(data, focusId), focusTask = task(data, focusId), focusRisk = risk(data, focusId);
    const focusMember = data.members.find((item) => item.id === focusId);
    if (focusO) {
      nodes.push(modelNode(data, focusO.id, "objective", 620, 155, { subtitle: focusO.note }));
      const children = data.krs.filter((item) => item.objectiveId === focusO.id);
      children.forEach((item, index) => {
        const x = 270 + index * (700 / Math.max(children.length - 1, 1));
        const tasks = data.tasks.filter((t) => t.krId === item.id);
        nodes.push(modelNode(data, item.id, "kr", x, 390, { risk: item.risk, count: tasks.length, blockerCount: data.risks.filter((r) => tasks.some((t) => t.id === r.taskId)).length, riskReason: krRiskReason(data, item), subtitle: item.title, matched: tasks.some((t) => matchesTask(data, t)) }));
        edges.push({ id: `owns-${item.id}`, from: focusO.id, to: item.id, kind: "owns", type: "owns", matched: true });
      });
      return applyDisplayMode({ nodes, edges });
    }
    if (focusKr) {
      const ownTasks = data.tasks.filter((item) => item.krId === focusKr.id);
      const ownIds = new Set(ownTasks.map((item) => item.id));
      const neighborIds = new Set();
      relations.forEach((rel) => { if (ownIds.has(rel.from) && !ownIds.has(rel.to)) neighborIds.add(rel.to); if (ownIds.has(rel.to) && !ownIds.has(rel.from)) neighborIds.add(rel.from); });
      nodes.push(modelNode(data, focusKr.id, "kr", 620, 330, { risk: focusKr.risk, count: ownTasks.length, blockerCount: data.risks.filter((r) => ownIds.has(r.taskId)).length, riskReason: krRiskReason(data, focusKr), subtitle: focusKr.title }));
      ownTasks.forEach((item, index) => {
        const angle = -Math.PI / 2 + index * Math.PI * 2 / Math.max(ownTasks.length, 4);
        nodes.push(modelNode(data, item.id, "task", 620 + Math.cos(angle) * 205, 330 + Math.sin(angle) * 185, { risk: riskKeyForTask(data, item), subtitle: item.title, matched: matchesTask(data, item) }));
        edges.push({ id: `owns-${item.id}`, from: focusKr.id, to: item.id, kind: "owns", type: "owns", matched: matchesTask(data, item) });
      });
      [...neighborIds].slice(0, 8).forEach((id, index) => {
        const item = task(data, id); if (!item) return;
        const side = index % 2 ? 1 : -1;
        nodes.push(modelNode(data, id, "task", side < 0 ? 110 : 1130, 130 + Math.floor(index / 2) * 145, { risk: riskKeyForTask(data, item), subtitle: item.title, external: true, matched: matchesTask(data, item) }));
      });
      addRelationEdges(new Set(nodes.map((node) => node.id)));
      return applyDisplayMode({ nodes, edges });
    }
    let centerTask = focusTask;
    if (focusRisk) centerTask = task(data, focusRisk.taskId);
    if (focusMember) {
      const request = data.inputRequests.find((item) => item.provider === focusMember.id);
      centerTask = task(data, request?.taskId);
    }
    if (centerTask) {
      const connectedIds = new Set([centerTask.id]);
      relations.forEach((rel) => { if (rel.from === centerTask.id) connectedIds.add(rel.to); if (rel.to === centerTask.id) connectedIds.add(rel.from); });
      const connectedTasks = [...connectedIds].map((id) => task(data, id)).filter(Boolean);
      connectedTasks.forEach((item, index) => {
        const angle = -Math.PI / 2 + (index - 1) * Math.PI * 2 / Math.max(connectedTasks.length - 1, 4);
        const isCenter = item.id === centerTask.id;
        nodes.push(modelNode(data, item.id, "task", isCenter ? 620 : 620 + Math.cos(angle) * 250, isCenter ? 350 : 350 + Math.sin(angle) * 220, { risk: riskKeyForTask(data, item), subtitle: item.title, focus: isCenter, matched: matchesTask(data, item) }));
      });
      addRelationEdges(new Set(connectedTasks.map((item) => item.id)));
      const requests = data.inputRequests.filter((item) => item.taskId === centerTask.id);
      requests.forEach((request, index) => {
        const provider = member(data, request.provider);
        if (!nodes.some((node) => node.id === provider.id)) nodes.push(modelNode(data, provider.id, "person", 120, 265 + index * 130, { subtitle: `${provider.role} · ${request.inputName}`, matched: state.filters.person === "all" || state.filters.person === provider.id }));
        edges.push({ id: `input-${request.id}`, from: provider.id, to: centerTask.id, kind: "member-input", type: "input", label: request.inputName, readiness: { key: request.state === "已提供" ? "ready" : "waiting", label: request.state, ready: request.state === "已提供" }, matched: true });
      });
    }
    return applyDisplayMode({ nodes, edges });
  }

  function fullModel(data, relations) {
    const nodes = [], edges = [];
    const objectiveX = { O1: 230, O2: 650, O3: 1070 };
    data.objectives.forEach((o) => nodes.push(modelNode(data, o.id, "objective", objectiveX[o.id] || 650, 70, { subtitle: o.note })));
    data.krs.forEach((item, index) => {
      const oi = data.objectives.findIndex((o) => o.id === item.objectiveId), siblings = data.krs.filter((k) => k.objectiveId === item.objectiveId), si = siblings.findIndex((k) => k.id === item.id);
      const x = 90 + oi * 420 + (si % 2) * 190, y = 200 + Math.floor(si / 2) * 270;
      const tasks = data.tasks.filter((t) => t.krId === item.id);
      nodes.push(modelNode(data, item.id, "kr", x + 85, y, { risk: item.risk, count: tasks.length, blockerCount: data.risks.filter((r) => tasks.some((t) => t.id === r.taskId)).length, riskReason: krRiskReason(data, item), subtitle: item.title, matched: tasks.some((t) => matchesTask(data, t)) }));
      edges.push({ id: `owns-${item.id}`, from: item.objectiveId, to: item.id, kind: "owns", type: "owns", matched: true });
      tasks.forEach((t, ti) => {
        nodes.push(modelNode(data, t.id, "task", x + (ti % 2) * 135, y + 92 + Math.floor(ti / 2) * 92, { risk: riskKeyForTask(data, t), subtitle: t.title, matched: matchesTask(data, t) }));
        edges.push({ id: `owns-${t.id}`, from: item.id, to: t.id, kind: "owns", type: "owns", matched: matchesTask(data, t) });
      });
    });
    relations.forEach((rel) => edges.push({ ...rel, kind: "relation" }));
    const relevantMembers = new Map();
    data.inputRequests.forEach((request) => relevantMembers.set(request.provider, request));
    [...relevantMembers].slice(0, 8).forEach(([id, request], index) => {
      const item = member(data, id);
      nodes.push(modelNode(data, id, "person", 70 + index * 160, 1040, { subtitle: `${item.role} · ${request.inputName}`, matched: state.filters.person === "all" || state.filters.person === id }));
      edges.push({ id: `member-${id}-${request.taskId}`, from: id, to: request.taskId, kind: "member-input", type: "input", label: request.inputName, matched: true, readiness: { key: "waiting", label: "待行动", ready: false } });
    });
    return applyDisplayMode({ nodes, edges });
  }

  function graphModel(data) {
    const relations = visibleRelations(data);
    if (state.mode === "aggregate") return aggregateModel(data);
    if (state.mode === "full") return fullModel(data, relations);
    return focusModel(data, relations);
  }

  function nodeMarkup(node) {
    const selected = state.selection?.kind === "node" && state.selection.id === node.id;
    const dim = node.matched === false ? " dimmed" : "";
    const external = node.external ? " external" : "";
    const cls = `cp-node ${node.type} ${node.risk || ""}${selected ? " selected" : ""}${node.endpoint ? " endpoint" : ""}${dim}${external}`;
    if (node.type === "objective") return `<g class="${cls}" data-cp-node="${node.id}" data-action="cp-node" data-id="${node.id}" transform="translate(${node.x} ${node.y})"><rect x="-64" y="-31" width="128" height="62" rx="18"/><text class="node-code" text-anchor="middle" y="-4">${node.id}</text><text class="node-caption" text-anchor="middle" y="16">目标中心</text></g>`;
    if (node.type === "kr") {
      const riskText = `${riskLabel(node.risk)} · ${node.blockerCount || 0} 卡点`;
      return `<g class="${cls}" data-cp-node="${node.id}" data-action="cp-node" data-id="${node.id}" transform="translate(${node.x} ${node.y})"><rect x="-82" y="-50" width="164" height="100" rx="12"/><text class="node-code" x="-65" y="-20">${node.id}</text><text class="node-count" x="65" y="-20" text-anchor="end">${node.count || 0} 任务</text><text class="node-caption" x="-65" y="2">${esc(node.subtitle?.slice(0, 12) || "")}</text><text class="node-risk-line ${node.risk || "normal"}" x="-65" y="25">${esc(riskText)}</text>${node.riskReason ? `<text class="node-risk-reason ${node.risk || "normal"}" x="-65" y="43">${esc(node.riskReason.slice(0, 9))}</text>` : ""}<title>${esc(node.riskReason || `${riskLabel(node.risk)}，${node.blockerCount || 0} 个卡点`)}</title></g>`;
    }
    if (node.type === "person") return `<g class="${cls}" data-cp-node="${node.id}" data-action="cp-node" data-id="${node.id}" transform="translate(${node.x} ${node.y})"><circle r="29"/><text class="node-code" text-anchor="middle" y="4">${esc(node.label.slice(0, 2))}</text><text class="node-caption" text-anchor="middle" y="48">${esc(node.subtitle?.slice(0, 12) || "")}</text></g>`;
    if (node.type === "risk") return `<g class="${cls}" data-cp-node="${node.id}" data-action="cp-node" data-id="${node.id}" transform="translate(${node.x} ${node.y})"><path d="M0-34 34 0 0 34-34 0Z"/><text class="node-code" text-anchor="middle" y="4">${node.id}</text><text class="node-caption" text-anchor="middle" y="53">${esc(node.subtitle?.slice(0, 13) || "")}</text></g>`;
    const riskMarker = ["warning", "risk"].includes(node.risk) ? `<g class="node-risk-marker ${node.risk}" transform="translate(${node.focus ? 25 : 21} ${node.focus ? -25 : -21})"><circle r="9"/><text text-anchor="middle" y="4">!</text></g>` : "";
    return `<g class="${cls}" data-cp-node="${node.id}" data-action="cp-node" data-id="${node.id}" transform="translate(${node.x} ${node.y})"><circle r="${node.focus ? 35 : 29}"/><text class="node-code" text-anchor="middle" y="4">${node.id}</text><text class="node-caption" text-anchor="middle" y="48">${esc(node.subtitle?.slice(0, 13) || "")}</text>${riskMarker}</g>`;
  }

  function edgePath(edge, from, to, model) {
    if (edge.kind === "owns" && from.type === "objective" && to.type === "kr") {
      const branchY = from.y + (to.y - from.y) * .46;
      return `M ${from.x} ${from.y + 31} V ${branchY} H ${to.x} V ${to.y - 50}`;
    }
    const reverse = model.edges.some((other) => other.id !== edge.id && other.from === edge.to && other.to === edge.from && other.kind !== "owns");
    if (!reverse) return `M ${from.x} ${from.y} L ${to.x} ${to.y}`;
    const dx = to.x - from.x, dy = to.y - from.y, length = Math.max(Math.hypot(dx, dy), 1), sign = edge.from < edge.to ? 1 : -1;
    const mx = (from.x + to.x) / 2 - dy / length * 34 * sign, my = (from.y + to.y) / 2 + dx / length * 34 * sign;
    return `M ${from.x} ${from.y} Q ${mx} ${my} ${to.x} ${to.y}`;
  }
  function edgeMarkup(edge, model, nodeById) {
    const from = nodeById[edge.from], to = nodeById[edge.to]; if (!from || !to) return "";
    const selected = state.selection?.kind === "edge" && state.selection.id === edge.id;
    const endpointSelected = state.selection?.kind === "edge" && [edge.from, edge.to].includes(state.selection.id);
    const dim = edge.matched === false ? " dimmed" : "";
    const critical = edge.pathId && edge.normalizedType === "hard" && edge.type !== "interlock" ? " critical" : "";
    const interlock = edge.type === "interlock" || edge.interlock ? " interlock" : "";
    const anomaly = edge.kind !== "owns" && (edge.type === "interlock" || edge.readiness?.ready === false) ? " anomaly" : "";
    const cls = `cp-edge ${edge.kind || "relation"} ${edge.normalizedType || edge.type || ""}${selected ? " selected" : ""}${edge.pathHighlighted ? " path-highlighted" : ""}${endpointSelected ? " endpoint" : ""}${critical}${interlock}${anomaly}${dim}`;
    const path = edgePath(edge, from, to, model), mx = (from.x + to.x) / 2, my = (from.y + to.y) / 2;
    const label = edge.kind === "owns" ? "" : edge.label || (edge.readiness ? edge.readiness.label : relationTypeLabel(edge));
    const edgeLabel = `<text x="${mx}" y="${my - 8}" text-anchor="middle">${esc(label || "")}</text>${edge.kind === "owns" ? "" : `<title>${esc(`${relationTypeLabel(edge)} · ${label || ""}`)}</title>`}`;
    return `<g class="${cls}" data-action="${edge.kind === "owns" ? "" : "cp-edge"}" data-id="${edge.id}"><path d="${path}" marker-end="${edge.kind === "owns" ? "" : "url(#cp-arrow)"}"/>${edgeLabel}</g>`;
  }

  function renderGraphSurface(data, placement = "right") {
    const model = graphModel(data), nodeById = Object.fromEntries(model.nodes.map((node) => [node.id, node]));
    const selectedEdge = state.selection?.kind === "edge" ? model.edges.find((edge) => edge.id === state.selection.id) || relation(data, state.selection.id) : null;
    const selectedEndpoints = selectedEdge ? new Set([selectedEdge.from, selectedEdge.to]) : new Set();
    const selectedTaskId = state.selection?.kind === "node" && task(data, state.selection.id) ? state.selection.id : "";
    const highlightedEdges = new Set(selectedTaskId ? model.edges.filter((edge) => edge.kind !== "owns" && (edge.from === selectedTaskId || edge.to === selectedTaskId)).map((edge) => edge.id) : []);
    const canBack = state.mode !== "aggregate";
    return `<section class="cp-surface placement-${placement} ${state.selection ? "inspector-open" : "inspector-closed"}">
      <div class="cp-canvas-wrap" id="cp-canvas-wrap">
        <div class="cp-legend"><span><i class="type o"></i>O</span><span><i class="type kr"></i>KR</span><span><i class="type task"></i>任务</span><span><i class="type warning"></i>预警</span><span><i class="type risk"></i>高风险／卡点</span><span><i class="line normal"></i>普通关系</span><span><i class="line feedback"></i>反馈</span><span><i class="line interlock"></i>互锁</span></div>
        <svg id="cp-graph-svg" viewBox="0 0 1300 ${state.mode === "full" ? 1260 : 760}" role="img" aria-label="协作关系图谱" class="labels-key">
          <defs><marker id="cp-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M0 0 10 5 0 10Z"/></marker></defs>
          <g id="cp-stage" transform="translate(${state.pan.x} ${state.pan.y}) scale(${state.zoom})">
            ${model.edges.map((edge) => edgeMarkup({ ...edge, pathHighlighted: highlightedEdges.has(edge.id) }, model, nodeById)).join("")}
            ${model.nodes.map((node) => nodeMarkup({ ...node, endpoint: selectedEndpoints.has(node.id) })).join("")}
          </g>
        </svg>
        <button class="cp-canvas-action cp-back-action" data-action="cp-back" aria-label="返回上一级" ${canBack ? "" : "disabled"}>${icon("back")}<span>返回上一级</span></button>
        <div class="cp-canvas-controls"><button data-action="cp-zoom" data-value="out" aria-label="缩小" title="缩小">−</button><span>${Math.round(state.zoom * 100)}%</span><button data-action="cp-zoom" data-value="in" aria-label="放大" title="放大">＋</button><button data-action="cp-fit" aria-label="适应屏幕" title="适应屏幕">${icon("fit")}</button></div>
        <button class="cp-canvas-action cp-relayout-action" data-action="cp-relayout" aria-label="重新布局">${icon("reset")}<span>重新布局</span></button>
        <div class="cp-minimap" aria-hidden="true"><svg viewBox="0 0 1300 ${state.mode === "full" ? 1260 : 760}">${model.edges.filter((edge) => edge.kind !== "owns").map((edge) => { const a = nodeById[edge.from], b = nodeById[edge.to]; return a && b ? `<line x1="${a.x}" y1="${a.y}" x2="${b.x}" y2="${b.y}"/>` : ""; }).join("")}${model.nodes.map((node) => `<circle cx="${node.x}" cy="${node.y}" r="12" class="${node.type} ${node.risk || ""}"/>`).join("")}</svg><i></i></div>
      </div>
      ${state.selection ? renderInspector(data, model) : ""}
    </section>`;
  }

  function affectedGoals(data, taskId) {
    const queue = [taskId], seen = new Set(queue), affectedKrs = new Set(), affectedOs = new Set();
    while (queue.length) {
      const current = queue.shift();
      data.relations.filter((rel) => rel.from === current && relationType(rel) === "hard").forEach((rel) => {
        if (seen.has(rel.to)) return; seen.add(rel.to); queue.push(rel.to);
        const target = task(data, rel.to), targetKr = kr(data, target?.krId); if (targetKr) { affectedKrs.add(targetKr.id); affectedOs.add(targetKr.objectiveId); }
      });
    }
    return { krs: [...affectedKrs], os: [...affectedOs] };
  }
  function property(label, value) { return `<div class="cp-property"><span>${label}</span><strong>${value}</strong></div>`; }
  function renderNodeDetail(data, node) {
    if (!node) return `<div class="cp-inspector-empty">${icon("graph")}<b>选择一个节点或关系</b><span>查看负责人、输入就绪、交付内容和影响路径。</span></div>`;
    const itemTask = task(data, node.id), itemKr = kr(data, node.id), itemO = objective(data, node.id), itemRisk = risk(data, node.id), itemMember = data.members.find((item) => item.id === node.id);
    if (itemTask) {
      const itemKrValue = kr(data, itemTask.krId), itemOValue = objective(data, itemKrValue?.objectiveId), incoming = data.relations.filter((rel) => rel.to === itemTask.id), outgoing = data.relations.filter((rel) => rel.from === itemTask.id), affected = affectedGoals(data, itemTask.id), current = data.deliverables.filter((item) => item.taskId === itemTask.id && item.state === "已生效"), itemRiskFact = data.risks.find((item) => item.taskId === itemTask.id);
      const taskRiskKey = riskKeyForTask(data, itemTask);
      return `<div class="cp-inspector-title"><span class="cp-eyebrow">任务节点 · ${itemTask.id}</span><h2>${esc(itemTask.title)}</h2>${statusBadge(itemTask.status, taskStatusKey(itemTask.status))}${taskRiskKey !== "normal" ? statusBadge(riskLabel(taskRiskKey), taskRiskKey) : ""}</div>
        <div class="cp-property-grid">${property("所属 O / KR", `${itemOValue?.id || "—"} / ${itemKrValue?.id || "—"}`)}${property("负责人", `${esc(member(data, itemTask.owner).displayName)} · ${esc(member(data, itemTask.owner).role)}`)}${property("计划周期", `${itemTask.start} — ${itemTask.due}`)}${property("直接输入 / 输出", `${incoming.length} / ${outgoing.length}`)}</div>
        ${itemRiskFact ? `<div class="cp-risk-fact ${taskRiskKey}"><span>结构化卡点 · ${esc(itemRiskFact.level)}</span><b>${esc(itemRiskFact.reason)}</b><small>待行动人：${esc(member(data, itemRiskFact.actionOwner).displayName)} · 已持续 ${itemRiskFact.days} 天</small><small>${esc(itemRiskFact.impact)}</small></div>` : ""}
        <div class="cp-detail-block"><div class="cp-block-head"><b>输入就绪</b><span>${incoming.filter((rel) => relationReadiness(data, rel).ready).length}/${incoming.length} 已就绪</span></div>${incoming.slice(0, 4).map((rel) => `<button class="cp-relation-mini" data-action="cp-edge" data-id="${rel.id}"><span><b>${rel.from} → ${rel.to}</b><small>${esc(rel.label)} · ${relationTypeLabel(rel)}</small></span>${statusBadge(relationReadiness(data, rel).label, relationReadiness(data, rel).key)}</button>`).join("") || `<p class="cp-empty-copy">没有配置上游输入。</p>`}</div>
        <div class="cp-detail-block"><div class="cp-block-head"><b>当前交付物</b><span>${current.length} 项</span></div>${current.slice(0, 2).map((file) => `<div class="cp-file-row"><span><b>${esc(file.name)}</b><small>${esc(file.fileType || "文件")} · ${esc(file.fileSize || "—")}</small></span><span><button class="cp-text-btn" data-action="preview-file" data-id="${file.id}">预览</button><button class="cp-text-btn" data-action="download-file" data-id="${file.id}">下载</button></span></div>`).join("") || `<p class="cp-empty-copy">尚无已生效的当前交付物。</p>`}</div>
        <div class="cp-impact"><span>系统推导 · 仅沿下游硬前置</span><b>受影响 O：${affected.os.join("、") || "无"}</b><b>受影响 KR：${affected.krs.join("、") || "无"}</b></div>`;
    }
    if (itemKr) {
      const tasks = data.tasks.filter((item) => item.krId === itemKr.id), itemRisks = data.risks.filter((r) => tasks.some((t) => t.id === r.taskId));
      return `<div class="cp-inspector-title"><span class="cp-eyebrow">KR 节点 · ${itemKr.id}</span><h2>${esc(itemKr.title)}</h2>${statusBadge(riskLabel(itemKr.risk), itemKr.risk)}</div><div class="cp-property-grid">${property("负责人", esc(member(data, itemKr.owner).displayName))}${property("任务数量", `${tasks.length} 项`)}${property("卡点数量", `${itemRisks.length} 项`)}${property("周期", esc(itemKr.cycle))}</div>${itemRisks.length ? `<div class="cp-risk-fact ${itemKr.risk}"><span>${riskLabel(itemKr.risk)}原因</span><b>${esc(itemRisks[0].reason)}</b><small>${esc(itemRisks[0].impact)}</small></div>` : ""}<div class="cp-detail-block"><div class="cp-block-head"><b>量化指标</b></div><p>${esc(itemKr.metric)}</p></div>`;
    }
    if (itemO) {
      const children = data.krs.filter((item) => item.objectiveId === itemO.id);
      return `<div class="cp-inspector-title"><span class="cp-eyebrow">目标 O · ${itemO.id}</span><h2>${esc(itemO.title)}</h2></div><div class="cp-property-grid">${property("下属 KR", `${children.length} 个`)}${property("预警／高风险 KR", `${children.filter((item) => item.risk !== "normal").length} 个`)}</div><div class="cp-detail-block"><div class="cp-block-head"><b>目标说明</b></div><p>${esc(itemO.note)}</p></div>`;
    }
    if (itemRisk) {
      const owner = member(data, itemRisk.actionOwner), belongs = task(data, itemRisk.taskId), affected = affectedGoals(data, itemRisk.taskId);
      return `<div class="cp-inspector-title"><span class="cp-eyebrow">结构化卡点 · ${itemRisk.id}</span><h2>${esc(itemRisk.reason)}</h2>${statusBadge(itemRisk.level, itemRisk.level === "高风险" ? "risk" : "warning")}</div><div class="cp-property-grid">${property("所属任务", `${itemRisk.taskId} · ${esc(belongs?.title || "")}`)}${property("待行动人", `${esc(owner.displayName)} · ${esc(owner.role)}`)}${property("已持续", `${itemRisk.days} 天`)}${property("影响范围", esc(itemRisk.impact))}</div><div class="cp-impact"><span>系统推导 · 硬前置影响路径</span><b>${affected.os.join("、") || "暂无上层目标影响"}</b><b>${affected.krs.join("、") || "暂无下游 KR 影响"}</b></div>`;
    }
    if (itemMember) {
      const requests = data.inputRequests.filter((item) => item.provider === itemMember.id), actionRisks = data.risks.filter((item) => item.actionOwner === itemMember.id);
      return `<div class="cp-inspector-title"><span class="cp-eyebrow">关系相关项目成员 · ${itemMember.id}</span><h2>${esc(itemMember.displayName)}</h2><p>${esc(itemMember.role)} · ${esc(itemMember.team)}</p></div><div class="cp-property-grid">${property("待提供输入", `${requests.filter((item) => item.state !== "已提供").length} 项`)}${property("卡点行动责任", `${actionRisks.length} 项`)}</div><div class="cp-detail-block"><div class="cp-block-head"><b>当前关系职责</b></div>${requests.map((item) => `<button class="cp-relation-mini" data-action="cp-focus" data-id="${item.taskId}"><span><b>${esc(item.inputName)}</b><small>目标任务 ${item.taskId} · 计划 ${item.due}</small></span>${statusBadge(item.state, item.state === "已提供" ? "ready" : "waiting")}</button>`).join("") || `<p class="cp-empty-copy">当前无直接输入责任。</p>`}</div>`;
    }
    return `<div class="cp-inspector-empty">${icon("graph")}<b>选择节点查看详情</b></div>`;
  }

  function renderRelationDetail(data, edge) {
    if (!edge) return `<div class="cp-inspector-empty">${icon("graph")}<b>选择关系查看详情</b></div>`;
    const concrete = relation(data, edge.id) || edge, from = task(data, concrete.from), to = task(data, concrete.to), readiness = relationReadiness(data, concrete), current = currentDeliverable(data, concrete), candidate = candidateDeliverable(data, concrete);
    return `<div class="cp-inspector-title"><span class="cp-eyebrow">交付物边 · ${concrete.id || "关系"}</span><h2>${esc(concrete.label || current?.name || "任务交付关系")}</h2>${statusBadge(readiness.label, readiness.key)}</div><div class="cp-flow-card"><button data-action="cp-focus" data-id="${concrete.from}"><span>${concrete.from}</span><b>${esc(from?.title || nameOf(data, concrete.from))}</b></button><i>→</i><button data-action="cp-focus" data-id="${concrete.to}"><span>${concrete.to}</span><b>${esc(to?.title || nameOf(data, concrete.to))}</b></button></div><div class="cp-property-grid">${property("关系类型", relationTypeLabel(concrete))}${property("必要性", relationType(concrete) === "hard" ? "下游开始／完成前必须就绪" : "参考或成果流转")}${property("提供方", esc(member(data, from?.owner).displayName))}${property("接收方", esc(member(data, to?.owner).displayName))}${property("计划到达", esc(from?.due || "待补充"))}${property("关键路径", concrete.type === "interlock" ? "互锁风险，不计算传统关键路径" : concrete.pathId ? `${concrete.pathId} · 关键路径` : relationType(concrete) === "hard" ? "硬依赖链" : "不参与")}</div>${candidate ? `<div class="cp-review-note">有更新审核中：候选内容不作为正式输入，当前内容继续有效。</div>` : ""}<div class="cp-detail-block"><div class="cp-block-head"><b>当前交付物</b><span>${current ? "终审已生效" : "尚未形成"}</span></div>${current ? `<div class="cp-file-row"><span><b>${esc(current.name)}</b><small>${esc(current.fileType || "文件")} · ${esc(current.fileSize || "—")} · ${esc(current.file)}</small></span><span><button class="cp-text-btn" data-action="preview-file" data-id="${current.id}">预览</button><button class="cp-text-btn" data-action="download-file" data-id="${current.id}">下载</button></span></div>` : `<p class="cp-empty-copy">当前没有已生效内容；${readiness.label}。</p>`}</div><div class="cp-inspector-actions"><button class="cp-btn" data-action="task-detail" data-id="${concrete.from}">来源任务</button><button class="cp-btn" data-action="task-detail" data-id="${concrete.to}">目标任务</button></div>`;
  }

  function selectedEdgeFromModel(data, model) {
    if (state.selection?.kind !== "edge") return null;
    return model.edges.find((edge) => edge.id === state.selection.id) || relation(data, state.selection.id);
  }
  function renderInspector(data, model) {
    const edge = selectedEdgeFromModel(data, model), node = state.selection?.kind === "node" ? model.nodes.find((item) => item.id === state.selection.id) || modelNode(data, state.selection.id, task(data, state.selection.id) ? "task" : kr(data, state.selection.id) ? "kr" : objective(data, state.selection.id) ? "objective" : risk(data, state.selection.id) ? "risk" : "person", 0, 0) : null;
    return `<aside class="cp-inspector"><div class="cp-inspector-head"><span>${edge ? "关系详情" : "节点详情"}</span><span class="cp-readonly">${icon("lock")}只读分析</span><button data-action="cp-clear" aria-label="关闭详情">${icon("close")}</button></div><div class="cp-inspector-body">${edge ? renderRelationDetail(data, edge) : renderNodeDetail(data, node)}</div></aside>`;
  }

  function searchCandidates(data) {
    const query = state.query.trim().toLowerCase(); if (!query) return [];
    const nodes = [
      ...data.objectives.map((item) => ({ kind: "node", id: item.id, type: "O", title: item.title })),
      ...data.krs.map((item) => ({ kind: "node", id: item.id, type: "KR", title: item.title })),
      ...data.tasks.map((item) => ({ kind: "node", id: item.id, type: "任务", title: item.title })),
      ...data.members.filter((item) => data.inputRequests.some((r) => r.provider === item.id) || data.risks.some((r) => r.actionOwner === item.id)).map((item) => ({ kind: "node", id: item.id, type: "成员", title: `${item.displayName} · ${item.role}` })),
      ...data.risks.map((item) => ({ kind: "node", id: item.id, type: "卡点", title: item.reason })),
    ];
    const edges = data.relations.map((rel) => ({ kind: "edge", id: rel.id, type: "关系", title: `${rel.from} → ${rel.to} · ${rel.label} · ${currentDeliverable(data, rel)?.name || ""}` }));
    return [...nodes, ...edges].filter((item) => `${item.id}${item.type}${item.title}`.toLowerCase().includes(query)).slice(0, 8);
  }
  function renderSearch(data, compact = false) {
    const candidates = searchCandidates(data);
    return `<div class="cp-search ${compact ? "compact" : ""}"><label>${icon("search")}<input id="cp-search-input" value="${esc(state.query)}" placeholder="搜索 O、KR、任务、成员、卡点或关系" autocomplete="off"/></label>${state.searchOpen && state.query ? `<div class="cp-search-results">${candidates.map((item) => `<button data-action="cp-search-select" data-kind="${item.kind}" data-id="${item.id}"><span class="cp-result-type">${item.type}</span><span><b>${item.id}</b><small>${esc(item.title)}</small></span>${icon("chevron")}</button>`).join("") || `<div class="cp-search-empty">没有匹配对象，画布保持不变。</div>`}</div>` : ""}</div>`;
  }
  function option(value, label, current) { return `<option value="${value}" ${value === current ? "selected" : ""}>${label}</option>`; }
  function renderFilters(data, condensed = false) {
    const f = state.filters;
    return `<div class="cp-filters ${condensed ? "condensed" : ""}">
      <select data-filter="o" aria-label="按 O 筛选">${option("all", "全部 O", f.o)}${data.objectives.map((item) => option(item.id, item.id, f.o)).join("")}</select>
      <select data-filter="kr" aria-label="按 KR 筛选">${option("all", "全部 KR", f.kr)}${data.krs.filter((item) => f.o === "all" || item.objectiveId === f.o).map((item) => option(item.id, item.id, f.kr)).join("")}</select>
      <select data-filter="person" aria-label="按人员筛选">${option("all", "全部人员", f.person)}${data.members.map((item) => option(item.id, item.displayName, f.person)).join("")}</select>
      <label class="cp-completed-toggle"><input type="checkbox" data-filter="includeCompleted" ${f.includeCompleted ? "checked" : ""}/><span aria-hidden="true"></span><b>显示已完成任务</b></label>
    </div>`;
  }
  function renderViewSwitch() {
    return `<div class="cp-segment"><button class="${state.view === "graph" ? "active" : ""}" data-action="cp-view" data-value="graph">${icon("graph")}图谱</button><button class="${state.view === "list" ? "active" : ""}" data-action="cp-view" data-value="list">${icon("list")}列表</button></div>`;
  }
  function renderMainActions() {
    return `<div class="cp-main-actions"><button class="cp-btn primary" data-action="cp-mode" data-value="${state.mode === "full" ? "aggregate" : "full"}">${icon("expand")}${state.mode === "full" ? "返回层级视图" : "全局展开"}</button></div>`;
  }

  function filteredRelations(data) {
    return visibleRelations(data).filter((rel) => rel.matched).sort((a, b) => {
      if (state.sort === "risk") return Number(!a.readiness.ready) - Number(!b.readiness.ready) || a.id.localeCompare(b.id);
      if (state.sort === "type") return relationTypeLabel(a).localeCompare(relationTypeLabel(b), "zh-CN");
      return (task(data, a.from)?.due || "99-99").localeCompare(task(data, b.from)?.due || "99-99");
    });
  }
  function renderRelationList(data) {
    const rows = filteredRelations(data);
    return `<section class="cp-list-card"><div class="cp-list-head"><div><b>${rows.length} 条任务关系</b><span>与图谱共享搜索、O／KR／人员筛选和已完成任务范围；V1 只读</span></div><label>排序<select id="cp-list-sort">${option("planned", "计划到达", state.sort)}${option("risk", "未就绪优先", state.sort)}${option("type", "关系类型", state.sort)}</select></label></div><div class="cp-table-wrap"><table><thead><tr><th>来源任务</th><th>交付物边</th><th>类型 / 必要性</th><th>当前内容</th><th>目标任务</th><th>提供 / 接收</th><th>就绪状态</th><th>计划日期</th><th></th></tr></thead><tbody>${rows.map((rel) => { const from = task(data, rel.from), to = task(data, rel.to), current = currentDeliverable(data, rel); return `<tr class="${rel.readiness.ready ? "" : "not-ready"}"><td><button data-action="cp-focus" data-id="${rel.from}"><b>${rel.from}</b><span>${esc(from?.title || "")}</span><span>${esc(from?.status || "")}</span></button></td><td><button data-action="cp-edge" data-id="${rel.id}"><b>${esc(current?.name || rel.label)}</b><span>${rel.id}</span></button></td><td><b>${relationTypeLabel(rel)}</b><span>${relationType(rel) === "hard" ? "必须就绪" : "参考／流转"}</span></td><td>${current ? `<b>${esc(current.fileType || "文件")}</b><span>${esc(current.fileSize || "—")}</span>` : `<span>尚无当前内容</span>`}</td><td><button data-action="cp-focus" data-id="${rel.to}"><b>${rel.to}</b><span>${esc(to?.title || "")}</span><span>${esc(to?.status || "")}</span></button></td><td><b>${esc(member(data, from?.owner).displayName)} → ${esc(member(data, to?.owner).displayName)}</b><span>${from?.krId || "—"} → ${to?.krId || "—"}</span></td><td>${statusBadge(rel.readiness.label, rel.readiness.key)}</td><td>${from?.due || "待补充"}</td><td><button class="cp-row-open" data-action="cp-edge" data-id="${rel.id}" aria-label="查看关系详情">${icon("chevron")}</button></td></tr>`; }).join("") || `<tr><td colspan="9"><div class="cp-list-empty">没有匹配的任务关系。调整筛选后再查看。</div></td></tr>`}</tbody></table></div></section>`;
  }

  function renderRiskLens(data) {
    const groups = data.krs.map((item) => {
      const tasks = data.tasks.filter((t) => t.krId === item.id), blockers = data.risks.filter((r) => tasks.some((t) => t.id === r.taskId)), notReady = data.relations.filter((rel) => tasks.some((t) => t.id === rel.to) && !relationReadiness(data, rel).ready);
      const targetTaskId = blockers[0]?.taskId || notReady[0]?.to || tasks[0]?.id;
      return { item, blockers, notReady, targetTaskId, targetTask: task(data, targetTaskId) };
    }).filter((group) => group.blockers.length || group.notReady.length).sort((a, b) => (b.blockers.length * 3 + b.notReady.length) - (a.blockers.length * 3 + a.notReady.length));
    return `<aside class="cp-risk-lens"><div class="cp-lens-head"><span>风险队列</span><b>${groups.length} 个 KR 需关注</b></div><div class="cp-lens-list">${groups.map((group) => `<button data-action="cp-risk-open" data-id="${group.targetTaskId}" data-kr="${group.item.id}" class="${state.focusId === group.item.id && state.selection?.id === group.targetTaskId ? "active" : ""}"><span><b>${group.item.id} · ${group.targetTaskId}</b><small>${esc(group.targetTask?.title || group.item.title)}</small></span><span>${group.blockers.length ? `<i>${group.blockers.length} 卡点</i>` : ""}<em>${group.notReady.length} 未就绪</em></span></button>`).join("")}</div><div class="cp-lens-foot"><span>红色仅表示真实阻塞或冲突</span><button data-action="cp-mode" data-value="aggregate">回到 O／KR 层级树</button></div></aside>`;
  }

  function renderCollaborationWorkspace(data) {
    return `<div class="relationship-prototype variant-a"><div class="cp-page-head"><div><span class="cp-kicker">项目事实 · 只读分析</span><h1>协作关系</h1><p>先查看 O 与 KR 的清晰归属，进入 KR 后再沿真实交付物边追踪任务关系与风险。</p></div>${renderMainActions()}</div><div class="cp-commandbar"><div class="cp-command-left">${renderSearch(data)}${renderFilters(data)}</div><div class="cp-command-right">${renderViewSwitch()}</div></div><div class="cp-a-layout">${renderRiskLens(data)}<div class="cp-a-main">${state.view === "list" ? renderRelationList(data) : renderGraphSurface(data, "right")}</div></div></div>`;
  }

  function render() {
    if ((location.hash || "#overview").slice(1).split("?")[0] !== "graph") return;
    const page = $("#page"), data = getData(); if (!page || !data) return;
    page.classList.add("cp-page");
    page.innerHTML = renderCollaborationWorkspace(data);
    bindCanvas();
  }
  function focus(id) {
    if (!id) return; const itemRisk = risk(getData(), id), focusId = itemRisk?.taskId || id; remember(); state.mode = "focus"; state.focusId = focusId; state.selection = { kind: "node", id: focusId }; state.zoom = 1; state.pan = { x: 0, y: 0 }; render();
  }
  function openRiskTask(taskId, krId) {
    if (!taskId || !krId) return;
    remember(); state.mode = "focus"; state.focusId = krId; state.selection = { kind: "node", id: taskId }; state.zoom = 1; state.pan = { x: 0, y: 0 }; render();
  }
  function goBack() {
    const data = getData();
    if (state.mode === "full") { setMode("aggregate"); return; }
    if (state.mode !== "focus") return;
    const focusedTask = task(data, state.focusId), focusedKr = kr(data, state.focusId), focusedO = objective(data, state.focusId);
    if (focusedTask) {
      state.focusId = focusedTask.krId; state.selection = { kind: "node", id: focusedTask.krId };
    } else if (focusedKr) {
      state.focusId = focusedKr.objectiveId; state.selection = { kind: "node", id: focusedKr.objectiveId };
    } else if (focusedO) {
      state.mode = "aggregate"; state.focusId = ""; state.selection = null;
    } else {
      state.mode = "aggregate"; state.focusId = ""; state.selection = null;
    }
    state.zoom = 1; state.pan = { x: 0, y: 0 }; render();
  }
  function selectEdge(id) {
    const data = getData(), rel = relation(data, id); state.selection = { kind: "edge", id };
    if (rel && state.mode === "aggregate") {
      remember(); state.mode = "focus"; state.focusId = rel.to;
    }
    render();
  }
  function setMode(mode) {
    remember(); state.mode = mode; state.focusId = mode === "focus" ? state.focusId || "KR1" : ""; state.selection = mode === "aggregate" ? null : state.selection; state.zoom = 1; state.pan = { x: 0, y: 0 }; render();
  }

  function handlePrototypeClick(event) {
    const actionEl = event.target.closest("[data-action]"); if (!actionEl) return;
    const inPrototype = actionEl.closest(".relationship-prototype"); if (!inPrototype) return;
    const action = actionEl.dataset.action;
    if (["task-detail", "preview-file", "download-file"].includes(action)) return;
    event.preventDefault(); event.stopImmediatePropagation();
    const id = actionEl.dataset.id, value = actionEl.dataset.value;
    if (action === "cp-view") { state.view = value; render(); }
    else if (action === "cp-mode") setMode(value);
    else if (action === "cp-focus" || action === "cp-node") { if (state.suppressClick === id) { state.suppressClick = ""; return; } focus(id); }
    else if (action === "cp-risk-open") openRiskTask(id, actionEl.dataset.kr);
    else if (action === "cp-edge") selectEdge(id);
    else if (action === "cp-back") goBack();
    else if (action === "cp-collapse") setMode("aggregate");
    else if (action === "cp-clear") { state.selection = null; render(); }
    else if (action === "cp-fit") { state.zoom = 1; state.pan = { x: 0, y: 0 }; render(); }
    else if (action === "cp-relayout") { state.fixed = {}; state.zoom = 1; state.pan = { x: 0, y: 0 }; render(); }
    else if (action === "cp-zoom") { state.zoom = Math.max(.45, Math.min(2.2, state.zoom + (value === "in" ? .15 : -.15))); updateTransform(); }
    else if (action === "cp-search-select") { state.searchOpen = false; state.query = ""; actionEl.dataset.kind === "edge" ? selectEdge(id) : focus(id); }
  }
  function handlePrototypeInput(event) {
    if (event.target.id !== "cp-search-input") return;
    event.stopImmediatePropagation(); state.query = event.target.value; state.searchOpen = true; render();
    const input = $("#cp-search-input"); if (input) { input.focus(); input.setSelectionRange(state.query.length, state.query.length); }
  }
  function handlePrototypeChange(event) {
    if (event.target.matches("[data-filter]")) {
      event.stopImmediatePropagation(); const key = event.target.dataset.filter; state.filters[key] = event.target.type === "checkbox" ? event.target.checked : event.target.value; if (key === "o") state.filters.kr = "all"; render();
    }
    if (event.target.id === "cp-list-sort") { event.stopImmediatePropagation(); state.sort = event.target.value; render(); }
  }
  function handleKeys(event) {
    if (event.key === "Escape" && state.searchOpen) { state.searchOpen = false; render(); }
  }

  function updateTransform() {
    const stage = $("#cp-stage"); if (stage) stage.setAttribute("transform", `translate(${state.pan.x} ${state.pan.y}) scale(${state.zoom})`);
    const percent = $(".cp-canvas-controls span"); if (percent) percent.textContent = `${Math.round(state.zoom * 100)}%`;
  }
  function bindCanvas() {
    const svg = $("#cp-graph-svg"), canvas = $("#cp-canvas-wrap"); if (!svg || !canvas) return;
    svg.addEventListener("wheel", (event) => { event.preventDefault(); state.zoom = Math.max(.45, Math.min(2.2, state.zoom + (event.deltaY < 0 ? .08 : -.08))); updateTransform(); }, { passive: false });
    let panning = false, start = null, origin = null, panMoved = false;
    svg.addEventListener("pointerdown", (event) => {
      if (event.target.closest("[data-cp-node], .cp-edge")) return;
      panning = true; panMoved = false; start = { x: event.clientX, y: event.clientY }; origin = { ...state.pan }; canvas.classList.add("dragging"); svg.setPointerCapture(event.pointerId);
    });
    svg.addEventListener("pointermove", (event) => { if (!panning) return; const dx = event.clientX - start.x, dy = event.clientY - start.y; panMoved ||= Math.hypot(dx, dy) > 4; state.pan = { x: origin.x + dx, y: origin.y + dy }; updateTransform(); });
    svg.addEventListener("pointerup", () => { const wasPanning = panning; panning = false; canvas.classList.remove("dragging"); if (wasPanning && !panMoved && state.selection) { state.selection = null; render(); } });
    $$('[data-cp-node]', svg).forEach((nodeEl) => {
      nodeEl.addEventListener("pointerdown", (event) => {
        event.stopPropagation(); const id = nodeEl.dataset.cpNode, model = graphModel(getData()), node = model.nodes.find((item) => item.id === id); if (!node) return;
        const startPoint = { x: event.clientX, y: event.clientY }, original = { x: node.x, y: node.y }; let moved = false;
        nodeEl.setPointerCapture(event.pointerId);
        const move = (moveEvent) => { const dx = (moveEvent.clientX - startPoint.x) / state.zoom, dy = (moveEvent.clientY - startPoint.y) / state.zoom; moved ||= Math.hypot(dx, dy) > 4; if (moved) nodeEl.setAttribute("transform", `translate(${original.x + dx} ${original.y + dy})`); };
        const up = (upEvent) => { nodeEl.removeEventListener("pointermove", move); nodeEl.removeEventListener("pointerup", up); if (!moved) return; const dx = (upEvent.clientX - startPoint.x) / state.zoom, dy = (upEvent.clientY - startPoint.y) / state.zoom; state.fixed[id] = { x: original.x + dx, y: original.y + dy }; state.suppressClick = id; render(); };
        nodeEl.addEventListener("pointermove", move); nodeEl.addEventListener("pointerup", up);
      });
    });
  }

  document.addEventListener("click", handlePrototypeClick, true);
  document.addEventListener("input", handlePrototypeInput, true);
  document.addEventListener("change", handlePrototypeChange, true);
  document.addEventListener("keydown", handleKeys, true);
  window.addEventListener("hashchange", () => setTimeout(render, 0));
  setTimeout(render, 0);
})();

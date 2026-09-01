(function () {
  "use strict";

  // PROTOTYPE — V4.5 merges the OKR entry into Project Overview while keeping management as a focused page mode.
  const STORAGE_KEY = "collaboration-prototype-v4.5-overview-okr-merged";
  const IDENTITY_KEY = `${STORAGE_KEY}:identity`;
  const seed = window.PROTOTYPE_SEED;
  const clone = (value) => JSON.parse(JSON.stringify(value));
  let data = loadData();
  // Remove the old T16 placeholder file from both seed and persisted demo data.
  // A real upload is marked with uploadedByUser and remains visible to every viewer.
  data.deliverables = data.deliverables.filter((item) => !(item.taskId === "T16" && item.file === "future-life-16.pdf" && item.uploadedByUser !== true));
  const ui = {
    identity: localStorage.getItem(IDENTITY_KEY) || "P02",
    expandedKrs: new Set(["KR1"]),
    taskSearch: "",
    taskKr: "all",
    taskStatus: "all",
    myTab: "todo",
    settingsTab: "members",
    reportPeriod: "7d",
    artifactSelected: new Set(),
    graphView: "graph",
    graphMode: "aggregate",
    graphSelected: "KR1",
    graphHistory: [],
    graphZoom: 1,
    graphPan: { x: 0, y: 0 },
    graphSearch: "",
    graphO: "all",
    graphKr: "all",
    graphPerson: "all",
    graphRisk: "all",
    graphRelation: "all",
    graphTime: "active",
    taskDrawerSource: "overview",
    taskDrawerContext: null,
    inviteMemberSelected: new Set(),
    inviteMemberSourceMarked: new Set(),
    inviteMemberTargetMarked: new Set(),
    inviteMemberCollapsedRoles: new Set(),
    inviteMemberSourceSearch: "",
    inviteMemberTargetSearch: "",
    inputTaskSelected: new Set(),
    inputTaskSourceMarked: new Set(),
    inputTaskTargetMarked: new Set(),
    inputTaskCollapsedObjectives: new Set(),
    inputTaskCollapsedKrs: new Set(),
    inputTaskSourceSearch: "",
    inputTaskTargetSearch: "",
    inputMemberSelected: new Set(),
    inputMemberSourceMarked: new Set(),
    inputMemberTargetMarked: new Set(),
    inputMemberCollapsedTeams: new Set(),
    inputMemberSourceSearch: "",
    inputMemberTargetSearch: "",
  };

  const routes = [
    ["overview", "项目总览", "overview"],
    ["tasks", "全部任务", "list"],
    ["graph", "协作关系", "graph"],
    ["mywork", "我的工作", "inbox"],
    ["artifacts", "成果与归档", "archive"],
    ["reports", "项目报告", "report"],
  ];

  const icons = {
    overview: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
    target: '<circle cx="12" cy="12" r="8"/><circle cx="12" cy="12" r="4"/><path d="m15 9 6-6M16 3h5v5"/>',
    list: '<path d="M8 6h13M8 12h13M8 18h13"/><circle cx="4" cy="6" r="1"/><circle cx="4" cy="12" r="1"/><circle cx="4" cy="18" r="1"/>',
    graph: '<circle cx="5" cy="12" r="2.5"/><circle cx="19" cy="5" r="2.5"/><circle cx="19" cy="19" r="2.5"/><path d="m7.2 10.9 9.5-4.8M7.2 13.1l9.5 4.8"/>',
    inbox: '<path d="M4 5h16v14H4z"/><path d="m4 14 4-4h8l4 4M9 16h6"/>',
    alert: '<path d="M12 3 2.8 20h18.4L12 3Z"/><path d="M12 9v5M12 17.5v.1"/>',
    archive: '<path d="M4 7h16v14H4zM3 3h18v4H3zM9 11h6"/>',
    report: '<path d="M5 3h14v18H5zM8 7h8M8 11h8M8 15h5"/>',
    settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3v-.2h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z"/>',
    search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>',
    bell: '<path d="M18 9a6 6 0 1 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"/>',
    chevron: '<path d="m9 6 6 6-6 6"/>',
    down: '<path d="m6 9 6 6 6-6"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    close: '<path d="m6 6 12 12M18 6 6 18"/>',
    menu: '<path d="M4 6h16M4 12h16M4 18h16"/>',
    reset: '<path d="M4 12a8 8 0 1 0 2-5.3L4 9M4 4v5h5"/>',
    filter: '<path d="M3 5h18l-7 8v6l-4 2v-8L3 5Z"/>',
    expand: '<path d="M8 3H3v5M16 3h5v5M8 21H3v-5M16 21h5v-5"/>',
    zoomin: '<circle cx="10" cy="10" r="6"/><path d="M10 7v6M7 10h6m2.5 5.5L21 21"/>',
    zoomout: '<circle cx="10" cy="10" r="6"/><path d="M7 10h6m2.5 5.5L21 21"/>',
    fit: '<path d="M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5"/>',
    back: '<path d="m15 18-6-6 6-6"/>',
    download: '<path d="M12 3v12m0 0 5-5m-5 5-5-5M4 20h16"/>',
    check: '<path d="m5 12 4 4L19 6"/>',
    x: '<path d="m6 6 12 12M18 6 6 18"/>',
    upload: '<path d="M12 16V4m0 0L7 9m5-5 5 5M4 20h16"/>',
    link: '<path d="M10 13a5 5 0 0 0 7.5.5l2-2a5 5 0 0 0-7-7l-1.1 1.1M14 11a5 5 0 0 0-7.5-.5l-2 2a5 5 0 0 0 7 7l1.1-1.1"/>',
    user: '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
    package: '<path d="M4 7 12 3l8 4-8 4-8-4Zm0 0v10l8 4 8-4V7M12 11v10"/>',
    edit: '<path d="m4 20 4.5-1 10-10-3.5-3.5-10 10L4 20ZM13.8 6.7l3.5 3.5"/>',
    more: '<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
  };

  function icon(name) {
    return `<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name] || icons.more}</svg>`;
  }
  function hydrateIcons(root = document) {
    root.querySelectorAll("[data-icon]").forEach((el) => { el.innerHTML = icon(el.dataset.icon); el.removeAttribute("data-icon"); });
  }
  function loadData() {
    try { return JSON.parse(localStorage.getItem(STORAGE_KEY)) || clone(seed); } catch (_) { return clone(seed); }
  }
  function saveData(action) {
    if (action) data.audit.unshift({ time: "刚刚", actor: ui.identity, action });
    localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
  }
  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  const getMember = (id) => data.members.find((x) => x.id === id) || { id, role: "未知成员", team: "" };
  const getTask = (id) => data.tasks.find((x) => x.id === id);
  const getKr = (id) => data.krs.find((x) => x.id === id);
  const getObjective = (id) => data.objectives.find((x) => x.id === id);
  const getDeliverable = (id) => data.deliverables.find((x) => x.id === id);
  const memberName = (id) => getMember(id).displayName || "项目成员";
  const name = (id) => `${memberName(id)}（${getMember(id).role}）`;
  const route = () => (location.hash || "#overview").slice(1).split("?")[0];
  const isOkrManagementMode = () => route() === "overview" && /(?:^|[?&])mode=okr(?:&|$)/.test((location.hash || "").split("?")[1] || "");
  const isLead = () => ["P01", "P02"].includes(ui.identity);
  const canCreate = () => !["P18", "P01"].includes(ui.identity);
  const canManage = () => ui.identity === "P02";
  const statusClass = (value) => {
    if (/高风险|阻塞|退回|作废/.test(value)) return "risk";
    if (/预警|待|等待|候选/.test(value)) return "warning";
    if (/审核|终审/.test(value)) return "review";
    if (/完成|通过|生效|就绪|已提供|已接收/.test(value)) return "normal";
    return "";
  };
  const status = (value, cls = "") => `<span class="status ${cls || statusClass(value)}">${esc(value)}</span>`;
  const entryApprovalText = (task) => `待${memberName(getKr(task?.krId)?.owner)}审批`;
  const taskStatusText = (task) => task?.status === "待入池审批" ? entryApprovalText(task) : task?.status || "—";
  const taskStatusChip = (task) => status(taskStatusText(task), statusClass(task?.status || ""));
  const riskReasonText = (risk) => risk?.reason === "任务等待入池审批" ? `任务${entryApprovalText(getTask(risk.taskId))}` : risk?.reason || "";
  const avatar = (id) => `<span class="avatar">${esc(memberName(id).slice(0, 1))}</span>`;
  const ownerCell = (id) => `<span class="owner-cell">${avatar(id)}<span>${esc(memberName(id))}</span></span>`;

  function init() {
    renderNav();
    renderIdentity();
    window.addEventListener("hashchange", renderPage);
    document.addEventListener("click", handleClick);
    document.addEventListener("input", handleInput);
    document.addEventListener("change", handleChange);
    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape") closeLayers();
      if (["Enter", " "].includes(event.key) && event.target.matches('.work-item[data-action]')) {
        event.preventDefault(); event.target.click();
      }
      if (["Enter", " "].includes(event.key) && event.target.matches('.okr-editable-row[data-action="edit-okr-row"]')) {
        event.preventDefault(); openOkrEditorDrawer(event.target.dataset.kind, event.target.dataset.id);
      }
      if (event.key === "Enter" && event.target.id === "graph-search") {
        const q = event.target.value.trim().toLowerCase();
        const match = [...data.tasks, ...data.krs, ...data.objectives].find((x) => `${x.id}${x.title}`.toLowerCase().includes(q));
        if (match) focusGraph(match.id); else toast("没有找到匹配节点。", "error");
      }
    });
    if (!location.hash) location.hash = "overview";
    renderPage();
    hydrateIcons();
  }

  function renderNav() {
    const current = route();
    $("#main-nav").innerHTML = routes.map(([id, label, iconName]) => `<button class="nav-row ${current === id ? "active" : ""}" data-route="${id}">${icon(iconName)}<span>${label}</span></button>`).join("");
  }
  function renderIdentity() {
    const member = getMember(ui.identity);
    $("#identity-button").innerHTML = `${avatar(member.id)}<span class="who"><b>${esc(member.displayName)}</b><small>${esc(member.role)} · ${esc(member.team)}</small></span>${icon("down")}`;
  }
  function renderPage() {
    closeLayers();
    renderNav();
    const current = route();
    if (isOkrManagementMode() && !canManage()) { location.hash = "overview"; return; }
    const label = isOkrManagementMode() ? "管理 O/KR" : ([...routes, ["settings", "项目设置"]].find((x) => x[0] === current) || [current, "项目总览"])[1];
    $("#breadcrumbs").innerHTML = `<span>${esc(data.project.name)}</span><span class="sep">/</span><b>${label}</b>`;
    const page = $("#page");
    page.className = `page page-${current}${isOkrManagementMode() ? " page-okr-management" : ""}`;
    const renderers = { overview: renderOverview, tasks: renderTasks, graph: renderGraph, mywork: renderMyWork, artifacts: renderArtifacts, reports: renderReports, settings: renderSettings };
    page.innerHTML = (renderers[current] || renderers.overview)();
    hydrateIcons(page);
    if (current === "graph" && ui.graphView === "graph") bindGraphInteractions();
    page.focus({ preventScroll: true });
  }

  function head(title, description, actions = "") {
    return `<div class="page-head"><div><h1>${title}</h1><p>${description}</p></div><div class="head-actions">${actions}</div></div>`;
  }

  function renderOverview() {
    if (isOkrManagementMode()) return renderOkrManagementPage();
    const activeTasks = data.tasks.filter((task) => task.status !== "已完成");
    const attentionKrs = data.krs.filter((kr) => kr.risk !== "normal");
    const highRiskKrs = data.krs.filter((kr) => kr.risk === "risk");
    const overviewBrief = `<section class="overview-brief" aria-label="项目态势摘要"><div class="overview-brief-copy"><h2>联调工作进入关键收敛期</h2><p>${attentionKrs.length} 个 KR 需要关注，其中 ${highRiskKrs.length} 个为高风险；建议优先处理硬前置输入与待审批事项。</p></div><dl class="overview-brief-facts"><div><dt>目标</dt><dd>${data.objectives.length}</dd></div><div><dt>关键结果</dt><dd>${data.krs.length}</dd></div><div><dt>活跃任务</dt><dd>${activeTasks.length}</dd></div><div class="attention"><dt>需关注 KR</dt><dd>${attentionKrs.length}</dd></div></dl></section>`;
    const objectives = data.objectives.map((objective) => {
      const krs = data.krs.filter((kr) => kr.objectiveId === objective.id);
      const objectiveTasks = data.tasks.filter((task) => krs.some((kr) => kr.id === task.krId));
      const completedTasks = objectiveTasks.filter((task) => task.status === "已完成").length;
      const objectiveAttention = krs.filter((kr) => kr.risk !== "normal").length;
      return `<section class="card objective">
        <div class="objective-head"><span class="objective-code">${objective.id}</span><div><h2>${esc(objective.title)}</h2><p>${esc(objective.note)}</p></div><div class="objective-head-meta"><span>${krs.length} 个 KR</span><small>${completedTasks}/${objectiveTasks.length} 任务完成${objectiveAttention ? ` · ${objectiveAttention} 个需关注` : ""}</small></div></div>
        ${krs.map((kr) => renderOverviewKr(kr)).join("")}
      </section>`;
    }).join("");
    const actions = `${canManage() ? `<button class="btn" data-action="open-okr-management">${icon("edit")}管理 O/KR</button>` : ""}<button class="btn" data-route="graph">${icon("graph")}查看协作全景</button>`;
    const projectMeta = `项目周期 ${esc(data.project.cycle)} · 主负责人 ${memberName(data.project.lead)} · 推进人 ${memberName(data.project.coordinator)}`;
    return `${head("项目总览", projectMeta, actions)}${overviewBrief}<div class="overview-list">${objectives}</div>`;
  }
  function renderOverviewKr(kr) {
    const expanded = ui.expandedKrs.has(kr.id);
    const tasks = data.tasks.filter((task) => task.krId === kr.id);
    const risk = data.risks.find((item) => tasks.some((task) => task.id === item.taskId));
    const riskNote = kr.risk === "normal" ? "" : risk ? `${riskReasonText(risk)} · ${risk.impact}` : "存在待处理的输入或审批卡点";
    const riskLabel = kr.risk === "risk" ? "风险因素" : "卡点";
    return `<div class="kr-row"><button class="kr-main" data-action="toggle-kr" data-id="${kr.id}"><span class="risk-stripe ${kr.risk}"></span><span class="kr-code">${kr.id}</span><span class="kr-title-cell"><span>${esc(kr.title)}</span>${riskNote ? `<small>${riskLabel}：${esc(riskNote)}</small>` : ""}</span>${status(kr.risk === "risk" ? "高风险" : kr.risk === "warning" ? "预警" : "正常", kr.risk)}<span>${icon(expanded ? "down" : "chevron")}</span></button>
      ${expanded ? `<div class="kr-tasks"><div class="kr-context"><span>负责人 ${memberName(kr.owner)}</span><span>周期 ${kr.cycle}</span><span>量化标准 ${esc(kr.metric)}</span></div><div class="mini-task mini-task-head" aria-hidden="true"><span>编号</span><span>任务</span><span>负责人</span><span>状态</span><span class="optional">进度</span><span></span></div>${tasks.map((task) => `<div class="mini-task"><span class="mono">${task.id}</span><span>${esc(task.title)}</span><span>${ownerCell(task.owner)}</span><span>${taskStatusChip(task)}</span><span class="optional">${task.progress == null ? "未填写" : `${task.progress}%`}</span><button class="icon-btn" data-action="task-detail" data-id="${task.id}" aria-label="查看任务">${icon("chevron")}</button></div>`).join("")}<div class="kr-graph-link"><button class="link-btn" data-action="focus-graph" data-id="${kr.id}">在协作全景中查看 ${kr.id} 影响链 →</button></div></div>` : ""}</div>`;
  }

  function renderOkrManagementPage() {
    const editAttrs = (kind, id) => `data-action="edit-okr-row" data-kind="${kind}" data-id="${id}" tabindex="0" aria-label="编辑 ${id}"`;
    const rows = data.objectives.map((objective) => {
      const objectiveOwner = objective.owner || data.project.lead;
      const krs = data.krs.filter((kr) => kr.objectiveId === objective.id);
      return `<tr ${editAttrs("objective", objective.id)} class="okr-structure-o okr-editable-row"><td><span class="okr-level-tag">O</span><span class="mono">${objective.id}</span></td><td><strong>${esc(objective.title)}</strong></td><td>—</td><td>${ownerCell(objectiveOwner)}</td><td>${esc(data.project.cycle)}</td><td>—</td><td><span class="okr-row-action">编辑</span></td></tr>${krs.map((kr) => `<tr ${editAttrs("kr", kr.id)} class="okr-structure-kr okr-editable-row"><td><span class="okr-level-branch" aria-hidden="true"></span><span class="okr-level-tag kr">KR</span><span class="mono">${kr.id}</span></td><td>${esc(kr.title)}</td><td>${esc(objective.id)}</td><td>${ownerCell(kr.owner)}</td><td>${esc(kr.cycle)}</td><td>${esc(kr.metric)}</td><td><span class="okr-row-action">编辑</span></td></tr>`).join("")}`;
    }).join("");
    const actions = `<button class="btn" data-action="back-overview">${icon("back")}返回项目总览</button><button class="btn" data-action="import-okr">${icon("upload")}导入已有表格</button><button class="btn primary" data-action="add-okr">${icon("plus")}新增 O / KR</button>`;
    return `${head("管理 O/KR", "集中维护目标结构和责任信息；项目态势、风险与任务进度仍在项目总览查看。", actions)}
      <section class="okr-management-note"><div><b>结构维护模式</b><span>点击任一 O 或 KR 行，在右侧编辑对应结构字段。</span></div><span>${data.objectives.length} 个 O · ${data.krs.length} 个 KR</span></section>
      <div class="data-table-wrap okr-structure-wrap"><table class="data-table okr-structure-table"><thead><tr><th>编号</th><th>名称</th><th>所属 O</th><th>负责人</th><th>周期</th><th>量化标准</th><th><span class="sr-only">操作</span></th></tr></thead><tbody>${rows}</tbody></table></div>`;
  }

  function renderTasks() {
    const search = ui.taskSearch.toLowerCase();
    const filtered = data.tasks.filter((task) => (ui.taskKr === "all" || task.krId === ui.taskKr) && (ui.taskStatus === "all" || task.status === ui.taskStatus) && (!search || `${task.id}${task.title}${task.owner}${memberName(task.owner)}`.toLowerCase().includes(search)));
    const groups = data.krs.map((kr) => ({ kr, tasks: filtered.filter((task) => task.krId === kr.id) })).filter((group) => group.tasks.length);
    const rows = groups.map(({ kr, tasks }) => `<tr class="table-group"><td colspan="9"><div class="task-group-label"><span><b>${kr.id}</b>${esc(kr.title)}</span><span class="meta">${tasks.length} 项任务</span></div></td></tr>${tasks.map((task) => `<tr class="task-table-row"><td><span class="tree-indent"><span class="mono">${task.id}</span></span></td><td><button class="link-btn task-title-link" data-action="task-detail" data-id="${task.id}">${esc(task.title)}</button></td><td>${ownerCell(task.owner)}</td><td>${taskStatusChip(task)}</td><td>${task.progress == null ? `<span class="meta">未填写</span>` : `<div class="task-progress-cell"><div class="progress" aria-label="进度 ${task.progress}%"><i style="width:${task.progress}%"></i></div><span class="meta">${task.progress}%</span></div>`}</td><td class="task-date">${task.start}</td><td class="task-date">${task.due}</td><td class="task-output">${esc(task.outputName)}</td><td><button class="icon-btn task-row-open" data-action="task-detail" data-id="${task.id}" aria-label="打开 ${esc(task.title)} 详情">${icon("chevron")}</button></td></tr>`).join("")}`).join("");
    const taskStatusOptions = [["未开始","未开始"],["进行中","进行中"],["等待输入","等待输入"],["待入池审批","待负责人审批"],["待中间审核","待中间审核"],["待 KR 终审","待 KR 终审"],["已完成","已完成"]];
    return `${head("全部任务", "按 O / KR 组织三级任务；任务创建后先提交所属 KR 负责人审批，通过后进入执行池。", `${canCreate() ? `<button class="btn" data-action="invite-owner">${icon("user")}邀请负责人完善</button><button class="btn primary" data-action="create-task">${icon("plus")}创建任务</button>` : ""}`)}
      <div class="toolbar"><div class="toolbar-group"><label class="search-box">${icon("search")}<input id="task-search" value="${esc(ui.taskSearch)}" placeholder="搜索任务、编号或负责人" aria-label="搜索任务" /></label><select id="task-kr" class="field" aria-label="按 KR 筛选"><option value="all">全部 KR</option>${data.krs.map((kr) => `<option value="${kr.id}" ${ui.taskKr === kr.id ? "selected" : ""}>${kr.id}</option>`).join("")}</select><select id="task-status" class="field" aria-label="按任务状态筛选"><option value="all">全部状态</option>${taskStatusOptions.map(([value,label]) => `<option value="${value}" ${ui.taskStatus === value ? "selected" : ""}>${label}</option>`).join("")}</select></div></div>
      <div class="data-table-wrap task-table-wrap"><table class="data-table task-table"><thead><tr><th>编号</th><th>任务</th><th>负责人</th><th>状态</th><th>进度</th><th>开始</th><th>截止</th><th>预期交付物</th><th><span class="sr-only">操作</span></th></tr></thead><tbody>${rows || `<tr><td colspan="9"><div class="empty task-empty"><b>没有匹配任务</b><span>请调整关键词或筛选条件</span></div></td></tr>`}</tbody></table></div>`;
  }

  function renderGraph() {
    const viewSwitch = `<div class="segment"><button class="${ui.graphView === "graph" ? "active" : ""}" data-action="graph-view" data-value="graph">关系图谱</button><button class="${ui.graphView === "list" ? "active" : ""}" data-action="graph-view" data-value="list">关系列表</button></div>`;
    const modeSwitch = `<div class="segment"><button class="${ui.graphMode === "aggregate" ? "active" : ""}" data-action="graph-mode" data-value="aggregate">KR 聚合</button><button class="${ui.graphMode === "full" ? "active" : ""}" data-action="graph-mode" data-value="full">全局展开</button><button class="${ui.graphMode === "focus" ? "active" : ""}" data-action="graph-mode" data-value="focus">选中聚焦</button></div>`;
    return `${head("协作关系", "交付物作为关系边连接来源和目标；图谱可承载跨 KR、跨 O、互锁和反馈循环。", `<button class="btn" data-action="graph-back" ${ui.graphHistory.length ? "" : "disabled"}>${icon("back")}返回上一级</button><button class="btn primary" data-action="graph-mode" data-value="${ui.graphMode === "full" ? "aggregate" : "full"}">${icon("expand")}${ui.graphMode === "full" ? "收起到 KR" : "全局展开"}</button>`)}
      <div class="toolbar"><div class="toolbar-group">${viewSwitch}${ui.graphView === "graph" ? modeSwitch : ""}<label class="search-box">${icon("search")}<input id="graph-search" value="${esc(ui.graphSearch)}" placeholder="搜索后按 Enter 定位节点" /></label></div><div class="toolbar-actions"><select class="field" id="graph-o"><option value="all">全部 O</option>${data.objectives.map((o)=>`<option value="${o.id}" ${ui.graphO===o.id?"selected":""}>${o.id}</option>`).join("")}</select><select class="field" id="graph-kr"><option value="all">全部 KR</option>${data.krs.filter((kr)=>ui.graphO==="all"||kr.objectiveId===ui.graphO).map((kr)=>`<option value="${kr.id}" ${ui.graphKr===kr.id?"selected":""}>${kr.id}</option>`).join("")}</select><select class="field" id="graph-person"><option value="all">全部人员</option>${data.members.map((m)=>`<option value="${m.id}" ${ui.graphPerson===m.id?"selected":""}>${m.displayName}</option>`).join("")}</select><select class="field" id="graph-risk"><option value="all">全部风险</option><option value="warning" ${ui.graphRisk==="warning"?"selected":""}>预警</option><option value="risk" ${ui.graphRisk==="risk"?"selected":""}>高风险</option></select><select class="field" id="graph-relation"><option value="all">全部关系</option><option value="hard" ${ui.graphRelation==="hard"?"selected":""}>硬依赖</option><option value="input" ${ui.graphRelation==="input"?"selected":""}>输入关系</option><option value="interlock" ${ui.graphRelation==="interlock"?"selected":""}>互锁风险</option><option value="feedback" ${ui.graphRelation==="feedback"?"selected":""}>反馈循环</option></select><select class="field" id="graph-time"><option value="active" ${ui.graphTime==="active"?"selected":""}>进行中与风险</option><option value="all" ${ui.graphTime==="all"?"selected":""}>全部时间</option><option value="done" ${ui.graphTime==="done"?"selected":""}>已完成历史</option></select></div></div>
      ${ui.graphView === "list" ? renderRelationList() : renderGraphCanvas()}`;
  }

  function graphModel() {
    const taskVisible = (task) => {
      if (ui.graphTime === "active" && task.status === "已完成") return false;
      if (ui.graphTime === "done" && task.status !== "已完成") return false;
      if (ui.graphKr !== "all" && task.krId !== ui.graphKr) return false;
      const kr = getKr(task.krId);
      if (ui.graphO !== "all" && kr?.objectiveId !== ui.graphO) return false;
      if (ui.graphPerson !== "all" && task.owner !== ui.graphPerson && !task.participants.includes(ui.graphPerson)) return false;
      if (ui.graphRisk !== "all") {
        const hasRisk = data.risks.some((risk) => risk.taskId === task.id && (ui.graphRisk !== "risk" || risk.level === "高风险"));
        if (!hasRisk) return false;
      }
      return true;
    };
    const relationVisible = (rel) => ui.graphRelation === "all" || rel.type === ui.graphRelation;
    if (ui.graphMode === "aggregate") {
      const centers = { O1: { x: 250, y: 335 }, O2: { x: 650, y: 335 }, O3: { x: 1050, y: 335 } };
      const nodes = [];
      data.objectives.forEach((objective) => {
        if (ui.graphO !== "all" && ui.graphO !== objective.id) return;
        const c = centers[objective.id];
        const visibleKrs = data.krs.filter((kr) => kr.objectiveId === objective.id && (ui.graphKr === "all" || ui.graphKr === kr.id) && (ui.graphRisk === "all" || kr.risk === ui.graphRisk) && data.tasks.some((task)=>task.krId===kr.id&&taskVisible(task)));
        if (!visibleKrs.length) return;
        nodes.push({ id: objective.id, label: objective.id, subtitle: objective.title, type: "objective", x: c.x, y: c.y, risk: "normal" });
        visibleKrs.forEach((kr, index) => {
          const angle = (-Math.PI / 2) + (index * Math.PI * 2 / visibleKrs.length);
          nodes.push({ id: kr.id, label: kr.id, subtitle: kr.title, type: "kr", x: c.x + Math.cos(angle) * 145, y: c.y + Math.sin(angle) * 145, risk: kr.risk });
        });
      });
      const byPair = new Map();
      data.relations.filter(relationVisible).forEach((rel) => {
        const from = getTask(rel.from), to = getTask(rel.to);
        if (!from || !to || !taskVisible(from) || !taskVisible(to) || from.krId === to.krId) return;
        const key = `${from.krId}:${to.krId}:${rel.type}`;
        if (!byPair.has(key)) byPair.set(key, { ...rel, from: from.krId, to: to.krId, count: 0 });
        byPair.get(key).count += 1;
      });
      const edges = [...byPair.values()].map((rel) => ({ ...rel, label: `${rel.label}${rel.count > 1 ? ` · ${rel.count}条` : ""}` }));
      data.krs.forEach((kr) => edges.push({ id: `owns-${kr.id}`, from: kr.objectiveId, to: kr.id, type: "owns", label: "" }));
      return { nodes, edges };
    }

    const centerMap = { KR1:[170,130], KR2:[460,130], KR3:[760,130], KR4:[1050,130], KR5:[240,390], KR6:[560,390], KR7:[880,390], KR8:[350,650], KR9:[760,650], KR10:[1080,650] };
    let taskIds = data.tasks.filter(taskVisible).map((t) => t.id);
    let edges = data.relations.filter(relationVisible).filter((rel)=>taskIds.includes(rel.from)&&taskIds.includes(rel.to));
    if (ui.graphMode === "focus") {
      const selected = ui.graphSelected;
      const selectedTask = getTask(selected);
      const baseIds = selectedTask ? [selected] : data.tasks.filter((t) => t.krId === selected).map((t) => t.id);
      const oneHop = new Set(baseIds);
      edges.forEach((edge) => { if (baseIds.includes(edge.from) || baseIds.includes(edge.to)) { oneHop.add(edge.from); oneHop.add(edge.to); } });
      taskIds = [...oneHop];
      edges = edges.filter((edge) => oneHop.has(edge.from) && oneHop.has(edge.to));
    }
    const nodes = [];
    const includedKrs = new Set(taskIds.map((id) => getTask(id)?.krId).filter(Boolean));
    const objectivePositions = { O1:[340,35], O2:[680,285], O3:[650,570] };
    const includedObjectives = new Set([...includedKrs].map((krId)=>getKr(krId)?.objectiveId).filter(Boolean));
    includedObjectives.forEach((objectiveId)=>{const o=getObjective(objectiveId),p=objectivePositions[objectiveId];nodes.push({id:o.id,label:o.id,subtitle:o.title,type:"objective",x:p[0],y:p[1],risk:"normal"})});
    includedKrs.forEach((krId, index) => {
      const kr = getKr(krId);
      let center = centerMap[krId] || [180 + (index % 3) * 350, 180 + Math.floor(index / 3) * 260];
      if (ui.graphMode === "focus" && ui.graphSelected === krId) center = [620, 340];
      nodes.push({ id: krId, label: krId, subtitle: kr.title, type: "kr", x: center[0], y: center[1], risk: kr.risk });
      const krTasks = taskIds.map(getTask).filter((t) => t && t.krId === krId);
      krTasks.forEach((task, taskIndex) => {
        const angle = (-Math.PI / 2) + (taskIndex * Math.PI * 2 / Math.max(krTasks.length, 3));
        nodes.push({ id: task.id, label: task.id, subtitle: task.title, type: "task", x: center[0] + Math.cos(angle) * 96, y: center[1] + Math.sin(angle) * 96, risk: /等待|待/.test(task.status) ? "warning" : /阻塞/.test(task.status) ? "risk" : "normal" });
      });
    });
    const internalEdges = [];
    includedKrs.forEach((krId)=>internalEdges.push({id:`objective-${krId}`,from:getKr(krId).objectiveId,to:krId,type:"owns",label:""}));
    nodes.filter((n) => n.type === "task").forEach((n) => internalEdges.push({ id: `group-${n.id}`, from: getTask(n.id).krId, to: n.id, type: "owns", label: "" }));
    const inputNodes = ui.graphMode === "full" ? data.inputRequests.filter((r)=>taskIds.includes(r.taskId)&&(ui.graphPerson==="all"||r.provider===ui.graphPerson)).map((r, index) => ({ id: r.id, label: memberName(r.provider), subtitle: r.inputName, type: "person", x: 1040 + (index * 170), y: 620, risk: r.state === "待接收" ? "warning" : "normal" })) : [];
    inputNodes.forEach((n) => internalEdges.push({ id: `request-${n.id}`, from: n.id, to: data.inputRequests.find((r) => r.id === n.id).taskId, type: "input", label: "人工输入请求" }));
    const riskNodes = ui.graphMode === "full" ? data.risks.filter((risk)=>taskIds.includes(risk.taskId)).map((risk,index)=>({id:risk.id,label:risk.id,subtitle:riskReasonText(risk),type:"risk",x:1180,y:120+index*105,risk:"risk"})) : [];
    riskNodes.forEach((n)=>internalEdges.push({id:`risk-${n.id}`,from:n.id,to:data.risks.find((r)=>r.id===n.id).taskId,type:"interlock",label:"卡点影响"}));
    return { nodes: [...nodes, ...inputNodes, ...riskNodes], edges: [...internalEdges, ...edges] };
  }

  function renderGraphCanvas() {
    const model = graphModel();
    const nodeById = Object.fromEntries(model.nodes.map((n) => [n.id, n]));
    const edgeMarkup = model.edges.map((edge) => {
      const from = nodeById[edge.from], to = nodeById[edge.to];
      if (!from || !to) return "";
      const cls = edge.type === "hard" ? "hard" : edge.type === "interlock" ? "interlock" : edge.type === "feedback" ? "feedback" : "";
      const midX = (from.x + to.x) / 2, midY = (from.y + to.y) / 2;
      return `<g><path class="graph-edge ${cls}" d="M ${from.x} ${from.y} L ${to.x} ${to.y}"/><text class="graph-edge-label" x="${midX}" y="${midY - 5}" text-anchor="middle">${esc(edge.label || "")}</text></g>`;
    }).join("");
    const nodeMarkup = model.nodes.map((node) => {
      const fill = node.type === "objective" ? "#214f69" : node.type === "kr" ? "#6e91a4" : node.type === "person" ? "#766aa8" : node.type === "risk" ? "#fff0f1" : "#eff4f6";
      const textFill = ["objective","kr","person"].includes(node.type) ? "#fff" : "#314353";
      const label = node.type === "task" ? `${node.label} ${node.subtitle.slice(0, 9)}` : node.label;
      return `<g class="node ${node.risk || ""} ${ui.graphSelected === node.id ? "selected" : ""}" data-node-id="${node.id}"><circle cx="${node.x}" cy="${node.y}" r="${node.type === "objective" ? 34 : node.type === "kr" ? 28 : 21}" fill="${fill}"/><text x="${node.x}" y="${node.y + 4}" text-anchor="middle" style="fill:${textFill};stroke:${fill}">${esc(node.label)}</text><text x="${node.x}" y="${node.y + (node.type === "task" ? 40 : 48)}" text-anchor="middle">${esc(label === node.label ? node.subtitle.slice(0, 12) : label)}</text></g>`;
    }).join("");
    const selected = nodeById[ui.graphSelected] || model.nodes[0];
    return `<div class="graph-shell"><div id="graph-canvas" class="graph-canvas"><div class="graph-note">${ui.graphMode === "aggregate" ? "默认聚合到 KR，避免复杂网络一次铺满" : ui.graphMode === "full" ? `已展开 ${model.nodes.length} 个节点、${model.edges.length} 条关系` : "仅显示选中节点及相邻一层"}</div><svg id="graph-svg" viewBox="0 0 1300 780" role="img" aria-label="协作关系图谱"><defs><marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse"><path d="M0 0 10 5 0 10Z" fill="#748d9c"/></marker></defs><g id="graph-stage" transform="translate(${ui.graphPan.x} ${ui.graphPan.y}) scale(${ui.graphZoom})">${edgeMarkup}${nodeMarkup}</g></svg><div class="graph-controls"><button class="icon-btn" data-action="graph-zoom" data-value="in" aria-label="放大">${icon("zoomin")}</button><button class="icon-btn" data-action="graph-zoom" data-value="out" aria-label="缩小">${icon("zoomout")}</button><button class="icon-btn" data-action="graph-fit" aria-label="适应屏幕">${icon("fit")}</button></div><div class="minimap"><svg viewBox="0 0 1300 780">${model.nodes.map((n) => `<circle cx="${n.x}" cy="${n.y}" r="12" fill="${n.risk === "risk" ? "#bd3e49" : n.type === "kr" ? "#6e91a4" : "#b6c7d1"}"/>`).join("")}<rect x="10" y="10" width="1280" height="760" fill="none" stroke="#395d71" stroke-width="10"/></svg></div></div>${renderGraphInspector(selected, model)}</div>`;
  }

  function renderGraphInspector(node, model) {
    if (!node) return `<aside class="graph-inspector"><div class="empty">选择节点查看属性</div></aside>`;
    const task = getTask(node.id), kr = getKr(node.id), objective = getObjective(node.id);
    const related = model.edges.filter((e) => e.from === node.id || e.to === node.id);
    let title = node.subtitle, kind = node.type === "task" ? "任务节点" : node.type === "kr" ? "KR 聚合节点" : node.type === "objective" ? "目标 O" : node.type === "risk" ? "卡点节点" : "人工输入提供人";
    return `<aside class="graph-inspector"><div class="inspector-head"><div><span class="pill">${kind}</span><h3 style="margin:8px 0 0">${esc(title)}</h3></div><button class="icon-btn" data-action="clear-graph-selection" aria-label="清除选择">${icon("close")}</button></div><div class="inspector-body">
      <div class="property"><label>节点编号</label><strong>${node.id}</strong></div>
      ${task ? `<div class="property"><label>负责人 / 状态</label><strong>${name(task.owner)}　${taskStatusChip(task)}</strong></div><div class="property"><label>预期交付物</label><strong>${esc(task.outputName)}</strong></div><div class="property"><label>周期</label><strong>${task.start} — ${task.due}</strong></div>` : ""}
      ${kr ? `<div class="property"><label>负责人 / 风险</label><strong>${name(kr.owner)}　${status(kr.risk === "risk" ? "高风险" : kr.risk === "warning" ? "预警" : "正常", kr.risk)}</strong></div><div class="property"><label>量化指标</label><strong>${esc(kr.metric)}</strong></div>` : ""}
      ${objective ? `<div class="property"><label>项目目标</label><strong>${esc(objective.note)}</strong></div>` : ""}
      <div class="property"><label>直接关联</label><strong>${related.length} 条</strong></div>
      <div style="display:grid;gap:7px;margin-top:14px"><button class="btn primary" data-action="graph-focus" data-id="${node.id}">${icon("expand")}向外逐层展开</button>${task ? `<button class="btn" data-action="task-detail" data-id="${task.id}">打开任务详情</button>` : ""}</div>
      <div class="drawer-section" style="margin-top:18px"><h3>关系与交付物边</h3>${related.slice(0,6).map((edge) => `<div class="side-item"><strong>${edge.from} → ${edge.to}</strong><p>${esc(edge.label || "隶属关系")}</p></div>`).join("") || `<p class="meta">暂无直接关系</p>`}</div>
    </div></aside>`;
  }

  function renderRelationList() {
    const rows = data.relations.map((rel) => {
      const from = getTask(rel.from), to = getTask(rel.to), deliverable = data.deliverables.find((d) => d.relationId === rel.id);
      const type = rel.type === "hard" ? "硬依赖" : rel.type === "interlock" ? "互锁" : rel.type === "feedback" ? "反馈循环" : "输入关系";
      return `<tr><td>${rel.id}</td><td><button class="link-btn" data-action="focus-graph" data-id="${rel.from}">${rel.from} ${esc(from?.title || "")}</button></td><td>→</td><td><button class="link-btn" data-action="focus-graph" data-id="${rel.to}">${rel.to} ${esc(to?.title || "")}</button></td><td>${status(type, rel.type === "interlock" ? "risk" : rel.type === "feedback" ? "review" : "")}</td><td>${esc(deliverable?.name || rel.label)}</td><td>${status(deliverable?.state || rel.state)}</td></tr>`;
    }).join("");
    return `<div class="data-table-wrap"><table class="data-table"><thead><tr><th>关系</th><th>来源节点</th><th></th><th>目标节点</th><th>类型</th><th>关系上的交付物</th><th>当前内容状态</th></tr></thead><tbody>${rows}</tbody></table></div>`;
  }

  const WORK_TODAY = 918;
  const workDateRank = (value) => { const match=String(value || "").match(/(\d{2})-(\d{2})/); return match ? Number(match[1]) * 100 + Number(match[2]) : 9999; };
  const necessaryRelation = (relation) => relation.necessity !== "参考";
  const approvalTimeout = () => Number(data.project.approvalTimeoutDays || 3);
  function completeWorkItem(item) {
    const dueRank=workDateRank(item.due), approvalLate=item.type === "approval" && Number(item.waitingDays || 0) >= approvalTimeout();
    item.approvalOverdue=approvalLate;
    item.overdue=Boolean(item.overdue || approvalLate || (item.type!=="approval" && dueRank < WORK_TODAY && !["receipt","invite"].includes(item.type)));
    item.todayDue=dueRank === WORK_TODAY;
    item.sortDue=dueRank;
    item.blocked=Boolean(item.blocked);
    return item;
  }
  function sortWorkItems(items) {
    const unique=[...new Map(items.map((item)=>[item.key,item])).values()];
    return unique.sort((a,b)=>Number(b.overdue)-Number(a.overdue) || Number(b.todayDue)-Number(a.todayDue) || a.sortDue-b.sortDue || Number(b.waitingDays || 0)-Number(a.waitingDays || 0) || Number(a.blocked)-Number(b.blocked) || a.title.localeCompare(b.title,"zh-CN"));
  }
  function workApprovalItems(id) {
    return [
      ...data.entryApprovals.filter((x)=>x.krOwner===id && x.state==="待审批").map((x)=>({key:`entry:${x.id}`,type:"approval",kind:"入",taskId:x.taskId,recordId:x.id,title:`${entryApprovalText(getTask(x.taskId))} · ${x.taskId} ${getTask(x.taskId)?.title || ""}`,why:"你是所属 KR 负责人，需要判断任务是否进入执行池",need:x.reason,impact:"通过后进入执行池；退回则返回创建人",due:getTask(x.taskId)?.due,waitingDays:x.waitingDays,tab:"audit",focusType:"approval",focusId:x.id})),
      ...data.changeRequests.filter((x)=>x.krOwner===id && x.state==="待审批").map((x)=>({key:`change:${x.id}`,type:"approval",kind:"变",taskId:x.taskId,recordId:x.id,title:`关键字段变更 · ${x.taskId} ${getTask(x.taskId)?.title || ""}`,why:`${x.field}：${x.oldValue} → ${x.newValue}，旧值继续生效`,need:x.reason,impact:"通过后新值生效；退回后形成提交人的待处理事项",due:getTask(x.taskId)?.due,waitingDays:x.waitingDays,tab:"audit",focusType:"approval",focusId:x.id})),
      ...data.completionApprovals.flatMap((x)=>x.reviewers.filter((r)=>r.person===id && r.state==="待审核" && x.state==="中间审核中").map(()=>({key:`middle:${x.id}:${id}`,type:"approval",kind:"审",taskId:x.taskId,recordId:x.id,title:`中间审核 · ${x.taskId} ${getTask(x.taskId)?.title || ""}`,why:"你是本轮或签审核人，任一人处理后其他待办自动关闭",need:`核对 ${completionCandidates(x).map((item)=>item.name).join("、") || "候选交付物"}`,impact:"通过后进入 KR 终审；退回则整体返回提交人",due:getTask(x.taskId)?.due,waitingDays:x.waitingDays,tab:"audit",focusType:"approval",focusId:x.id}))),
      ...data.completionApprovals.filter((x)=>x.krOwner===id && x.state==="待 KR 终审").map((x)=>({key:`final:${x.id}`,type:"approval",kind:"终",taskId:x.taskId,recordId:x.id,title:`KR 终审 · ${x.taskId} ${getTask(x.taskId)?.title || ""}`,why:"你是所属 KR 负责人，这是任务完成的唯一硬闭环",need:`核对 ${completionCandidates(x).map((item)=>item.name).join("、") || "候选交付物"} 与完成标准`,impact:"通过后任务完成并向接收方生成待接收项",due:getTask(x.taskId)?.due,waitingDays:x.waitingDays,tab:"audit",focusType:"approval",focusId:x.id}))
    ].map(completeWorkItem);
  }
  function workItems(tab) {
    const id=ui.identity;
    if (tab === "approvals") return sortWorkItems(workApprovalItems(id));
    if (tab === "receive") return sortWorkItems(data.deliverables.filter((item)=>item.receiver===id && item.state==="已生效" && item.receiptState!=="已确认").map((item)=>completeWorkItem({key:`receipt:${item.id}`,type:"receipt",kind:"收",taskId:item.taskId,title:`待接收 · ${item.taskId} ${item.name}`,why:"你是该任务当前交付物的接收方",need:`查看 ${item.fileType || "文件"} · ${item.fileSize || "—"} 后确认接收`,impact:"确认后形成接收记录，不改变任务审核结果",waitingDays:0,tab:"overview",focusType:"receipt",focusId:item.id})));
    if (tab === "waiting") {
      const inputWait=data.inputRequests.filter((x)=>x.requester===id && x.necessity!=="参考" && ["待接收","已接收"].includes(x.state)).map((x)=>completeWorkItem({key:`input-wait:${x.id}`,type:"waiting",kind:"等",taskId:x.taskId,title:`等待 ${memberName(x.provider)} · ${x.inputName}`,why:`你发起的必要输入请求当前为“${x.state}”`,need:`期望 ${x.due} 前提供`,impact:x.impact,due:x.due,waitingDays:x.waitingDays,blocked:true,tab:"overview",focusType:"waiting-input",focusId:x.id,reminderTarget:x.provider}));
      const upstream=data.tasks.filter((task)=>task.owner===id).flatMap((task)=>data.relations.filter((rel)=>rel.to===task.id && necessaryRelation(rel) && rel.state!=="已就绪").map((rel)=>{const source=getTask(rel.from),actionOwner=source?taskStage(source).actor:null;return completeWorkItem({key:`upstream:${rel.id}`,type:"waiting",kind:"等",taskId:task.id,title:`等待上游 · ${source?.id || "—"} ${source?.title || rel.label}`,why:`你的任务 ${task.id} 有必要输入尚未就绪`,need:`${rel.label} · 当前${rel.state}`,impact:`阻塞 ${task.title}`,due:source?.due,waitingDays:1,blocked:true,tab:"overview",focusType:"upstream",focusId:rel.id,reminderTarget:actionOwner});}));
      const entry=data.entryApprovals.filter((x)=>x.submitter===id && x.state==="待审批").map((x)=>completeWorkItem({key:`entry-wait:${x.id}`,type:"waiting",kind:"等",taskId:x.taskId,title:`${entryApprovalText(getTask(x.taskId))} · ${x.taskId} ${getTask(x.taskId)?.title || ""}`,why:`你已提交任务，当前由 ${memberName(x.krOwner)} 处理`,need:"审批通过前任务不进入执行池",impact:"通过后任务进入未开始",due:getTask(x.taskId)?.due,waitingDays:x.waitingDays,tab:"overview",focusType:"waiting-approval",focusId:x.id,reminderTarget:x.krOwner}));
      const changes=data.changeRequests.filter((x)=>x.submitter===id && x.state==="待审批").map((x)=>completeWorkItem({key:`change-wait:${x.id}`,type:"waiting",kind:"等",taskId:x.taskId,title:`等待变更审批 · ${x.taskId} ${getTask(x.taskId)?.title || ""}`,why:`${x.field}修改已提交，旧值继续生效`,need:`当前停在 ${memberName(x.krOwner)}`,impact:"通过后新值生效；退回后形成待处理事项",due:getTask(x.taskId)?.due,waitingDays:x.waitingDays,tab:"overview",focusType:"waiting-approval",focusId:x.id,reminderTarget:x.krOwner}));
      const completions=data.completionApprovals.filter((x)=>x.submitter===id && ["中间审核中","待 KR 终审"].includes(x.state)).map((x)=>{const stage=taskStage(getTask(x.taskId));return completeWorkItem({key:`completion-wait:${x.id}`,type:"waiting",kind:"等",taskId:x.taskId,title:`等待完成审核 · ${x.taskId} ${getTask(x.taskId)?.title || ""}`,why:`当前环节：${x.state}`,need:`当前停在 ${memberName(stage.actor)}`,impact:"KR 终审通过后候选交付物才成为当前内容",due:getTask(x.taskId)?.due,waitingDays:x.waitingDays,tab:"overview",focusType:"waiting-approval",focusId:x.id,reminderTarget:stage.actor});});
      return sortWorkItems([...inputWait,...upstream,...entry,...changes,...completions]);
    }
    if (tab === "risks") {
      const actual=data.risks.filter((risk)=>{const task=getTask(risk.taskId),kr=getKr(task?.krId),duplicate=data.inputRequests.some((request)=>request.taskId===risk.taskId && request.provider===id && request.state!=="已提供");return (risk.actionOwner===id || task?.owner===id || kr?.owner===id) && !(duplicate && risk.actionOwner===id);}).map((risk)=>{const task=getTask(risk.taskId);return completeWorkItem({key:`risk:${risk.id}`,type:"risk",kind:"险",taskId:risk.taskId,title:`${risk.level} · ${risk.taskId} ${riskReasonText(risk)}`,why:`你是待行动人、任务负责人或所属 KR 负责人；已持续 ${risk.days} 天`,need:risk.impact,impact:"解除条件满足后系统自动关闭，并同步关系图谱",due:task?.due,waitingDays:risk.days,overdue:risk.level==="高风险",tab:"overview",focusType:"risk",focusId:risk.id,reminderTarget:risk.actionOwner});});
      const timeout=workApprovalItems(id).filter((item)=>item.approvalOverdue).map((item)=>completeWorkItem({...item,key:`timeout:${item.key}`,type:"risk",kind:"险",title:`审批超时 · ${item.title}`,why:`当前审批环节已等待 ${item.waitingDays} 天，超过项目阈值 ${approvalTimeout()} 天`,need:"请在审核 Tab 处理当前审批件",impact:"处理或关闭审批后卡点自动解除",overdue:true,tab:"overview",focusType:"risk-timeout",focusId:item.recordId,reminderTarget:id}));
      return sortWorkItems([...actual,...timeout]);
    }
    const pendingCompletion=new Set(data.completionApprovals.filter((x)=>["中间审核中","待 KR 终审"].includes(x.state)).map((x)=>x.taskId));
    const owned=data.tasks.filter((task)=>task.owner===id && ["未开始","进行中","等待输入"].includes(task.status) && !pendingCompletion.has(task.id)).map((task)=>{const missingRelations=data.relations.filter((rel)=>rel.to===task.id && necessaryRelation(rel) && rel.state!=="已就绪");const missingRequests=data.inputRequests.filter((request)=>request.taskId===task.id && request.necessity!=="参考" && request.state!=="已提供");const missing=[...missingRelations.map((rel)=>getTask(rel.from)?.title || rel.label),...missingRequests.map((request)=>request.inputName)];return completeWorkItem({key:`task:${task.id}`,type:"task",kind:"办",taskId:task.id,title:`${task.id} ${task.title}`,why:`你是任务负责人 · 当前状态 ${taskStatusText(task)}`,need:missing.length?`上游未就绪：缺 ${missing.join("、")}`:`需要形成：${task.outputName}`,impact:`完成后交给 ${memberName(task.receiver)}`,due:task.due,blocked:missing.length>0,tab:"overview",focusType:"task",focusId:task.id});});
    const inputs=data.inputRequests.filter((request)=>request.provider===id && request.necessity!=="参考" && ["待接收","已接收"].includes(request.state)).map((request)=>completeWorkItem({key:`input:${request.id}`,type:"input",kind:"输",taskId:request.taskId,title:`输入请求 · ${request.taskId} ${request.inputName}`,why:`${memberName(request.requester)} 指定你提供必要输入`,need:`${request.state} · 期望 ${request.due} 前提供`,impact:request.impact,due:request.due,waitingDays:request.waitingDays,tab:"overview",focusType:"input",focusId:request.id}));
    const rejected=data.changeRequests.filter((request)=>request.submitter===id && request.state==="已退回" && !request.dismissed).map((request)=>completeWorkItem({key:`rejected-change:${request.id}`,type:"rejected",kind:"退",taskId:request.taskId,title:`退回待处理 · ${request.taskId} ${request.field}变更`,why:`已退回：${request.opinion || "请修改后重新提交"}`,need:`补充后重新提交，或明确放弃本次变更`,impact:"仅查看不会清除该事项；任务继续使用旧值",due:getTask(request.taskId)?.due,waitingDays:request.waitingDays,tab:"overview",focusType:"rejection",focusId:request.id}));
    const invites=(data.taskInvites || []).filter((invite)=>invite.invitee===id && invite.state==="待处理").map((invite)=>completeWorkItem({key:`invite:${invite.id}`,type:"invite",kind:"邀",taskId:"",inviteId:invite.id,title:`任务创建邀请 · ${invite.krId}`,why:`${memberName(invite.inviter)} 邀请你补充任务`,need:invite.note,impact:"通过本邀请提交至少一项关联任务后退出；同 KR 无关任务不影响",waitingDays:invite.waitingDays,focusType:"invite"}));
    return sortWorkItems([...owned,...inputs,...rejected,...invites]);
  }

  function workResponsibilities(id) {
    const labels=[];
    if (data.tasks.some((task)=>task.owner===id)) labels.push("任务负责人");
    if (data.krs.some((kr)=>kr.owner===id)) labels.push("KR 负责人");
    if (data.completionApprovals.some((item)=>item.reviewers.some((reviewer)=>reviewer.person===id))) labels.push("中间审核人");
    if (data.deliverables.some((item)=>item.receiver===id)) labels.push("接收方");
    if (data.inputRequests.some((item)=>item.provider===id)) labels.push("输入对接人");
    if ((data.taskInvites || []).some((item)=>item.invitee===id && item.state==="待处理")) labels.push("被邀请人");
    return labels.length ? labels.join("、") : "当前未承担行动职责";
  }
  function reminderDisabled(item) {
    if (!item.reminderTarget || item.reminderTarget===ui.identity) return true;
    return (data.reminders || []).some((record)=>record.sender===ui.identity && record.receiver===item.reminderTarget && record.taskId===item.taskId && record.day==="今天");
  }
  function renderWorkCard(item,tab) {
    const openAction=item.type==="invite"?"handle-invite":"work-detail", actionLabel=tab==="approvals"?"去审批":tab==="todo"?"去处理":"查看详情";
    const task=getTask(item.taskId), invite=data.taskInvites?.find((candidate)=>candidate.id===item.inviteId);
    const cardTitle=task?`${task.id} ${task.title}`:invite?`${invite.id} 任务创建邀请`:item.title;
    const krId=task?.krId || invite?.krId || "—";
    const cardDate=item.due || task?.due || invite?.createdAt?.split(" ")[0] || "—";
    const taskState=task ? taskStatusText(task) : invite?.state || "待处理";
    const reminder=(["waiting","risks"].includes(tab) && item.reminderTarget)?`<button class="work-text-action quiet" data-action="work-remind" data-task-id="${item.taskId}" data-target="${item.reminderTarget}" ${reminderDisabled(item)?"disabled":""}>${reminderDisabled(item)?(item.reminderTarget===ui.identity?"无需提醒":"今日已提醒"):"提醒"}</button>`:"";
    return `<article class="work-item ${item.overdue?"overdue":""} ${item.blocked?"blocked":""}" data-action="${openAction}" data-id="${item.taskId}" data-tab="${item.tab || "overview"}" data-source="${tab}" data-focus-type="${item.focusType || "task"}" data-focus-id="${item.focusId || ""}" data-invite-id="${item.inviteId || ""}"><div class="work-kind" aria-hidden="true">${item.kind}</div><div class="work-main"><div class="work-title-row"><h3>${esc(cardTitle)}</h3></div><div class="work-meta"><span>${krId}</span><span>日期 ${cardDate}</span></div></div><div class="work-trailing"><div class="work-state">${status(taskState, task ? statusClass(task.status) : "")}</div><div class="actions">${reminder}<button class="work-text-action" data-action="${openAction}" data-id="${item.taskId}" data-tab="${item.tab || "overview"}" data-source="${tab}" data-focus-type="${item.focusType || "task"}" data-focus-id="${item.focusId || ""}" data-invite-id="${item.inviteId || ""}" aria-label="${esc(actionLabel)}：${esc(cardTitle)}">${actionLabel}${icon("chevron")}</button></div></div></article>`;
  }
  function renderMyWork() {
    const tabs=[["todo","待我处理"],["approvals","待我审批"],["receive","待我接收"],["waiting","等待他人"],["risks","与我相关的卡点"]];
    if (!tabs.some(([id])=>id===ui.myTab)) ui.myTab="todo";
    const member=getMember(ui.identity), permission=member.permission==="admin"?"项目管理员":member.permission==="readonly"?"访客":"成员", items=workItems(ui.myTab);
    return `${head("我的工作","跨项目汇总当前轮到我、或我正在等待他人的事项；任务行仅用于定位，不直接改变业务状态。","")}<section class="work-identity"><div>${avatar(member.id)}<div><b>${member.displayName}</b><span>${permission} · ${member.team}</span></div></div><p><span>当前职责</span>${esc(workResponsibilities(member.id))}</p></section><section class="work-board" aria-label="我的工作事项"><nav class="work-tabs" role="tablist" aria-label="工作分组">${tabs.map(([id,label])=>`<button class="${ui.myTab===id?"active":""}" role="tab" aria-selected="${ui.myTab===id}" data-action="my-tab" data-value="${id}">${label}<span class="pill">${workItems(id).length}</span></button>`).join("")}</nav><div class="work-list" role="tabpanel">${items.length?items.map((item)=>renderWorkCard(item,ui.myTab)).join(""):`<div class="empty work-empty"><b>当前没有${tabs.find(([id])=>id===ui.myTab)[1]}事项</b><span>事项会随任务、审批、接收、输入和卡点事实自动进入或退出。</span></div>`}</div></section>`;
  }
  function sendWorkReminder(taskId,target) {
    if (!target || target===ui.identity) return toast("不能提醒本人。","error");
    data.reminders ||= [];
    if (data.reminders.some((record)=>record.sender===ui.identity && record.receiver===target && record.taskId===taskId && record.day==="今天")) return toast("同一人对同一任务每天只能提醒一次。","error");
    const task=getTask(taskId); data.reminders.push({id:`RM${String(data.reminders.length+1).padStart(2,"0")}`,sender:ui.identity,receiver:target,taskId,day:"今天",createdAt:"刚刚"});
    saveData(`提醒 ${target} 处理 ${taskId}`); renderPage(); toast(`已提醒 ${memberName(target)}：${taskId} ${task?.title || "任务"}，并附带截止时间和下游影响。`,"success");
  }
  function abandonRejectedChange(id) {
    const request=data.changeRequests.find((item)=>item.id===id && item.submitter===ui.identity && item.state==="已退回"); if(!request)return;
    request.dismissed=true; saveData(`放弃 ${request.taskId} 的退回变更事项`); closeDrawer(); renderPage(); toast("已放弃本次变更；任务继续使用原值。","success");
  }

  function renderArtifacts() {
    const groups = data.krs.map((kr) => ({ kr, items: data.deliverables.filter((d) => d.krId === kr.id) })).filter((group) => group.items.length);
    const groupMarkup = groups.map(({ kr, items }) => `<section class="card artifact-group"><div class="artifact-group-head"><h3>${kr.id} · ${esc(kr.title)}</h3><span class="meta">KR 负责人 ${memberName(kr.owner)} · ${items.length} 项交付物</span></div><div class="artifact-table-scroll"><table class="artifact-table"><thead><tr><th></th><th>交付物</th><th>来源任务</th><th>任务负责人</th><th>文件</th><th>内容状态</th><th>接收方</th><th>提交／生效时间</th><th>来源关系边</th></tr></thead><tbody>${items.map((item) => `<tr><td><input class="checkbox artifact-check" type="checkbox" value="${item.id}" ${ui.artifactSelected.has(item.id) ? "checked" : ""} ${item.state === "已生效" ? "" : "disabled"} aria-label="选择 ${esc(item.name)}"></td><td><button class="link-btn artifact-name" data-action="artifact-detail" data-id="${item.id}">${esc(item.name)}</button></td><td><button class="link-btn" data-action="task-detail" data-id="${item.taskId}">${item.taskId}</button></td><td>${ownerCell(item.owner)}</td><td>${esc(item.fileType || "文件")} · ${esc(item.fileSize || "—")}</td><td>${status(item.state)}</td><td>${memberName(item.receiver)}</td><td>${item.effectiveAt || item.submittedAt || item.formedAt}</td><td><button class="link-btn" data-action="focus-relation" data-id="${item.relationId}">${item.relationId}</button></td></tr>`).join("")}</tbody></table></div></section>`).join("");
    return `${head("成果与归档", "按 O / KR 归集当前交付物、审核中的候选内容和来源关系；不保留历史文件。", `${canCreate()?`<button class="btn" data-action="upload-attachment">${icon("upload")}上传过程文件</button>`:""}`)}
      <div class="toolbar"><div class="toolbar-group"><label class="search-box">${icon("search")}<input placeholder="搜索交付物、任务或负责人" /></label><select class="field"><option>全部 O / KR</option>${data.krs.map((kr) => `<option>${kr.id}</option>`).join("")}</select><select class="field"><option>全部内容状态</option><option>已生效</option><option>审核中</option></select></div><span class="meta">仅当前已生效交付物可进入成果包</span></div>
      <div class="artifact-groups">${groupMarkup}</div>
      <section class="package-section"><div class="page-head package-section-head"><div><h1>已形成的阶段成果包</h1><p>保留成果目录与来源事实；下载时使用各项交付物的当前内容。</p></div></div><div class="package-list">${data.packages.map((pkg) => `<article class="package-item"><div class="package-item-head"><h3>${esc(pkg.name)}</h3>${status(pkg.version,"normal")}</div><p>${esc(pkg.scope)} · ${pkg.deliverableIds.length} 项成果</p><p>负责人 ${memberName(pkg.owner)} · ${pkg.formedAt}</p><div class="package-item-actions"><button class="btn compact" data-action="package-detail" data-id="${pkg.id}">查看来源</button><button class="btn compact" data-action="download-package" data-id="${pkg.id}">${icon("download")}模拟下载</button></div></article>`).join("")}</div></section>
      ${ui.artifactSelected.size ? `<div class="selection-bar"><span>已选择 <b>${ui.artifactSelected.size}</b> 项已生效交付物</span><div><button class="btn compact" data-action="clear-artifact-selection">取消</button><button class="btn compact primary" data-action="generate-package">${icon("package")}生成阶段成果包</button></div></div>` : ""}`;
  }

  function renderReports() {
    const labels = { today:"今天", "7d":"近 7 天", "30d":"近 30 天", all:"项目整体" };
    const completed = data.tasks.filter((x) => x.status === "已完成").length;
    return `${head("项目报告", "报告由当前项目事实生成，可补充判断后发布或导出。", `<button class="btn" data-action="export-report" data-type="png">${icon("download")}导出移动端长图</button><button class="btn primary" data-action="export-report" data-type="pdf">${icon("download")}导出 PDF</button>`)}
      <div class="toolbar"><div class="segment">${Object.entries(labels).map(([id,label]) => `<button class="${ui.reportPeriod === id ? "active" : ""}" data-action="report-period" data-value="${id}">${label}</button>`).join("")}</div><div class="toolbar-actions"><span class="meta">报告范围：${labels[ui.reportPeriod]}</span>${canManage()?`<button class="btn compact" data-action="publish-report">发布固定版本</button>`:""}</div></div>
      <article id="report-sheet" class="report-sheet"><div class="report-title"><h2>${esc(data.project.name)}项目报告</h2><p>${labels[ui.reportPeriod]} · 生成时间 2026-08-21 21:30 · 基于当前项目事实</p></div>
        <section class="report-section"><h3>一、整体判断</h3><p>项目处于${esc(data.project.phase)}。三项目标持续推进，体验与运行主线已进入联合验证；当前需优先处理两项负责人审批和一组跨 KR 硬依赖互锁。</p></section>
        <section class="report-section"><h3>二、进展摘要</h3><ul><li>${data.objectives.length} 个 O、${data.krs.length} 个 KR、${data.tasks.length} 项任务在同一事实源中维护。</li><li>${completed} 项任务已由 KR 负责人闭环，${data.deliverables.filter((x) => x.state === "已生效").length} 项当前交付物已生效。</li><li>关键路径 CP-A 与 CP-B 自动标识；CP-B 受互锁关系影响。</li></ul></section>
        <section class="report-section"><h3>三、卡点与待决策</h3><ul>${data.risks.slice(0,4).map((risk) => `<li>${risk.taskId}：${esc(riskReasonText(risk))}，当前待行动人 ${memberName(risk.actionOwner)}；${esc(risk.impact)}。</li>`).join("")}</ul></section>
        <section class="report-section"><h3>四、下一步</h3><ul><li>由对应 KR 负责人完成任务入池和完成终审。</li><li>由总推进人协调互锁任务先后条件，不自动改动承诺日期。</li><li>将新生效成果补充到下一版阶段成果包。</li></ul></section>
      </article>`;
  }

  function renderSettings() {
    const tabs = [["members","成员与职责"],["permissions","系统权限"],["progress","进度权重"],["reminders","提醒规则"],["imports","导入记录"],["audit","操作审计"]];
    let content = "";
    if (ui.settingsTab === "members") content = `<div class="card-head"><h2>成员与职责</h2>${canManage() ? `<button class="btn compact" data-action="invite-member">${icon("plus")}邀请成员</button>` : ""}</div><div class="card-body member-grid">${data.members.map((m) => `<div class="member-card">${avatar(m.id)}<div><b>${m.displayName}</b><span>${m.role} · ${m.team}</span></div></div>`).join("")}</div>`;
    else if (ui.settingsTab === "permissions") content = `<div class="card-head"><h2>统一权限体系</h2></div><div class="card-body"><div class="notice">所有成员查看同一份项目事实；权限只决定创建、编辑、审批、闭环和配置操作。</div>${[["项目总负责人","查看完整项目并处理决策"],["总推进人","项目录入、维护、协调与报告"],["KR 负责人","入池、关键变更和完成终审"],["任务负责人/参与人","执行、提交成果和主动上报卡点"],["只读成员","查看完整上下文，不可修改"]].map((x) => `<div class="property"><label>${x[0]}</label><strong>${x[1]}</strong></div>`).join("")}</div>`;
    else if (ui.settingsTab === "audit") content = `<div class="card-head"><h2>操作审计</h2><button class="btn compact" data-action="download-audit">${icon("download")}导出</button></div><div class="card-body timeline">${data.audit.map((a) => `<div class="timeline-item"><b>${memberName(a.actor)} · ${esc(a.action.replace(/P\d{2}/g, (id) => memberName(id)))}</b><p>${a.time}</p></div>`).join("")}</div>`;
    else content = `<div class="card-head"><h2>${tabs.find((x) => x[0] === ui.settingsTab)[1]}</h2></div><div class="card-body"><div class="property"><label>启用项目默认规则</label><div style="display:flex;justify-content:space-between"><strong>仅在必要节点提醒相关行动人</strong><button class="toggle on" data-action="toggle-setting" aria-label="切换设置"></button></div></div><div class="property"><label>说明</label><strong>该原型保留最少配置项，避免为了管理增加执行者填写负担。</strong></div></div>`;
    return `${head("项目设置", "配置同一项目中的职责与操作权限；只读成员仍可查看完整上下文。", "")}<div class="settings-layout"><aside class="card settings-nav">${tabs.map(([id,label]) => `<button class="${ui.settingsTab === id ? "active" : ""}" data-action="settings-tab" data-value="${id}">${label}</button>`).join("")}</aside><section class="card">${content}</section></div>`;
  }

  function completionCandidateIds(item) { return item?.deliverableIds || (item?.deliverableId ? [item.deliverableId] : []); }
  function completionCandidates(item) { return completionCandidateIds(item).map(getDeliverable).filter(Boolean); }
  function relationTypeLabel(type) { return ({hard:"硬前置",input:"输入关系",interlock:"互锁关系",feedback:"反馈迭代"})[type] || "交付关系"; }
  function taskApprovalItems(taskId) {
    return [
      ...data.entryApprovals.filter((item)=>item.taskId===taskId).map((record)=>({kind:"entry",label:entryApprovalText(getTask(record.taskId)),record})),
      ...data.changeRequests.filter((item)=>item.taskId===taskId).map((record)=>({kind:"change",label:"关键字段修改审核",record})),
      ...data.completionApprovals.filter((item)=>item.taskId===taskId).map((record)=>({kind:"completion",label:"任务完成审核",record})),
    ];
  }
  function approvalPending(item) { return item.kind === "completion" ? ["中间审核中","待 KR 终审"].includes(item.record.state) : item.record.state === "待审批"; }
  function approvalCanAct(item) {
    if (!approvalPending(item)) return false;
    if (item.kind !== "completion") return item.record.krOwner === ui.identity;
    if (item.record.state === "待 KR 终审") return item.record.krOwner === ui.identity;
    return item.record.reviewers.some((reviewer)=>reviewer.person===ui.identity && reviewer.state==="待审核");
  }
  function taskStage(task) {
    const current = taskApprovalItems(task.id).find(approvalPending);
    if (!current) return {stage:task.status === "已完成" ? "已闭环" : "任务执行", actor:task.owner};
    if (current.kind !== "completion") return {stage:current.label,actor:current.record.krOwner};
    if (current.record.state === "待 KR 终审") return {stage:"KR 终审",actor:current.record.krOwner};
    return {stage:"中间或签审核",actor:current.record.reviewers.find((reviewer)=>reviewer.state==="待审核")?.person || current.record.krOwner};
  }
  function taskRelationCard(rel, taskId) {
    const otherId = rel.from === taskId ? rel.to : rel.from, other = getTask(otherId), kr = getKr(other?.krId);
    if (!other) return "";
    return `<button class="relation-card" data-action="task-detail" data-id="${other.id}"><span class="relation-card-main"><b>${other.id} · ${esc(other.title)}</b><small>${kr?.id || "—"} · ${relationTypeLabel(rel.type)}</small></span><span class="relation-card-meta"><span>${memberName(other.owner)} · ${taskStatusText(other)}</span></span>${icon("chevron")}</button>`;
  }
  function workFocusPanel(task, context) {
    if (!context || context.source!=="mywork") return "";
    const focusClass="work-focus-card focus-flash", readonly=context.sourceGroup==="waiting";
    if (context.focusType==="input" || context.focusType==="waiting-input") {
      const request=data.inputRequests.find((item)=>item.id===context.focusId); if(!request)return "";
      const canAct=context.focusType==="input" && request.provider===ui.identity;
      const action=canAct && request.state!=="已提供"?(request.state==="待接收"?`<button class="btn primary" data-action="accept-input" data-id="${request.id}">同意接收</button>`:`<button class="btn primary" data-action="provide-input" data-id="${request.id}">提交内容</button>`):request.state==="已提供"?status("已提供","normal"):"";
      return `<section id="task-focus-target" class="${focusClass}"><div><span class="focus-kicker">${readonly?"等待他人 · 只读查看":"当前待处理输入"}</span><h3>${esc(request.inputName)}</h3><p>${memberName(request.requester)} → ${memberName(request.provider)} · ${request.state} · 期望 ${request.due}</p></div>${action}</section>`;
    }
    if (context.focusType==="receipt") {
      const item=getDeliverable(context.focusId); if(!item)return "";
      return `<section id="task-focus-target" class="${focusClass}"><div><span class="focus-kicker">当前待接收交付物</span><h3>${esc(item.name)}</h3><p>${esc(item.fileType || "文件")} · ${esc(item.fileSize || "—")} · ${esc(item.file)}</p></div><div><button class="btn" data-action="preview-file" data-id="${item.id}">预览</button>${item.receiptState==="已确认"?status("已确认接收","normal"):`<button class="btn primary" data-action="confirm-receipt" data-id="${item.id}">确认接收</button>`}</div></section>`;
    }
    if (context.focusType==="upstream") {
      const relation=data.relations.find((item)=>item.id===context.focusId), source=getTask(relation?.from); if(!relation)return "";
      return `<section id="task-focus-target" class="${focusClass}"><div><span class="focus-kicker">等待他人 · 必要输入未就绪</span><h3>${source?.id || "—"} · ${esc(source?.title || relation.label)}</h3><p>${esc(relation.label)} · ${relation.state} · 当前待行动人 ${source?memberName(taskStage(source).actor):"—"}</p></div><span class="status warning">只读查看</span></section>`;
    }
    if (context.focusType==="waiting-approval") {
      const record=[...data.entryApprovals,...data.changeRequests,...data.completionApprovals].find((item)=>item.id===context.focusId), stage=taskStage(task); if(!record)return "";
      return `<section id="task-focus-target" class="${focusClass}"><div><span class="focus-kicker">等待他人 · 当前环节</span><h3>${esc(stage.stage)}</h3><p>${record.id} · 已等待 ${record.waitingDays || 0} 天 · 当前停在 ${memberName(stage.actor)}</p></div><span class="status warning">只读查看</span></section>`;
    }
    if (context.focusType==="risk" || context.focusType==="risk-timeout") {
      const risk=data.risks.find((item)=>item.id===context.focusId), approval=taskApprovalItems(task.id).find((item)=>item.record.id===context.focusId)?.record;
      const title=risk?`${risk.level} · ${riskReasonText(risk)}`:`审批超时 · ${approval?.id || context.focusId}`, detail=risk?`${risk.impact} · 待行动人 ${memberName(risk.actionOwner)}`:`当前环节已等待 ${approval?.waitingDays || 0} 天，超过项目阈值 ${approvalTimeout()} 天`;
      return `<section id="task-focus-target" class="${focusClass} danger-focus"><div><span class="focus-kicker">系统派生卡点</span><h3>${esc(title)}</h3><p>${esc(detail)}；触发条件消失后自动解除。</p></div><button class="btn" data-action="focus-graph" data-id="${task.id}">${icon("graph")}查看影响路径</button></section>`;
    }
    if (context.focusType==="rejection") {
      const request=data.changeRequests.find((item)=>item.id===context.focusId); if(!request)return "";
      return `<section id="task-focus-target" class="${focusClass} danger-focus"><div><span class="focus-kicker">退回待处理事项</span><h3>${request.id} · ${esc(request.field)}</h3><p>已退回：${esc(request.opinion || "请补充后重新提交")}。仅查看不会清除。</p></div><div><button class="btn" data-action="abandon-change" data-id="${request.id}">放弃本次变更</button><button class="btn primary" data-action="edit-task" data-id="${task.id}">修改并重提</button></div></section>`;
    }
    return `<section id="task-focus-target" class="${focusClass}"><div><span class="focus-kicker">当前待处理任务</span><h3>${task.id} · ${esc(task.title)}</h3><p>${taskStatusText(task)} · 截止 ${task.due} · 任务负责人 ${memberName(task.owner)}</p></div></section>`;
  }
  function taskOverviewTab(task, context=ui.taskDrawerContext) {
    const kr=getKr(task.krId);
    const editableProgress=context?.source==="mywork"&&context.sourceGroup==="todo";
    const incoming=data.relations.filter((rel)=>rel.to===task.id), outgoing=data.relations.filter((rel)=>rel.from===task.id), manual=data.inputRequests.filter((item)=>item.taskId===task.id);
    const necessaryIncoming=incoming.filter(necessaryRelation), referenceIncoming=incoming.filter((rel)=>!necessaryRelation(rel)), necessaryManual=manual.filter((item)=>item.necessity!=="参考"), referenceManual=manual.filter((item)=>item.necessity==="参考");
    const allCurrent=data.deliverables.filter((item)=>item.taskId===task.id && item.state==="已生效"), current=editableProgress?allCurrent.filter((item)=>item.uploadedByUser===true||item.submittedAt==="刚刚"):allCurrent, candidates=data.deliverables.filter((item)=>item.taskId===task.id && item.state==="审核中");
    const inputCards=[...necessaryIncoming.map((rel)=>{const source=getTask(rel.from);return `<div class="input-fact"><div><b>${esc(rel.label)}</b><span>已有任务 · ${source?.id || "—"} ${esc(source?.title || "")}</span></div><div><span>提供人 ${source ? memberName(source.owner) : "—"} · 计划 ${source?.due || "—"}</span>${status(rel.state)}</div></div>`}),...necessaryManual.map((request)=>`<div class="input-fact"><div><b>${esc(request.inputName)}</b><span>指定项目成员提供</span></div><div><span>提供人 ${memberName(request.provider)} · 计划 ${request.due}</span>${status(request.state)}</div>${!["已提供"].includes(request.state)?`<small>缺失原因：内容尚未提交 · 待行动人 ${memberName(request.provider)}</small>`:""}</div>`)].join("");
    const referenceCards=[...referenceIncoming.map((rel)=>{const source=getTask(rel.from);return `<div class="reference-input"><b>${esc(rel.label)}</b><span>${source?.id || "—"} ${esc(source?.title || "")} · ${rel.state}</span></div>`}),...referenceManual.map((request)=>`<div class="reference-input"><b>${esc(request.inputName)}</b><span>${memberName(request.provider)} · ${request.state}</span></div>`)].join("");
    const deliverableCards=current.map((item)=>`<article class="deliverable-card"><div><b>${esc(item.name)}</b><button class="upload-file-link" data-action="preview-file" data-id="${item.id}">${esc(item.file)}</button><span>${esc(item.fileType || "文件")} · ${esc(item.fileSize || "—")} · 更新于 ${item.effectiveAt || item.formedAt}</span></div><div><button class="btn compact" data-action="download-file" data-id="${item.id}">${icon("download")}下载</button></div></article>`).join("");
    const necessarySection=inputCards?`<section class="drawer-section"><h3>必要输入 <span class="meta">${necessaryIncoming.length+necessaryManual.length} 项</span></h3>${inputCards}</section>`:"";
    const relationSection=(incoming.length||outgoing.length)?`<section class="drawer-section task-relation-section"><div class="section-title-row"><h3>协作关系</h3><button class="link-btn" data-action="focus-graph" data-id="${task.id}">在关系图谱中查看 →</button></div>${incoming.length?`<div class="relation-group"><b>直接上游 · ${incoming.length}</b>${incoming.map((rel)=>taskRelationCard(rel,task.id)).join("")}</div>`:""}${outgoing.length?`<div class="relation-group"><b>直接下游 · ${outgoing.length}</b>${outgoing.map((rel)=>taskRelationCard(rel,task.id)).join("")}</div>`:""}</section>`:"";
    const progressField=editableProgress?`<div class="task-progress-inline" data-task-id="${task.id}"><strong>${task.progress == null ? "未填写" : `${task.progress}%`}</strong><button class="task-progress-action" data-action="edit-task-progress" data-id="${task.id}" aria-label="编辑任务进度">${icon("edit")}</button></div>`:`<strong>${task.progress == null ? "未填写" : `${task.progress}%`}</strong>`;
    const currentSection=current.length?`<section class="drawer-section"><h3>当前交付物 <span class="meta">${current.length} 项</span></h3>${candidates.length?`<div class="notice warning" style="margin-bottom:10px">有 ${candidates.length} 项更新审核中，候选内容请在“审核”Tab 查看；当前内容继续有效。</div>`:""}${deliverableCards}</section>`:"";
    return `<div class="task-tab-panel" role="tabpanel">${workFocusPanel(task,context)}<section class="drawer-section task-basic-section"><h3>基础信息</h3><div class="task-basic-panel"><div class="task-info-list"><div class="task-info-row"><span>负责人</span><strong>${name(task.owner)}</strong></div><div class="task-info-row"><span>参与人</span><strong>${task.participants.length ? task.participants.map(memberName).join("、") : "未设置"}</strong></div><div class="task-info-row"><span>周期</span><strong>${task.start} — ${task.due}</strong></div><div class="task-info-row"><span>执行状态</span><div>${taskStatusChip(task)}</div></div><div class="task-info-row"><span>进度</span>${progressField}</div>${task.description?`<div class="task-info-row task-info-long"><span>任务说明</span><strong>${esc(task.description)}</strong></div>`:""}<div class="task-info-row task-info-long"><span>量化标准</span><strong>${esc(task.acceptanceCriteria || `形成可供 ${memberName(task.receiver)} 接收的“${task.outputName}”，并通过 KR 终审。`)}</strong></div></div></div></section>
      ${necessarySection}${referenceCards?`<section class="drawer-section reference-section"><h3>参考输入 <span class="meta">仅提示，不产生“等待他人”事项</span></h3>${referenceCards}</section>`:""}
      ${currentSection}${relationSection}</div>`;
  }

  function startTaskProgressEdit(taskId) {
    if (ui.taskDrawerSource!=="mywork" || ui.taskDrawerContext?.sourceGroup!=="todo") return;
    const task=getTask(taskId),root=$(`.task-progress-inline[data-task-id="${taskId}"]`); if(!task||!root)return;
    root.innerHTML=`<input id="task-progress-input" class="task-progress-input" type="number" min="0" max="100" step="1" value="${task.progress == null ? "" : task.progress}" placeholder="0–100" aria-label="任务进度百分比"><span>%</span><button class="task-progress-action confirm" data-action="confirm-task-progress" data-id="${taskId}" aria-label="确认任务进度">${icon("check")}</button>`;
    const input=$("#task-progress-input",root); input?.focus(); input?.select();
  }

  function confirmTaskProgress(taskId) {
    if (ui.taskDrawerSource!=="mywork" || ui.taskDrawerContext?.sourceGroup!=="todo") return;
    const input=$("#task-progress-input"),task=getTask(taskId),value=Number(input?.value); if(!task||!input)return;
    if(input.value===""||!Number.isFinite(value)||value<0||value>100)return toast("请输入 0–100 的进度。","error");
    task.progress=Math.round(value);task.updatedAt="刚刚";saveData(`更新 ${taskId} 进度为 ${task.progress}%`);toast(`${taskId} 进度已更新为 ${task.progress}%。`,"success");refreshMyWorkDrawer(taskId);
  }
  function taskDiscussionTab(task) {
    const comments=(data.discussions || []).filter((item)=>item.taskId===task.id);
    return `<div class="task-tab-panel"><section class="discussion-list">${comments.map((item)=>`<article class="discussion-item">${avatar(item.author)}<div><div><b>${memberName(item.author)}</b><time>${item.createdAt}</time></div><p>${esc(item.content)}</p></div></article>`).join("") || `<div class="empty compact-empty">尚无讨论意见</div>`}</section><form id="task-discussion-form" data-task-id="${task.id}" class="discussion-compose"><label for="discussion-content">提交意见</label><textarea id="discussion-content" name="content" class="field" placeholder="输入文字意见，可使用 @姓名 提醒项目成员" required></textarea><div><span class="meta">提交后不可编辑、不可删除；任务负责人和被 @ 成员会收到通知。</span><button type="button" class="btn primary" data-action="submit-discussion">提交讨论</button></div></form></div>`;
  }
  function approvalContent(item) {
    const record=item.record;
    if (item.kind==="entry") return `<div class="approval-content"><b>申请内容</b><p>${esc(record.reason)}</p></div>`;
    if (item.kind==="change") return `<div class="approval-compare"><div><span>修改前</span><b>${esc(record.oldValue)}</b></div><span>→</span><div><span>修改后</span><b>${esc(record.newValue)}</b></div></div><p class="meta">修改原因：${esc(record.reason)}；审核完成前旧值继续生效。</p>`;
    const live=completionCandidates(record), snapshot=record.candidateSnapshot || [], files=live.length ? live : snapshot;
    return `<div class="notice warning">候选交付物审核中，不可作为正式输入；本次 ${files.length} 项内容整体通过或退回。</div><div class="candidate-list">${files.map((file)=>`<div class="candidate-file"><div><b>${esc(file.name)}</b><span>${esc(file.fileType || "文件")} · ${esc(file.fileSize || "—")} · ${esc(file.file || "文件已删除")}</span></div>${live.some((candidate)=>candidate.id===file.id)?`<div><button class="btn compact" data-action="preview-file" data-id="${file.id}">预览</button><button class="btn compact" data-action="download-file" data-id="${file.id}">下载</button></div>`:`<span class="meta">文件已按覆盖／退回规则删除</span>`}</div>`).join("")}</div>${approvalLane(record)}`;
  }
  function taskAuditTab(task, context=ui.taskDrawerContext) {
    const items=taskApprovalItems(task.id).sort((a,b)=>Number(approvalPending(b))-Number(approvalPending(a)));
    const cards=items.map((item)=>{
      const record=item.record,pending=approvalPending(item),canAct=approvalCanAct(item),kind=item.kind==="completion"?(record.state==="中间审核中"?"middle":"final"):item.kind;
      const head=`<div class="audit-card-head"><div><span class="pill">${item.label}</span><h3>${record.id} · ${status(record.state)}</h3></div><span class="meta">申请人 ${memberName(record.submitter)} · ${record.createdAt || "已记录"}${pending?` · 已等待 ${record.waitingDays || 0} 天`:""}</span></div>`;
      const body=`<div class="audit-card-body">${approvalContent(item)}${record.handledBy?`<div class="handled-fact"><b>${memberName(record.handledBy)} · ${record.result}</b><span>${record.handledAt} · ${esc(record.opinion || "未填写意见")}</span></div>`:""}${canAct?`<div class="audit-actions"><button class="btn danger" data-action="review-decision" data-kind="${kind}" data-id="${record.id}" data-value="reject">退回</button><button class="btn primary" data-action="review-decision" data-kind="${kind}" data-id="${record.id}" data-value="approve">通过${kind==="final"?" / 闭环":""}</button></div>`:""}</div>`;
      const focused=context?.source==="mywork" && context.focusId===record.id ? " focus-flash" : "";
      return pending ? `<article id="${focused?"task-focus-target":""}" class="audit-card pending${focused}">${head}${body}</article>` : `<details id="${focused?"task-focus-target":""}" class="audit-card ended${focused}" ${focused?"open":""}><summary>${head}</summary>${body}</details>`;
    }).join("");
    return `<div class="task-tab-panel"><div class="audit-list">${cards || `<div class="empty compact-empty">暂无审核记录</div>`}</div></div>`;
  }
  function openTaskDrawer(taskId, tab="overview", context=ui.taskDrawerContext) {
    const task=getTask(taskId); if(!task)return;
    ui.taskDrawerContext=context;
    const kr=getKr(task.krId), comments=(data.discussions || []).filter((item)=>item.taskId===task.id).length, approvals=taskApprovalItems(task.id), pending=approvals.filter(approvalPending).length, needsAction=approvals.some(approvalCanAct);
    const tabs=`<nav class="task-tabs" role="tablist" aria-label="任务详情"><button role="tab" class="${tab==="overview"?"active":""}" aria-selected="${tab==="overview"}" data-action="task-tab" data-id="${task.id}" data-value="overview">任务概况</button><button role="tab" class="${tab==="audit"?"active":""} ${needsAction?"needs-action":""}" aria-selected="${tab==="audit"}" data-action="task-tab" data-id="${task.id}" data-value="audit">审核 ${pending}</button><button role="tab" class="${tab==="discussion"?"active":""}" aria-selected="${tab==="discussion"}" data-action="task-tab" data-id="${task.id}" data-value="discussion">讨论 ${comments}</button></nav>`;
    const content=tab==="discussion"?taskDiscussionTab(task):tab==="audit"?taskAuditTab(task,context):taskOverviewTab(task,context);
    let footer="";
    if (["tasks","mywork"].includes(ui.taskDrawerSource) && !(context?.source==="mywork" && (["waiting","risks"].includes(context.sourceGroup) || ["receipt","input"].includes(context.focusType)))) {
      if (canManage() || task.owner===ui.identity) footer+=`<button class="btn" data-action="edit-task" data-id="${task.id}">${icon("edit")}编辑任务</button>`;
      if (task.owner===ui.identity || isLead()) footer+=`<button class="btn" data-action="configure-input" data-id="${task.id}">${icon("link")}配置输入</button>`;
      if (task.owner===ui.identity && !approvals.some(approvalPending) && task.status!=="已完成") footer+=`<button class="btn primary" data-action="submit-completion" data-id="${task.id}">${icon("check")}提交完成申请</button>`;
    }
    const drawer=$("#drawer-root .task-drawer"), sameTask=drawer?.dataset.taskId===task.id;
    if (sameTask) {
      const headTitle=drawer.querySelector(".drawer-head h2"), headSubtitle=drawer.querySelector(".drawer-head p"), body=drawer.querySelector(".drawer-body"), foot=drawer.querySelector(".drawer-foot");
      if (headTitle) headTitle.textContent=`${task.id} · ${task.title}`;
      if (headSubtitle) headSubtitle.textContent=`所属 O / KR：${kr.objectiveId} / ${kr.id} · 更新于 ${task.updatedAt}`;
      if (body) { body.innerHTML=`${tabs}${content}`; body.scrollTop=0; }
      if (foot) foot.innerHTML=footer;
      drawer.classList.toggle("mywork-task-drawer",context?.source==="mywork");
      hydrateIcons(drawer);
    } else {
      openDrawer(`${task.id} · ${task.title}`, `所属 O / KR：${kr.objectiveId} / ${kr.id} · 更新于 ${task.updatedAt}`, `${tabs}${content}`, footer);
      const nextDrawer=$("#drawer-root .drawer");
      nextDrawer?.classList.add("task-drawer");
      if (nextDrawer) nextDrawer.dataset.taskId=task.id;
      if (context?.source==="mywork") nextDrawer?.classList.add("mywork-task-drawer");
    }
    if (context?.source==="mywork" && typeof requestAnimationFrame==="function") requestAnimationFrame(()=>$("#task-focus-target")?.scrollIntoView({behavior:"smooth",block:"center"}));
  }

  function openTaskEditorDrawer(taskId) {
    const task = getTask(taskId); if (!task || !(canManage() || task.owner === ui.identity)) return;
    const body = `<form id="task-edit-form" data-task-id="${taskId}" class="form-grid">${memberDatalist("task-editor-members", false)}<div class="form-row full"><label>任务名称</label><input class="field" name="title" value="${esc(task.title)}" required></div><div class="form-row full"><label>负责人</label>${memberSearchInput("owner", task.owner, "task-editor-members", "任务负责人")}</div><div class="form-row"><label>开始日期</label><input type="date" class="field" name="start" value="${dateInputValue(task.start)}" required></div><div class="form-row"><label>截止日期</label><input type="date" class="field" name="due" value="${dateInputValue(task.due)}" required></div><div class="form-row full"><label>预期交付物</label><input class="field" name="outputName" value="${esc(task.outputName)}"></div><div class="form-row full"><div class="notice">负责人和周期属于关键字段；提交后由所属 KR 负责人审批，审批期间旧值继续生效。</div></div></form>`;
    openDrawer(`编辑 ${taskId}`, `${task.krId} · 提交关键字段变更`, body, `<button class="btn" data-action="close-drawer">取消</button><button class="btn primary" data-action="submit-task-edit">提交变更审批</button>`);
    $("#drawer-root .drawer")?.classList.add("task-editor-drawer");
  }

  function submitTaskEdit() {
    const form = $("#task-edit-form"); if (!form?.reportValidity()) return;
    const fd = new FormData(form), task = getTask(form.dataset.taskId), owner = resolveMemberInput(fd.get("owner"), false); if (!task) return;
    if (!owner) return toast("请从匹配结果中选择有效任务负责人。", "error");
    const proposed = { title:fd.get("title"), owner, start:compactDate(fd.get("start")), due:compactDate(fd.get("due")), outputName:fd.get("outputName") };
    const oldValue = `${memberName(task.owner)} · ${task.start}—${task.due}`, newValue = `${memberName(owner)} · ${proposed.start}—${proposed.due}`;
    data.changeRequests.filter((item)=>item.taskId===task.id && item.submitter===ui.identity && item.state==="已退回").forEach((item)=>{item.dismissed=true;});
    data.changeRequests.push({ id:`CR${String(data.changeRequests.length + 1).padStart(2,"0")}`, taskId:task.id, submitter:ui.identity, krOwner:getKr(task.krId).owner, state:"待审批", field:"负责人 / 周期", oldValue, newValue, reason:"更新任务负责人、周期或任务信息", proposed, createdAt:"刚刚", waitingDays:0 });
    saveData(`提交 ${task.id} 负责人和周期变更审批`); renderPage(); toast("任务修改已提交 KR 负责人审批；旧值继续生效。", "success");
  }

  function approvalLane(item) {
    const reviewers = item.reviewers.length ? item.reviewers.map((r) => `<div class="approval-node"><b>${memberName(r.person)} 中间审核</b><span>${r.state} · 或签</span></div><span class="approval-arrow">→</span>`).join("") : `<div class="approval-node"><b>无需中间审核</b><span>提交后直达终审</span></div><span class="approval-arrow">→</span>`;
    return `<div class="approval-lane"><div class="approval-node"><b>${memberName(item.submitter)} 提交</b><span>${completionCandidateIds(item).length} 项候选交付物</span></div><span class="approval-arrow">→</span>${reviewers}<div class="approval-node"><b>${memberName(item.krOwner)} KR 终审</b><span>${item.state}</span></div></div>`;
  }

  function openArtifactDrawer(id) {
    const item = getDeliverable(id); if (!item) return;
    const task = getTask(item.taskId), kr = getKr(item.krId), relation = data.relations.find((r) => r.id === item.relationId), completion = data.completionApprovals.find((x) => completionCandidateIds(x).includes(id));
    const body = `<div class="drawer-section"><div class="detail-grid"><div class="detail-cell"><label>所属 KR</label><strong>${kr.id} · ${memberName(kr.owner)}</strong></div><div class="detail-cell"><label>审核状态</label>${status(item.state)}</div><div class="detail-cell"><label>来源任务</label><strong>${task.id} · ${esc(task.title)}</strong></div><div class="detail-cell"><label>任务负责人</label><strong>${name(item.owner)}</strong></div><div class="detail-cell"><label>接收方</label><strong>${name(item.receiver)}</strong></div><div class="detail-cell"><label>形成时间</label><strong>${item.formedAt}</strong></div></div></div>
      <div class="drawer-section"><h3>${item.state === "审核中" ? "候选内容" : "当前内容"}</h3><div class="notice ${item.state === "审核中" ? "warning" : ""}">${item.state} · ${esc(item.fileType || "文件")} · ${esc(item.fileSize || "—")} · ${esc(item.file)}</div>${item.state === "审核中" ? `<p class="meta">审核中，不可作为正式输入。</p>` : ""}</div>
      <div class="drawer-section"><h3>来源关系边</h3><div class="side-item"><strong>${relation?.from || "—"} → ${relation?.to || "—"}</strong><p>${esc(relation?.label || "无关系标签")} · ${relation?.state || "—"}</p></div><button class="link-btn" data-action="focus-relation" data-id="${item.relationId}">在图谱中定位关系 →</button></div>
      <div class="drawer-section"><h3>审核事实</h3>${completion ? approvalLane(completion) : `<p class="meta">当前内容已通过 KR 终审。系统不保留历史文件或历史版本入口。</p>`}</div>`;
    openDrawer(item.name, `${item.id} · ${item.state}`, body, `<button class="btn" data-action="task-detail" data-id="${item.taskId}">返回来源任务</button><button class="btn" data-action="preview-file" data-id="${item.id}">预览</button><button class="btn primary" data-action="download-file" data-id="${item.id}">${icon("download")}模拟下载文件</button>`);
  }

  function openDrawer(title, subtitle, body, footer = "") {
    $("#drawer-root").innerHTML = `<div class="drawer-backdrop" data-action="close-drawer"></div><aside class="drawer"><div class="drawer-head"><div><h2>${esc(title)}</h2><p>${esc(subtitle)}</p></div><button class="icon-btn" data-action="close-drawer" aria-label="关闭详情">${icon("close")}</button></div><div class="drawer-body">${body}</div><div class="drawer-foot">${footer}</div></aside>`;
    hydrateIcons($("#drawer-root"));
  }
  function closeDrawer() { $("#drawer-root").innerHTML = ""; ui.taskDrawerContext=null; }
  function openModal(title, subtitle, body, footer) {
    $("#modal-root").innerHTML = `<div class="modal-backdrop" data-action="close-modal"></div><section class="modal" role="dialog" aria-modal="true"><div class="modal-head"><div><h2>${esc(title)}</h2><p>${esc(subtitle)}</p></div><button class="icon-btn" data-action="close-modal" aria-label="关闭弹窗">${icon("close")}</button></div><div class="modal-body">${body}</div><div class="modal-foot">${footer}</div></section>`;
    hydrateIcons($("#modal-root"));
    setTimeout(() => $("#modal-root input, #modal-root select, #modal-root button")?.focus(), 30);
  }
  function closeModal() { $("#modal-root").innerHTML = ""; }
  function closePopover() { $("#popover-root").innerHTML = ""; }
  function closeLayers() { closeDrawer(); closeModal(); closePopover(); $("#sidebar")?.classList.remove("open"); }
  function markTaskWorkflowModal(wide=false) {
    const modal=$("#modal-root .modal");
    modal?.classList.add("task-workflow-modal");
    if (wide) modal?.classList.add("modal-wide");
  }

  function createTaskModal(inviteId="") {
    if (!canCreate()) return toast("当前身份仅可查看，不能创建任务。", "error");
    const invite=(data.taskInvites || []).find((item)=>item.id===inviteId && item.invitee===ui.identity && item.state==="待处理");
    const body = `<form id="create-task-form" data-invite-id="${invite?.id || ""}">${memberDatalist("task-member-options", false)}${invite?`<div class="notice" style="margin-bottom:12px"><b>${invite.krId} 任务创建邀请</b><br>${esc(invite.note)}<br><span class="meta">只有通过本邀请提交关联任务，邀请才会退出。</span></div>`:""}<div class="task-sheet"><div class="task-sheet-head"><span>所属 KR</span><span>任务名称</span><span>负责人</span><span>任务周期</span><span>预期交付物</span><span></span></div><div id="task-sheet-rows">${taskDraftRow(invite)}</div></div><div class="task-sheet-actions"><button type="button" class="btn compact" data-action="add-task-row">${icon("plus")}继续添加任务</button><span class="meta">可连续录入多项任务，保存后统一提交各自所属 KR 负责人审批。</span></div><div class="notice task-create-notice">任务提交后由所属 KR 负责人审批；通过后才进入执行池并变为“未开始”。</div></form>`;
    openModal(invite?"响应任务创建邀请":"创建任务", invite?`${invite.krId} · ${memberName(invite.inviter)} 发起`:"按 KR 连续录入任务骨架并指定负责人", body, `<button class="btn" data-action="close-modal">取消</button><button class="btn primary" data-action="submit-new-task">提交负责人审批</button>`);
    markTaskWorkflowModal(true);
  }

  function taskDraftRow(invite=null) {
    const defaultOwner = data.members.some((m) => m.id === ui.identity && m.permission !== "readonly") ? ui.identity : "P06";
    const krOptions = data.krs.map((kr) => `<option value="${kr.id}" ${invite?.krId===kr.id?"selected":""}>${kr.id} · ${esc(kr.title)}</option>`).join("");
    return `<div class="task-sheet-row"><div class="task-sheet-cell"><select name="krId" class="field" aria-label="所属 KR">${krOptions}</select></div><div class="task-sheet-cell"><input name="title" class="field" required value="${esc(invite?"验证现场联动异常回退":"新增协作任务")}" aria-label="任务名称"></div><div class="task-sheet-cell task-owner-cell">${memberSearchInput("owner", defaultOwner, "task-member-options", "任务负责人")}</div><div class="task-sheet-cell"><div class="sheet-date-range task-sheet-date-range" role="group" aria-label="任务周期"><input type="date" name="start" value="2026-09-12" max="2026-09-21" required aria-label="周期开始日期"><span aria-hidden="true">—</span><input type="date" name="due" value="2026-09-21" min="2026-09-12" required aria-label="周期结束日期"></div></div><div class="task-sheet-cell"><input name="outputName" class="field" value="任务成果记录" aria-label="预期交付物"></div><button type="button" class="icon-btn" data-action="remove-task-row" aria-label="删除该任务行">${icon("close")}</button></div>`;
  }

  function addTaskDraftRow() {
    $("#task-sheet-rows")?.insertAdjacentHTML("beforeend", taskDraftRow());
  }

  function inviteTaskOwnersModal() {
    if (!canCreate()) return toast("当前身份仅可查看，不能邀请成员创建任务。", "error");
    ui.inviteMemberSelected.clear(); ui.inviteMemberSourceMarked.clear(); ui.inviteMemberTargetMarked.clear(); ui.inviteMemberCollapsedRoles.clear(); ui.inviteMemberSourceSearch = ""; ui.inviteMemberTargetSearch = "";
    const body = `<form id="invite-task-owner-form" class="form-grid"><div class="form-row full"><label>邀请成员为哪个 KR 创建任务</label><select name="krId" class="field">${data.krs.map((kr) => `<option value="${kr.id}">${kr.id} · ${esc(kr.title)}</option>`).join("")}</select></div><div class="form-row full"><label>选择受邀成员（可多选）</label><div id="invite-member-transfer">${inviteMemberTransferMarkup()}</div></div><div class="form-row full"><label>邀请说明</label><textarea name="note" class="field">请结合你负责的工作，在该 KR 下补充需要推进的任务。</textarea></div><div class="form-row full"><div class="notice">邀请不依赖现有任务。发送后，右侧已选成员会收到带 KR 上下文的站内通知。</div></div></form>`;
    openModal("邀请成员创建任务", "KR 负责人可以在任务尚未建立时先发出邀请", body, `<button class="btn" data-action="close-modal">取消</button><button class="btn primary" data-action="send-task-invites">发送邀请</button>`);
    markTaskWorkflowModal(true);
  }

  function inviteMemberCandidates() {
    return data.members.filter((member) => member.permission !== "readonly" && member.id !== "P01" && member.id !== ui.identity);
  }

  function inviteMemberTransferMarkup() {
    const candidates = inviteMemberCandidates();
    const source = candidates.filter((member) => !ui.inviteMemberSelected.has(member.id));
    const selected = candidates.filter((member) => ui.inviteMemberSelected.has(member.id));
    const roles = [...new Set(source.map((member) => member.role))];
    const sourceGroups = roles.map((role) => {
      const members = source.filter((member) => member.role === role);
      const allMarked = members.length > 0 && members.every((member) => ui.inviteMemberSourceMarked.has(member.id));
      return `<div class="transfer-tree-group-block ${ui.inviteMemberCollapsedRoles.has(role) ? "collapsed" : ""}" data-transfer-role-block="${esc(role)}"><div class="transfer-tree-group"><button type="button" class="transfer-tree-toggle" data-action="toggle-invite-tree-group" data-value="${esc(role)}" aria-label="${ui.inviteMemberCollapsedRoles.has(role) ? "展开" : "收起"} ${esc(role)}">${icon("down")}</button><label><input type="checkbox" data-invite-source-group value="${esc(role)}" ${allMarked ? "checked" : ""}><b>${esc(role)}</b><span>${members.length} 人</span></label></div><div class="transfer-tree-children">${members.map((member) => `<label class="transfer-member-row" data-transfer-source-item data-search="${esc(`${member.displayName} ${member.role} ${member.team}`.toLowerCase())}"><input type="checkbox" data-invite-source-member value="${member.id}" ${ui.inviteMemberSourceMarked.has(member.id) ? "checked" : ""}>${avatar(member.id)}<span><b>${esc(member.displayName)}</b><small>${esc(member.team)}</small></span></label>`).join("")}</div></div>`;
    }).join("");
    const targetRows = selected.map((member) => `<label class="transfer-member-row" data-transfer-target-item data-search="${esc(`${member.displayName} ${member.role} ${member.team}`.toLowerCase())}"><input type="checkbox" data-invite-target-member value="${member.id}" ${ui.inviteMemberTargetMarked.has(member.id) ? "checked" : ""}>${avatar(member.id)}<span><b>${esc(member.displayName)}</b><small>${esc(member.role)} · ${esc(member.team)}</small></span></label>`).join("");
    const hiddenInputs = selected.map((member) => `<input type="hidden" name="invitee" value="${member.id}">`).join("");
    return `<div class="member-transfer">${hiddenInputs}<section class="transfer-panel"><div class="transfer-panel-head"><b>可选成员</b><span>${source.length} 人</span></div><label class="transfer-search">${icon("search")}<input id="invite-member-source-search" type="search" placeholder="搜索姓名、角色或团队" value="${esc(ui.inviteMemberSourceSearch)}" autocomplete="off"></label><div class="transfer-tree" role="tree" aria-multiselectable="true">${sourceGroups}<div id="invite-member-source-empty" class="transfer-empty hidden">没有匹配成员</div></div></section><div class="transfer-actions"><button id="invite-transfer-add" type="button" data-action="invite-transfer-add" aria-label="加入已选成员" disabled>${icon("chevron")}</button><button id="invite-transfer-remove" type="button" data-action="invite-transfer-remove" aria-label="移回可选成员" disabled>${icon("back")}</button></div><section class="transfer-panel"><div class="transfer-panel-head"><b>已选成员</b><span>${selected.length} 人</span></div><label class="transfer-search">${icon("search")}<input id="invite-member-target-search" type="search" placeholder="搜索已选成员" value="${esc(ui.inviteMemberTargetSearch)}" autocomplete="off"></label><div class="transfer-target-list" role="listbox" aria-multiselectable="true">${targetRows}<div id="invite-member-target-empty" class="transfer-empty ${selected.length ? "hidden" : ""}">${selected.length ? "没有匹配成员" : "请从左侧选择成员"}</div></div></section></div>`;
  }

  function renderInviteMemberTransfer() {
    const root = $("#invite-member-transfer"); if (!root) return;
    root.innerHTML = inviteMemberTransferMarkup();
    filterInviteMemberTransfer("source", ui.inviteMemberSourceSearch);
    filterInviteMemberTransfer("target", ui.inviteMemberTargetSearch);
    updateInviteTransferControls();
  }

  function filterInviteMemberTransfer(side, query = "") {
    const root = $("#invite-member-transfer"); if (!root) return;
    const keyword = query.trim().toLowerCase();
    if (side === "source") {
      ui.inviteMemberSourceSearch = query;
      let visible = 0;
      $$("[data-transfer-source-item]", root).forEach((row) => { const match = !keyword || row.dataset.search.includes(keyword); row.classList.toggle("hidden", !match); if (match) visible += 1; });
      $$("[data-transfer-role-block]", root).forEach((block) => { const hasMatch = $$("[data-transfer-source-item]", block).some((row) => !row.classList.contains("hidden")); block.classList.toggle("hidden", !hasMatch); block.classList.toggle("searching", Boolean(keyword)); });
      $("#invite-member-source-empty")?.classList.toggle("hidden", visible > 0);
    } else {
      ui.inviteMemberTargetSearch = query;
      let visible = 0;
      $$("[data-transfer-target-item]", root).forEach((row) => { const match = !keyword || row.dataset.search.includes(keyword); row.classList.toggle("hidden", !match); if (match) visible += 1; });
      const empty = $("#invite-member-target-empty"); if (empty) { empty.textContent = ui.inviteMemberSelected.size ? "没有匹配成员" : "请从左侧选择成员"; empty.classList.toggle("hidden", visible > 0); }
    }
  }

  function updateInviteTransferControls() {
    const candidates = inviteMemberCandidates();
    $$("[data-invite-source-group]", $("#invite-member-transfer")).forEach((input) => {
      const ids = candidates.filter((member) => member.role === input.value && !ui.inviteMemberSelected.has(member.id)).map((member) => member.id);
      const marked = ids.filter((memberId) => ui.inviteMemberSourceMarked.has(memberId)).length;
      input.checked = ids.length > 0 && marked === ids.length; input.indeterminate = marked > 0 && marked < ids.length;
    });
    const add = $("#invite-transfer-add"), remove = $("#invite-transfer-remove");
    if (add) add.disabled = ui.inviteMemberSourceMarked.size === 0;
    if (remove) remove.disabled = ui.inviteMemberTargetMarked.size === 0;
  }

  function sendTaskInvites() {
    const form = $("#invite-task-owner-form"), fd = new FormData(form), invitees = fd.getAll("invitee"), krId = fd.get("krId");
    if (!invitees.length) return toast("请至少选择一名受邀成员。", "error");
    data.taskInvites ||= [];
    invitees.forEach((invitee) => data.taskInvites.push({ id:`TI${String(data.taskInvites.length + 1).padStart(2,"0")}`, krId, inviter:ui.identity, invitee, note:fd.get("note"), state:"待处理", createdAt:"刚刚" }));
    saveData(`邀请 ${invitees.map(memberName).join("、")} 为 ${krId} 创建任务`); closeModal(); toast(`已向 ${invitees.length} 名成员发送 ${krId} 任务创建邀请。`, "success");
  }

  function configureInputModal(taskId) {
    const task = getTask(taskId); if (!task) return;
    ui.inputTaskSelected.clear(); ui.inputTaskSourceMarked.clear(); ui.inputTaskTargetMarked.clear(); ui.inputTaskCollapsedObjectives.clear(); ui.inputTaskCollapsedKrs.clear(); ui.inputTaskSourceSearch=""; ui.inputTaskTargetSearch="";
    ui.inputMemberSelected.clear(); ui.inputMemberSourceMarked.clear(); ui.inputMemberTargetMarked.clear(); ui.inputMemberCollapsedTeams.clear(); ui.inputMemberSourceSearch=""; ui.inputMemberTargetSearch="";
    const body = `<form id="input-source-form" data-task-id="${taskId}"><div class="radio-cards"><label class="radio-card active" data-action="input-mode" data-value="task"><input type="radio" name="mode" value="task" checked hidden><b>从已有任务选择</b><span>按 O / KR / 任务多选输入来源</span></label><label class="radio-card" data-action="input-mode" data-value="person"><input type="radio" name="mode" value="person" hidden><b>指定项目成员提供</b><span>按团队选择一组人或多个人</span></label></div><div id="input-task-panel" class="input-transfer-panel"><div id="input-task-transfer">${inputTaskTransferMarkup(taskId)}</div></div><div id="input-person-panel" class="hidden input-transfer-panel"><div id="input-member-transfer">${inputMemberTransferMarkup()}</div><div class="input-request-fields"><div class="form-row"><label>期望时间</label><input class="field" name="due" value="09-22"></div><div class="form-row"><label>需要提供的内容</label><input class="field" name="inputName" value="现场联调确认记录"></div></div><div class="notice">审批通过后向所有已选成员发送站内通知；不会自动创建新任务。</div></div></form>`;
    openModal("配置输入来源", `${taskId} · 两种来源互斥`, body, `<button class="btn" data-action="close-modal">取消</button><button class="btn primary" data-action="save-input-source">确认并建立关系</button>`);
    markTaskWorkflowModal(true);
  }

  function inputTaskCandidates(taskId) {
    return data.tasks.filter((task) => task.id !== taskId);
  }

  function inputTaskTransferMarkup(taskId) {
    const candidates=inputTaskCandidates(taskId), source=candidates.filter((task)=>!ui.inputTaskSelected.has(task.id)), selected=candidates.filter((task)=>ui.inputTaskSelected.has(task.id));
    const objectiveBlocks=data.objectives.map((objective)=>{
      const objectiveKrs=data.krs.filter((kr)=>kr.objectiveId===objective.id && source.some((task)=>task.krId===kr.id));
      const objectiveTasks=source.filter((task)=>objectiveKrs.some((kr)=>kr.id===task.krId));
      if(!objectiveTasks.length)return "";
      const allObjectiveMarked=objectiveTasks.every((task)=>ui.inputTaskSourceMarked.has(task.id));
      const krBlocks=objectiveKrs.map((kr)=>{
        const tasks=source.filter((task)=>task.krId===kr.id), allKrMarked=tasks.every((task)=>ui.inputTaskSourceMarked.has(task.id));
        return `<div class="input-tree-kr-block ${ui.inputTaskCollapsedKrs.has(kr.id)?"collapsed":""}" data-input-task-kr-block="${kr.id}"><div class="transfer-tree-group input-tree-kr"><button type="button" class="transfer-tree-toggle" data-action="toggle-input-task-kr" data-value="${kr.id}" aria-label="${ui.inputTaskCollapsedKrs.has(kr.id)?"展开":"收起"} ${kr.id}">${icon("down")}</button><label><input type="checkbox" data-input-task-source-kr value="${kr.id}" ${allKrMarked?"checked":""}><b>${kr.id} · ${esc(kr.title)}</b><span>${tasks.length} 项</span></label></div><div class="input-tree-kr-children">${tasks.map((task)=>`<label class="transfer-member-row transfer-task-row" data-input-task-source-item data-search="${esc(`${objective.id} ${objective.title} ${kr.id} ${kr.title} ${task.id} ${task.title} ${memberName(task.owner)} ${task.outputName}`.toLowerCase())}"><input type="checkbox" data-input-task-source value="${task.id}" ${ui.inputTaskSourceMarked.has(task.id)?"checked":""}><span class="transfer-task-code">${task.id}</span><span><b>${esc(task.title)}</b><small>${memberName(task.owner)} · ${esc(task.outputName)}</small></span></label>`).join("")}</div></div>`;
      }).join("");
      return `<div class="input-tree-o-block ${ui.inputTaskCollapsedObjectives.has(objective.id)?"collapsed":""}" data-input-task-o-block="${objective.id}"><div class="transfer-tree-group input-tree-o"><button type="button" class="transfer-tree-toggle" data-action="toggle-input-task-o" data-value="${objective.id}" aria-label="${ui.inputTaskCollapsedObjectives.has(objective.id)?"展开":"收起"} ${objective.id}">${icon("down")}</button><label><input type="checkbox" data-input-task-source-o value="${objective.id}" ${allObjectiveMarked?"checked":""}><b>${objective.id} · ${esc(objective.title)}</b><span>${objectiveTasks.length} 项</span></label></div><div class="input-tree-o-children">${krBlocks}</div></div>`;
    }).join("");
    const targetRows=selected.map((task)=>{const kr=getKr(task.krId),objective=getObjective(kr.objectiveId);return `<label class="transfer-member-row transfer-task-row transfer-task-target-row" data-input-task-target-item data-search="${esc(`${objective.id} ${kr.id} ${task.id} ${task.title} ${memberName(task.owner)} ${task.outputName}`.toLowerCase())}"><input type="checkbox" data-input-task-target value="${task.id}" ${ui.inputTaskTargetMarked.has(task.id)?"checked":""}><span class="transfer-task-code">${task.id}</span><span><b>${esc(task.title)}</b><small>${objective.id} / ${kr.id} · ${memberName(task.owner)}</small></span></label>`}).join("");
    const hiddenInputs=selected.map((task)=>`<input type="hidden" name="sourceTask" value="${task.id}">`).join("");
    return `<div class="member-transfer input-source-transfer">${hiddenInputs}<section class="transfer-panel"><div class="transfer-panel-head"><b>可选任务</b><span>${source.length} 项</span></div><label class="transfer-search">${icon("search")}<input id="input-task-source-search" type="search" placeholder="搜索 O、KR、任务、负责人或交付物" value="${esc(ui.inputTaskSourceSearch)}" autocomplete="off"></label><div class="transfer-tree" role="tree" aria-multiselectable="true">${objectiveBlocks}<div id="input-task-source-empty" class="transfer-empty hidden">没有匹配任务</div></div></section><div class="transfer-actions"><button id="input-task-transfer-add" type="button" data-action="input-task-transfer-add" aria-label="加入已选任务" disabled>${icon("chevron")}</button><button id="input-task-transfer-remove" type="button" data-action="input-task-transfer-remove" aria-label="移回可选任务" disabled>${icon("back")}</button></div><section class="transfer-panel"><div class="transfer-panel-head"><b>已选任务</b><span>${selected.length} 项</span></div><label class="transfer-search">${icon("search")}<input id="input-task-target-search" type="search" placeholder="搜索已选任务" value="${esc(ui.inputTaskTargetSearch)}" autocomplete="off"></label><div class="transfer-target-list" role="listbox" aria-multiselectable="true">${targetRows}<div id="input-task-target-empty" class="transfer-empty ${selected.length?"hidden":""}">${selected.length?"没有匹配任务":"请从左侧选择任务"}</div></div></section></div>`;
  }

  function inputMemberCandidates() {
    return data.members.filter((member)=>!["readonly","lead"].includes(member.permission));
  }

  function inputMemberTransferMarkup() {
    const candidates=inputMemberCandidates(), source=candidates.filter((member)=>!ui.inputMemberSelected.has(member.id)), selected=candidates.filter((member)=>ui.inputMemberSelected.has(member.id));
    const teams=[...new Set(source.map((member)=>member.team))];
    const teamBlocks=teams.map((team)=>{const members=source.filter((member)=>member.team===team),allMarked=members.every((member)=>ui.inputMemberSourceMarked.has(member.id));return `<div class="transfer-tree-group-block ${ui.inputMemberCollapsedTeams.has(team)?"collapsed":""}" data-input-member-team-block="${esc(team)}"><div class="transfer-tree-group"><button type="button" class="transfer-tree-toggle" data-action="toggle-input-member-team" data-value="${esc(team)}" aria-label="${ui.inputMemberCollapsedTeams.has(team)?"展开":"收起"} ${esc(team)}">${icon("down")}</button><label><input type="checkbox" data-input-member-source-team value="${esc(team)}" ${allMarked?"checked":""}><b>${esc(team)}</b><span>${members.length} 人</span></label></div><div class="transfer-tree-children">${members.map((member)=>`<label class="transfer-member-row" data-input-member-source-item data-search="${esc(`${member.displayName} ${member.role} ${member.team}`.toLowerCase())}"><input type="checkbox" data-input-member-source value="${member.id}" ${ui.inputMemberSourceMarked.has(member.id)?"checked":""}>${avatar(member.id)}<span><b>${esc(member.displayName)}</b><small>${esc(member.role)}</small></span></label>`).join("")}</div></div>`}).join("");
    const targetRows=selected.map((member)=>`<label class="transfer-member-row" data-input-member-target-item data-search="${esc(`${member.displayName} ${member.role} ${member.team}`.toLowerCase())}"><input type="checkbox" data-input-member-target value="${member.id}" ${ui.inputMemberTargetMarked.has(member.id)?"checked":""}>${avatar(member.id)}<span><b>${esc(member.displayName)}</b><small>${esc(member.team)} · ${esc(member.role)}</small></span></label>`).join("");
    const hiddenInputs=selected.map((member)=>`<input type="hidden" name="provider" value="${member.id}">`).join("");
    return `<div class="member-transfer input-source-transfer">${hiddenInputs}<section class="transfer-panel"><div class="transfer-panel-head"><b>可选成员</b><span>${source.length} 人</span></div><label class="transfer-search">${icon("search")}<input id="input-member-source-search" type="search" placeholder="搜索姓名、角色或团队" value="${esc(ui.inputMemberSourceSearch)}" autocomplete="off"></label><div class="transfer-tree" role="tree" aria-multiselectable="true">${teamBlocks}<div id="input-member-source-empty" class="transfer-empty hidden">没有匹配成员</div></div></section><div class="transfer-actions"><button id="input-member-transfer-add" type="button" data-action="input-member-transfer-add" aria-label="加入已选成员" disabled>${icon("chevron")}</button><button id="input-member-transfer-remove" type="button" data-action="input-member-transfer-remove" aria-label="移回可选成员" disabled>${icon("back")}</button></div><section class="transfer-panel"><div class="transfer-panel-head"><b>已选成员</b><span>${selected.length} 人</span></div><label class="transfer-search">${icon("search")}<input id="input-member-target-search" type="search" placeholder="搜索已选成员" value="${esc(ui.inputMemberTargetSearch)}" autocomplete="off"></label><div class="transfer-target-list" role="listbox" aria-multiselectable="true">${targetRows}<div id="input-member-target-empty" class="transfer-empty ${selected.length?"hidden":""}">${selected.length?"没有匹配成员":"请从左侧选择成员"}</div></div></section></div>`;
  }

  function renderInputTaskTransfer() { const root=$("#input-task-transfer"); if(!root)return; root.innerHTML=inputTaskTransferMarkup($("#input-source-form")?.dataset.taskId); filterInputTransfer("task","source",ui.inputTaskSourceSearch); filterInputTransfer("task","target",ui.inputTaskTargetSearch); updateInputTransferControls("task"); }
  function renderInputMemberTransfer() { const root=$("#input-member-transfer"); if(!root)return; root.innerHTML=inputMemberTransferMarkup(); filterInputTransfer("member","source",ui.inputMemberSourceSearch); filterInputTransfer("member","target",ui.inputMemberTargetSearch); updateInputTransferControls("member"); }

  function filterInputTransfer(kind,side,query="") {
    const root=$(kind==="task"?"#input-task-transfer":"#input-member-transfer");if(!root)return;const keyword=query.trim().toLowerCase(),prefix=`input-${kind}`;
    ui[kind==="task"?(side==="source"?"inputTaskSourceSearch":"inputTaskTargetSearch"):(side==="source"?"inputMemberSourceSearch":"inputMemberTargetSearch")]=query;
    let visible=0;$$(`[data-${prefix}-${side}-item]`,root).forEach((row)=>{const match=!keyword||row.dataset.search.includes(keyword);row.classList.toggle("hidden",!match);if(match)visible+=1;});
    if(side==="source"){
      if(kind==="task"){$$("[data-input-task-kr-block]",root).forEach((block)=>{const has=$$("[data-input-task-source-item]",block).some((row)=>!row.classList.contains("hidden"));block.classList.toggle("hidden",!has);block.classList.toggle("searching",Boolean(keyword));});$$("[data-input-task-o-block]",root).forEach((block)=>{const has=$$("[data-input-task-source-item]",block).some((row)=>!row.classList.contains("hidden"));block.classList.toggle("hidden",!has);block.classList.toggle("searching",Boolean(keyword));});}
      else{$$("[data-input-member-team-block]",root).forEach((block)=>{const has=$$("[data-input-member-source-item]",block).some((row)=>!row.classList.contains("hidden"));block.classList.toggle("hidden",!has);block.classList.toggle("searching",Boolean(keyword));});}
    }
    const empty=$(`#${prefix}-${side}-empty`);if(empty){const selectedSize=kind==="task"?ui.inputTaskSelected.size:ui.inputMemberSelected.size;empty.textContent=side==="target"&&!selectedSize?(kind==="task"?"请从左侧选择任务":"请从左侧选择成员"):(kind==="task"?"没有匹配任务":"没有匹配成员");empty.classList.toggle("hidden",visible>0);}
  }

  function updateInputTransferControls(kind) {
    const root=$(kind==="task"?"#input-task-transfer":"#input-member-transfer");if(!root)return;
    if(kind==="task"){
      const candidates=inputTaskCandidates($("#input-source-form")?.dataset.taskId).filter((task)=>!ui.inputTaskSelected.has(task.id));
      $$('[data-input-task-source-kr]',root).forEach((input)=>{const ids=candidates.filter((task)=>task.krId===input.value).map((task)=>task.id),marked=ids.filter((id)=>ui.inputTaskSourceMarked.has(id)).length;input.checked=ids.length>0&&marked===ids.length;input.indeterminate=marked>0&&marked<ids.length;});
      $$('[data-input-task-source-o]',root).forEach((input)=>{const krIds=data.krs.filter((kr)=>kr.objectiveId===input.value).map((kr)=>kr.id),ids=candidates.filter((task)=>krIds.includes(task.krId)).map((task)=>task.id),marked=ids.filter((id)=>ui.inputTaskSourceMarked.has(id)).length;input.checked=ids.length>0&&marked===ids.length;input.indeterminate=marked>0&&marked<ids.length;});
      const add=$("#input-task-transfer-add"),remove=$("#input-task-transfer-remove");if(add)add.disabled=!ui.inputTaskSourceMarked.size;if(remove)remove.disabled=!ui.inputTaskTargetMarked.size;
    } else {
      const candidates=inputMemberCandidates().filter((member)=>!ui.inputMemberSelected.has(member.id));
      $$('[data-input-member-source-team]',root).forEach((input)=>{const ids=candidates.filter((member)=>member.team===input.value).map((member)=>member.id),marked=ids.filter((id)=>ui.inputMemberSourceMarked.has(id)).length;input.checked=ids.length>0&&marked===ids.length;input.indeterminate=marked>0&&marked<ids.length;});
      const add=$("#input-member-transfer-add"),remove=$("#input-member-transfer-remove");if(add)add.disabled=!ui.inputMemberSourceMarked.size;if(remove)remove.disabled=!ui.inputMemberTargetMarked.size;
    }
  }

  function formatUploadSize(size=0){return size>=1024*1024?`${(size/1024/1024).toFixed(1)} MB`:`${Math.max(1,Math.round(size/1024))} KB`;}
  function uploadFileType(file){const ext=file.name.split(".").pop()?.toLowerCase();return ext==="pdf"?"PDF":["doc","docx"].includes(ext)?"Word":["xls","xlsx"].includes(ext)?"Excel":["ppt","pptx"].includes(ext)?"PowerPoint":["png","jpg","jpeg","gif","webp"].includes(ext)?"图片":ext==="zip"?"ZIP":"文件";}
  function uploadControl(id,{prompt="点击选择或将文件拖到此处",hint="支持 PDF、Office、图片或 ZIP，单个文件不超过 20MB",accept=".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.png,.jpg,.jpeg,.zip",confirmId=""}={}){
    return `<label id="${id}-zone" class="upload-zone" for="${id}">${icon("upload")}<strong>${esc(prompt)}</strong><span>${esc(hint)}</span><input id="${id}" data-upload-control data-confirm-id="${confirmId}" data-max-mb="20" name="uploadFile" type="file" accept="${accept}" required></label><div id="${id}-selection" class="upload-file-row hidden"><div><button id="${id}-name" type="button" class="upload-file-link" data-action="preview-local-upload" data-input-id="${id}"></button><span id="${id}-size"></span></div><button type="button" class="btn compact" data-action="clear-local-upload" data-input-id="${id}">删除</button></div>`;
  }
  function refreshUploadControl(input){
    let file=input.files?.[0];const maxMb=Number(input.dataset.maxMb||20),confirm=input.dataset.confirmId?$("#"+input.dataset.confirmId):null;
    if(file&&file.size>maxMb*1024*1024){input.value="";file=null;toast(`文件不能超过 ${maxMb}MB。`,"error");}
    $("#"+input.id+"-zone")?.classList.toggle("selected",Boolean(file));$("#"+input.id+"-selection")?.classList.toggle("hidden",!file);if(confirm)confirm.disabled=!file;
    if(file){$("#"+input.id+"-name").textContent=file.name;$("#"+input.id+"-size").textContent=`${formatUploadSize(file.size)} · ${uploadFileType(file)} · 已准备上传`;}
  }
  function clearLocalUpload(inputId){const input=$("#"+inputId);if(!input)return;input.value="";refreshUploadControl(input);}
  function previewLocalUpload(inputId){
    const file=$("#"+inputId)?.files?.[0];if(!file)return toast("请先选择文件。","error");closeLocalUploadPreview();const url=URL.createObjectURL(file),isImage=file.type.startsWith("image/"),isPdf=file.type==="application/pdf"||/\.pdf$/i.test(file.name),root=document.createElement("div");root.id="local-upload-preview-root";root.dataset.url=url;
    const preview=isImage?`<img class="local-preview-image" src="${url}" alt="${esc(file.name)}">`:isPdf?`<iframe class="local-preview-frame" src="${url}" title="${esc(file.name)}"></iframe>`:`<div class="file-preview"><span>${icon("report")}</span><b>${esc(file.name)}</b><p>${uploadFileType(file)} 文件预览 · ${formatUploadSize(file.size)}<br>原型以预览占位呈现，正式产品接入在线文档预览服务。</p></div>`;
    root.innerHTML=`<div class="modal-backdrop local-preview-backdrop" data-action="close-local-upload-preview"></div><section class="modal local-upload-preview"><div class="modal-head"><div><h2>文件预览</h2><p>${esc(file.name)} · ${formatUploadSize(file.size)}</p></div><button class="icon-btn" data-action="close-local-upload-preview" aria-label="关闭文件预览">${icon("close")}</button></div><div class="modal-body">${preview}</div><div class="modal-foot"><button class="btn primary" data-action="close-local-upload-preview">返回上传</button></div></section>`;document.body.appendChild(root);hydrateIcons(root);
  }
  function closeLocalUploadPreview(){const root=$("#local-upload-preview-root");if(!root)return;if(root.dataset.url)URL.revokeObjectURL(root.dataset.url);root.remove();}
  function candidateDraftRow(task, index=0) {
    return `<div class="candidate-draft-row"><div class="candidate-name"><label>交付物名称</label><input class="field" name="name" value="${esc(index ? `${task.outputName}补充说明` : task.outputName)}" required></div><div class="candidate-upload"><label>上传文件</label>${uploadControl(`completion-file-${index}`)}</div><button type="button" class="icon-btn candidate-remove" data-action="remove-candidate-row" aria-label="删除该交付物">${icon("close")}</button></div>`;
  }
  function submitCompletionModal(taskId) {
    const task = getTask(taskId); if (!task) return;
    const body = `<form id="completion-form" data-task-id="${taskId}" class="form-grid"><div class="form-row full"><label>本次候选交付物</label><div id="candidate-draft-list" class="candidate-draft-list">${candidateDraftRow(task)}</div><button type="button" class="btn compact candidate-add" data-action="add-candidate-row">${icon("plus")}继续添加交付物</button><span class="meta">本次申请中的多项内容整体通过或退回，不允许部分通过。</span></div><div class="form-row full"><label>可选中间审核人（或签）</label><div style="display:flex;gap:12px;padding:9px;border:1px solid var(--line);border-radius:6px"><label><input type="checkbox" name="reviewer" value="P10" ${task.middleReviewers?.includes("P10") ? "checked" : ""}> 陆川 · 专业评审</label><label><input type="checkbox" name="reviewer" value="P15" ${task.middleReviewers?.includes("P15") ? "checked" : ""}> 叶青 · 体验评审</label></div><span class="meta">可不选；多人时任一人通过即进入 KR 终审，任一人退回则整体退回。</span></div><div class="form-row full"><div class="notice">KR 负责人终审通过后，候选内容覆盖对应当前内容，被替换的旧文件永久删除，不保留历史。</div></div></form>`;
    openModal("提交成果与完成申请", "第三道审批：可选中间审核 + KR 最终闭环", body, `<button class="btn" data-action="close-modal">取消</button><button class="btn primary" data-action="confirm-completion">提交申请</button>`);
    markTaskWorkflowModal(true);
  }

  function reportRiskModal(taskId="") {
    const selected=taskId || data.tasks[0]?.id;
    const body = `<form id="risk-form" class="form-grid"><div class="form-row"><label>关联任务</label><select name="taskId" class="field">${data.tasks.map((t) => `<option value="${t.id}" ${t.id===selected?"selected":""}>${t.id} · ${esc(t.title)}</option>`).join("")}</select></div><div class="form-row"><label>希望谁行动</label><select name="actionOwner" class="field">${data.members.filter((m) => m.permission !== "readonly").map((m) => `<option value="${m.id}">${m.displayName} · ${m.role}</option>`).join("")}</select></div><div class="form-row full"><label>缺少的上游输入／条件</label><input name="missing" class="field" value="上游场地确认记录" required></div><div class="form-row full"><label>阻塞原因</label><textarea name="reason" class="field" required>上游提供时间晚于当前联调窗口，无法完成验证。</textarea></div><div class="form-row"><label>预计恢复时间（可选）</label><input name="restore" class="field" value="09-23"></div><div class="form-row"><label>类型／等级</label><select name="level" class="field"><option>预警</option><option>高风险</option></select></div><div class="form-row full"><div class="notice">提交后可一键提醒上游负责人；系统不会自动修改下游日期。</div></div></form>`;
    openModal("主动上报卡点", "由执行者说明事实与所需行动", body, `<button class="btn" data-action="close-modal">取消</button><button class="btn primary" data-action="submit-risk">上报并提醒</button>`);
    markTaskWorkflowModal();
  }

  function reviewDecisionModal(kind,id,approved) {
    const label=approved?"通过":"退回";
    const body=`<form id="review-decision-form" data-kind="${kind}" data-id="${id}" data-approved="${approved}"><label>${label}意见${approved?"（可选）":"（必填）"}</label><textarea class="field" name="opinion" ${approved?"":"required"} placeholder="${approved?"可补充审核意见":"请说明退回原因"}"></textarea>${kind==="final"&&approved?`<div class="notice warning" style="margin-top:12px">确认后，本次候选内容将覆盖同名当前交付物，被替换旧文件无法恢复。</div>`:""}</form>`;
    openModal(`${label}审核`, `${id} · 处理结果将记入审核事实`, body, `<button class="btn" data-action="close-modal">取消</button><button class="btn ${approved?"primary":"danger"}" data-action="confirm-review-decision">确认${label}</button>`);
    markTaskWorkflowModal();
  }

  function previewFile(id) {
    const item=getDeliverable(id); if(!item)return toast("文件已按覆盖或退回规则删除。","error");
    const supported=/PDF|图片|文本|Word|Excel|PowerPoint/.test(item.fileType || "");
    openModal("交付物预览", `${item.name} · ${item.fileType || "文件"}`, supported?`<div class="file-preview"><span>${icon("report")}</span><b>${esc(item.file)}</b><p>此区域模拟 ${esc(item.fileType || "文件")} 在线预览。原型不传输真实文件。</p></div>`:`<div class="empty">该格式不支持在线预览，可下载查看。</div>`,`<button class="btn" data-action="close-modal">关闭</button><button class="btn primary" data-action="download-file" data-id="${id}">${icon("download")}下载</button>`);
  }

  const PROJECT_YEAR = "2026";
  function assignableMembers(includeProjectLead = true) {
    return data.members.filter((member) => member.permission !== "readonly" && (includeProjectLead || member.id !== "P01"));
  }
  function memberDatalist(id, includeProjectLead = true) {
    return `<datalist id="${id}">${assignableMembers(includeProjectLead).map((member) => `<option value="${esc(member.displayName)}">${esc(member.role)} · ${esc(member.team)}</option>`).join("")}</datalist>`;
  }
  function memberSearchInput(name, selectedId, listId, label = "负责人") {
    return `<input class="field member-search-input" name="${name}" value="${esc(memberName(selectedId))}" list="${listId}" placeholder="输入姓名、角色或团队" aria-label="${label}" autocomplete="off" required>`;
  }
  function resolveMemberInput(value, includeProjectLead = true) {
    const query = String(value || "").trim().toLowerCase(), members = assignableMembers(includeProjectLead);
    const exact = members.find((member) => member.id.toLowerCase() === query || member.displayName.toLowerCase() === query);
    if (exact) return exact.id;
    const matches = members.filter((member) => `${member.displayName} ${member.role} ${member.team}`.toLowerCase().includes(query));
    return query && matches.length === 1 ? matches[0].id : "";
  }
  function dateInputValue(value, fallback = "2026-09-01") {
    const text = String(value || "").trim().replace(/\./g, "-");
    if (/^\d{4}-\d{2}-\d{2}$/.test(text)) return text;
    if (/^\d{2}-\d{2}$/.test(text)) return `${PROJECT_YEAR}-${text}`;
    return fallback;
  }
  function cycleDateParts(value) {
    const parts = String(value || "").split(/[—~至]/).map((part) => part.trim());
    return [dateInputValue(parts[0], "2026-09-01"), dateInputValue(parts[1], "2026-09-30")];
  }
  function compactDate(value) { return String(value || "").slice(5); }
  function cycleValue(start, due) { return `${compactDate(start).replace("-", ".")}—${compactDate(due).replace("-", ".")}`; }
  function draftObjectiveIds() {
    return [...data.objectives.map((o) => o.id), ...$$('.okr-sheet-row[data-type="o"] input[name="code"]', $("#modal-root")).map((input) => input.value.trim()).filter(Boolean)];
  }
  function okrParentOptions(selected = "") {
    return draftObjectiveIds().map((id) => `<option value="${esc(id)}" ${id === selected ? "selected" : ""}>${esc(id)}</option>`).join("");
  }
  function okrDraftRow(type, code, parent = "") {
    const isO = type === "o";
    return `<div class="okr-sheet-row" data-type="${type}">
      <div class="okr-sheet-cell">${isO ? `<div class="sheet-input-pair"><input class="field okr-code" name="code" value="${code}" aria-label="O 编号"><input class="field" name="title" value="新增项目目标" aria-label="O 目标描述"></div>` : `<span class="sheet-placeholder">—</span>`}</div>
      <div class="okr-sheet-cell">${isO ? `<span class="sheet-placeholder">该行用于创建 O</span>` : `<div class="sheet-input-pair"><input class="field okr-code" name="code" value="${code}" aria-label="KR 编号"><input class="field" name="title" value="新增关键结果" aria-label="KR 目标描述"></div>`}</div>
      <div class="okr-sheet-cell">${isO ? `<span class="sheet-placeholder">项目目标</span>` : `<select class="field" name="objectiveId" aria-label="所属 O">${okrParentOptions(parent)}</select>`}</div>
      <div class="okr-sheet-cell">${memberSearchInput("owner", isO ? "P01" : "P03", "okr-member-options")}</div>
      <div class="okr-sheet-cell">${isO ? `<span class="sheet-placeholder">沿用项目周期</span>` : `<div class="sheet-date-range" role="group" aria-label="周期日期范围"><input type="date" name="cycleStart" value="2026-09-01" max="2026-09-30" aria-label="周期开始日期"><span aria-hidden="true">—</span><input type="date" name="cycleDue" value="2026-09-30" min="2026-09-01" aria-label="周期结束日期"></div>`}</div>
      <button type="button" class="icon-btn" data-action="remove-okr-row" aria-label="删除该行">${icon("close")}</button>
    </div>`;
  }
  function addOkrModal() {
    const nextO = `O${Math.max(0, ...data.objectives.map((o) => Number(o.id.replace(/\D/g, "")) || 0)) + 1}`;
    const nextKr = `KR${Math.max(0, ...data.krs.map((kr) => Number(kr.id.replace(/\D/g, "")) || 0)) + 1}`;
    const body = `<form id="add-okr-form">${memberDatalist("okr-member-options")}<div class="notice">O、KR 仍由线下确定；这里仅用于一次性连续录入结构和负责人。</div><div class="okr-sheet" style="margin-top:12px"><div class="okr-sheet-head"><span>目标 O</span><span>关键结果 KR</span><span>所属 O</span><span>负责人</span><span>周期</span><span></span></div><div id="okr-sheet-rows">${okrDraftRow("o", nextO)}${okrDraftRow("kr", nextKr, nextO)}</div></div><div class="okr-sheet-actions"><button type="button" class="btn compact" data-action="add-okr-row" data-value="o">${icon("plus")}添加 O 事项</button><button type="button" class="btn compact" data-action="add-okr-row" data-value="kr">${icon("plus")}添加 KR 事项</button></div></form>`;
    openModal("新增 O / KR", "横向区分 O 与 KR，向下连续增加事项并指定负责人", body, `<button class="btn" data-action="close-modal">取消</button><button class="btn primary" data-action="save-okr-sheet">保存并通知负责人</button>`);
    $(".modal").classList.add("modal-wide", "okr-modal");
    refreshOkrParentOptions();
    const initialParent = $('.okr-sheet-row[data-type="kr"] select[name="objectiveId"]', $("#modal-root"));
    if (initialParent) initialParent.value = nextO;
  }
  function addOkrDraftRow(type) {
    const rows = $$(".okr-sheet-row", $("#modal-root"));
    const base = type === "o" ? Math.max(0, ...data.objectives.map((o) => Number(o.id.replace(/\D/g, "")) || 0)) : Math.max(0, ...data.krs.map((kr) => Number(kr.id.replace(/\D/g, "")) || 0));
    const count = rows.filter((row) => row.dataset.type === type).length;
    const code = `${type === "o" ? "O" : "KR"}${base + count + 1}`;
    $("#okr-sheet-rows").insertAdjacentHTML("beforeend", okrDraftRow(type, code, draftObjectiveIds().at(-1)));
    hydrateIcons($("#okr-sheet-rows"));
    refreshOkrParentOptions();
  }
  function refreshOkrParentOptions() {
    const options = draftObjectiveIds();
    $$('.okr-sheet-row[data-type="kr"] select[name="objectiveId"]', $("#modal-root")).forEach((select) => { const current = select.value; select.innerHTML = options.map((id) => `<option value="${esc(id)}" ${id === current ? "selected" : ""}>${esc(id)}</option>`).join(""); });
  }
  function saveOkrSheet() {
    const rows = $$(".okr-sheet-row", $("#add-okr-form"));
    if (!rows.length) return toast("请至少添加一个 O 或 KR 事项。", "error");
    const drafts = rows.map((row) => ({ type: row.dataset.type, code: $("input[name=code]", row)?.value.trim(), title: $("input[name=title]", row)?.value.trim(), objectiveId: $("select[name=objectiveId]", row)?.value || "", owner: resolveMemberInput($("input[name=owner]", row)?.value), cycleStart:$("input[name=cycleStart]", row)?.value || "", cycleDue:$("input[name=cycleDue]", row)?.value || "" }));
    if (drafts.some((item) => !item.code || !item.title)) return toast("请填写每一行的编号和目标描述。", "error");
    if (drafts.some((item) => !item.owner)) return toast("请从匹配结果中选择有效负责人。", "error");
    const allCodes = [...data.objectives.map((o) => o.id), ...data.krs.map((kr) => kr.id), ...drafts.map((item) => item.code)];
    if (new Set(allCodes).size !== allCodes.length) return toast("O / KR 编号不能重复，请修改后保存。", "error");
    const validObjectives = new Set([...data.objectives.map((o) => o.id), ...drafts.filter((item) => item.type === "o").map((item) => item.code)]);
    if (drafts.some((item) => item.type === "kr" && !validObjectives.has(item.objectiveId))) return toast("请为每个 KR 选择有效的所属 O。", "error");
    drafts.filter((item) => item.type === "o").forEach((item) => data.objectives.push({ id:item.code, title:item.title, note:"待补充目标说明", owner:item.owner }));
    drafts.filter((item) => item.type === "kr").forEach((item) => data.krs.push({ id:item.code, title:item.title, objectiveId:item.objectiveId, owner:item.owner, risk:"normal", cycle:cycleValue(item.cycleStart,item.cycleDue), metric:"待补充量化指标" }));
    const noticesByOwner = new Map();
    drafts.forEach((item) => {
      if (item.owner === ui.identity) return;
      if (!noticesByOwner.has(item.owner)) noticesByOwner.set(item.owner, []);
      noticesByOwner.get(item.owner).push(item.code);
    });
    data.okrNotifications ||= [];
    let noticeSequence = data.okrNotifications.length;
    noticesByOwner.forEach((codes, receiver) => data.okrNotifications.unshift({
      id:`ON${String(++noticeSequence).padStart(2,"0")}`,
      sender:ui.identity,
      receiver,
      codes,
      state:"未读",
      createdAt:"刚刚"
    }));
    saveData(`新增 ${drafts.filter((item) => item.type === "o").length} 个 O、${drafts.filter((item) => item.type === "kr").length} 个 KR，并通知 ${noticesByOwner.size} 位负责人`);
    closeModal(); renderPage();
    toast(noticesByOwner.size ? `O / KR 已保存，并已通知 ${noticesByOwner.size} 位负责人。` : "O / KR 已保存；本次负责人均为你本人。", "success");
  }
  function openOkrEditorDrawer(kind, id) {
    if (!canManage()) return;
    const item = kind === "objective" ? getObjective(id) : getKr(id);
    if (!item) return;
    const [cycleStart, cycleDue] = cycleDateParts(item.cycle);
    const body = kind === "objective" ? `<form id="okr-item-form" data-kind="objective" data-id="${id}" class="form-grid">${memberDatalist("okr-editor-members")}<div class="form-row full"><label>O 名称</label><input class="field" name="title" value="${esc(item.title)}" required></div><div class="form-row full"><label>目标说明</label><textarea class="field" name="note">${esc(item.note || "")}</textarea></div><div class="form-row full"><label>负责人</label>${memberSearchInput("owner", item.owner || data.project.lead, "okr-editor-members")}</div></form>` : `<form id="okr-item-form" data-kind="kr" data-id="${id}" class="form-grid">${memberDatalist("okr-editor-members")}<div class="form-row full"><label>KR 名称</label><input class="field" name="title" value="${esc(item.title)}" required></div><div class="form-row full"><label>负责人</label>${memberSearchInput("owner", item.owner, "okr-editor-members")}</div><div class="form-row"><label>周期开始</label><input type="date" class="field" name="cycleStart" value="${cycleStart}"></div><div class="form-row"><label>周期截止</label><input type="date" class="field" name="cycleDue" value="${cycleDue}"></div><div class="form-row full"><label>量化标准</label><input class="field" name="metric" value="${esc(item.metric)}"></div></form>`;
    openDrawer(`编辑 ${id}`, kind === "objective" ? "项目目标 O" : `所属 ${item.objectiveId} · 关键结果 KR`, body, `<button class="btn" data-action="close-drawer">取消</button><button class="btn primary" data-action="save-okr-item">保存修改</button>`);
  }
  function saveOkrItem() {
    const form = $("#okr-item-form"); if (!form?.reportValidity()) return;
    const fd = new FormData(form), kind = form.dataset.kind, id = form.dataset.id, item = kind === "objective" ? getObjective(id) : getKr(id);
    if (!item) return;
    const owner = resolveMemberInput(fd.get("owner")); if (!owner) return toast("请从匹配结果中选择有效负责人。", "error");
    item.title = fd.get("title"); item.owner = owner;
    if (kind === "objective") item.note = fd.get("note");
    else { item.cycle = cycleValue(fd.get("cycleStart"), fd.get("cycleDue")); item.metric = fd.get("metric"); }
    saveData(`编辑 ${id}`); renderPage(); toast(`${id} 已更新。`, "success");
  }

  function importOkrModal() {
    const body = `${uploadControl("okr-import-file",{prompt:"上传 OKR 表格文件",hint:"点击选择或拖入 .xlsx、.xls、.csv 文件",accept:".xlsx,.xls,.csv",confirmId:"confirm-import-button"})}<div class="approval-lane"><div class="approval-node"><b>1. 上传文件</b><span>等待选择</span></div><span class="approval-arrow">→</span><div class="approval-node"><b>2. 字段映射</b><span>自动识别列名</span></div><span class="approval-arrow">→</span><div class="approval-node"><b>3. 导入预览</b><span>确认后写入结构</span></div></div><div class="data-table-wrap" style="margin-top:12px"><table class="data-table" style="min-width:520px"><thead><tr><th>原表字段</th><th>系统字段</th><th>示例</th></tr></thead><tbody><tr><td>目标编号</td><td>O 编号</td><td>O4</td></tr><tr><td>关键结果</td><td>KR 目标描述</td><td>形成试运行方案</td></tr><tr><td>责任人</td><td>KR 负责人</td><td>姜雨</td></tr></tbody></table></div><div class="notice" style="margin-top:12px">导入只用于建立 O / KR 结构；导入后仍在当前系统中维护，不转为表格产品。</div>`;
    openModal("导入 OKR 表格", "先上传文件，再完成字段映射与导入预览", body, `<button class="btn" data-action="close-modal">取消</button><button id="confirm-import-button" class="btn primary" data-action="confirm-import" disabled>确认导入</button>`);
    $(".modal").classList.add("modal-wide");
  }

  function processAttachmentModal(){
    const body=`<form id="process-attachment-form" class="form-grid"><div class="form-row full"><label>过程文件名称</label><input name="name" class="field" value="联调过程记录" required></div><div class="form-row full"><label>上传文件</label>${uploadControl("process-attachment-file",{confirmId:"confirm-process-upload-button"})}</div><div class="form-row full"><div class="notice">过程文件用于协作记录，不作为正式交付物；确认上传前可预览或删除本次选择。</div></div></form>`;
    openModal("上传过程文件","成果与归档 · 协作过程记录",body,`<button class="btn" data-action="close-modal">取消</button><button id="confirm-process-upload-button" class="btn primary" data-action="confirm-process-upload" disabled>确认上传</button>`);
  }
  function confirmProcessUpload(){const form=$("#process-attachment-form"),file=$("#process-attachment-file")?.files?.[0];if(!form?.reportValidity()||!file)return toast("请先选择需要上传的文件。","error");data.processAttachments ||= [];data.processAttachments.push({id:`PA${String(data.processAttachments.length+1).padStart(2,"0")}`,name:new FormData(form).get("name"),file:file.name,fileType:uploadFileType(file),fileSize:formatUploadSize(file.size),owner:ui.identity,createdAt:"刚刚"});saveData(`上传过程文件 ${file.name}`);closeModal();toast(`${file.name} 已上传为过程文件。`,"success");}

  function generatePackageModal() {
    const selected = [...ui.artifactSelected].map(getDeliverable).filter(Boolean);
    const body = `<form id="package-form" class="form-grid"><div class="form-row full"><label>成果包名称</label><input name="name" class="field" value="联合联调阶段成果包"></div><div class="form-row"><label>范围</label><input name="scope" class="field" value="跨 KR / 联调阶段"></div><div class="form-row"><label>成果包标识</label><input name="version" class="field" value="V1.0"></div><div class="form-row full"><label>自动目录预览</label><div class="card" style="padding:9px">${selected.map((x,i) => `<div class="property"><label>${i+1}. ${x.krId} / ${x.taskId}</label><strong>${esc(x.name)} · 当前内容</strong></div>`).join("")}</div></div><div class="form-row full"><div class="notice">成果包保留目录和来源事实；交付物被更新后，下载时使用新的当前内容，不复制旧文件。</div></div></form>`;
    openModal("生成阶段成果包", `已选择 ${selected.length} 项当前已生效交付物`, body, `<button class="btn" data-action="close-modal">取消</button><button class="btn primary" data-action="confirm-package">生成成果包</button>`);
  }

  function showIdentityMenu() {
    const root = $("#popover-root");
    if (root.querySelector(".identity-popover")) return closePopover();
    root.innerHTML = `<div class="popover identity-popover"><div class="popover-head"><span>切换演示身份</span><span class="meta">同一项目事实</span></div>${["P01","P02","P03","P06","P10","P14","P18"].map((id) => { const m = getMember(id); return `<button class="popover-item" data-action="select-identity" data-id="${id}">${avatar(id)}<span style="flex:1"><b>${m.displayName}</b><span>${m.role} · ${m.team}</span></span>${ui.identity === id ? icon("check") : ""}</button>`; }).join("")}</div>`;
    hydrateIcons(root);
  }
  function showNotifications() {
    const items = data.inputRequests.filter((x) => x.provider === ui.identity && ["待接收","已接收"].includes(x.state));
    const taskInvites = (data.taskInvites || []).filter((x) => x.invitee === ui.identity && x.state === "待处理");
    const okrNotifications = (data.okrNotifications || []).filter((x) => x.receiver === ui.identity);
    $("#popover-root").innerHTML = `<div class="popover"><div class="popover-head"><span>通知</span><span class="meta">${items.length + taskInvites.length + okrNotifications.length + 2} 条</span></div>${okrNotifications.map((notice) => `<button class="popover-item" data-route="overview"><span class="work-kind" style="width:28px;height:28px">O</span><span><b>${memberName(notice.sender)} 为你分配了 O / KR</b><span>${notice.codes.map(esc).join("、")} · 结构已保存，请查看并推进</span></span></button>`).join("")}${taskInvites.map((invite) => `<button class="popover-item" data-route="tasks"><span class="work-kind" style="width:28px;height:28px">任</span><span><b>${memberName(invite.inviter)} 邀请你创建任务</b><span>${invite.krId} · ${esc(invite.note)}</span></span></button>`).join("")}${items.map((r) => `<button class="popover-item" data-action="go-input-request"><span class="work-kind" style="width:28px;height:28px">输</span><span><b>${memberName(r.requester)} 请求你提供输入</b><span>${r.inputName} · ${r.state}</span></span></button>`).join("")}<button class="popover-item" data-route="mywork"><span class="work-kind" style="width:28px;height:28px">办</span><span><b>查看我的全部待办</b><span>审批、终审、接收与卡点</span></span></button></div>`;
  }

  function handleClick(event) {
    if ($("#popover-root .popover") && !event.target.closest("#popover-root .popover") && !event.target.closest('[data-action="identity-menu"], [data-action="notifications"]')) closePopover();
    const routeEl = event.target.closest("[data-route]");
    if (routeEl) { location.hash = routeEl.dataset.route; return; }
    const el = event.target.closest("[data-action]");
    if (!el) return;
    const action = el.dataset.action, id = el.dataset.id, value = el.dataset.value, kind = el.dataset.kind;
    const simple = {
      "close-drawer": closeDrawer, "close-modal": closeModal, "close-sidebar": () => $("#sidebar").classList.remove("open"),
      "toggle-sidebar": () => $("#sidebar").classList.toggle("open"), "identity-menu": showIdentityMenu,
      notifications: showNotifications, "create-task": createTaskModal, "invite-owner": inviteTaskOwnersModal,
      "import-okr": importOkrModal, "add-okr": addOkrModal, "generate-package": generatePackageModal,
    };
    if (simple[action]) { simple[action](); return; }
    if (action === "open-okr-management") { if (canManage()) location.hash = "overview?mode=okr"; }
    else if (action === "back-overview") { location.hash = "overview"; }
    else if (action === "toggle-kr") { ui.expandedKrs.has(id) ? ui.expandedKrs.delete(id) : ui.expandedKrs.add(id); renderPage(); }
    else if (action === "task-detail") { if (!$("#drawer-root .task-tabs")) ui.taskDrawerSource=route(); ui.taskDrawerContext=null; openTaskDrawer(id); }
    else if (action === "work-detail") { ui.taskDrawerSource="mywork"; const context={source:"mywork",sourceGroup:el.dataset.source,focusType:el.dataset.focusType,focusId:el.dataset.focusId}; openTaskDrawer(id,el.dataset.tab || "overview",context); }
    else if (action === "task-tab") openTaskDrawer(id,value,ui.taskDrawerContext);
    else if (action === "handle-invite") createTaskModal(el.dataset.inviteId);
    else if (action === "work-remind") sendWorkReminder(el.dataset.taskId,el.dataset.target);
    else if (action === "abandon-change") abandonRejectedChange(id);
    else if (action === "submit-discussion") submitDiscussion();
    else if (action === "edit-task-progress") startTaskProgressEdit(id);
    else if (action === "confirm-task-progress") confirmTaskProgress(id);
    else if (action === "edit-task") openTaskEditorDrawer(id);
    else if (action === "submit-task-edit") submitTaskEdit();
    else if (action === "artifact-detail") openArtifactDrawer(id);
    else if (action === "select-identity") { ui.identity = id; localStorage.setItem(IDENTITY_KEY,id); closePopover(); renderIdentity(); renderPage(); toast(`已切换为 ${name(id)}；项目事实保持不变。`,"success"); }
    else if (action === "my-tab") { ui.myTab = value; renderPage(); }
    else if (action === "settings-tab") { ui.settingsTab = value; renderPage(); }
    else if (action === "report-period") { ui.reportPeriod = value; renderPage(); }
    else if (action === "add-okr-row") addOkrDraftRow(value);
    else if (action === "remove-okr-row") { el.closest(".okr-sheet-row")?.remove(); refreshOkrParentOptions(); }
    else if (action === "add-task-row") addTaskDraftRow();
    else if (action === "remove-task-row") { const rows = $$(".task-sheet-row", $("#create-task-form")); if (rows.length === 1) return toast("至少保留一行任务。", "error"); el.closest(".task-sheet-row")?.remove(); }
    else if (action === "toggle-invite-tree-group") { ui.inviteMemberCollapsedRoles.has(value) ? ui.inviteMemberCollapsedRoles.delete(value) : ui.inviteMemberCollapsedRoles.add(value); renderInviteMemberTransfer(); }
    else if (action === "invite-transfer-add") { ui.inviteMemberSourceMarked.forEach((memberId) => ui.inviteMemberSelected.add(memberId)); ui.inviteMemberSourceMarked.clear(); renderInviteMemberTransfer(); }
    else if (action === "invite-transfer-remove") { ui.inviteMemberTargetMarked.forEach((memberId) => ui.inviteMemberSelected.delete(memberId)); ui.inviteMemberTargetMarked.clear(); renderInviteMemberTransfer(); }
    else if (action === "toggle-input-task-o") { ui.inputTaskCollapsedObjectives.has(value)?ui.inputTaskCollapsedObjectives.delete(value):ui.inputTaskCollapsedObjectives.add(value); renderInputTaskTransfer(); }
    else if (action === "toggle-input-task-kr") { ui.inputTaskCollapsedKrs.has(value)?ui.inputTaskCollapsedKrs.delete(value):ui.inputTaskCollapsedKrs.add(value); renderInputTaskTransfer(); }
    else if (action === "input-task-transfer-add") { ui.inputTaskSourceMarked.forEach((taskId)=>ui.inputTaskSelected.add(taskId));ui.inputTaskSourceMarked.clear();renderInputTaskTransfer(); }
    else if (action === "input-task-transfer-remove") { ui.inputTaskTargetMarked.forEach((taskId)=>ui.inputTaskSelected.delete(taskId));ui.inputTaskTargetMarked.clear();renderInputTaskTransfer(); }
    else if (action === "toggle-input-member-team") { ui.inputMemberCollapsedTeams.has(value)?ui.inputMemberCollapsedTeams.delete(value):ui.inputMemberCollapsedTeams.add(value);renderInputMemberTransfer(); }
    else if (action === "input-member-transfer-add") { ui.inputMemberSourceMarked.forEach((memberId)=>ui.inputMemberSelected.add(memberId));ui.inputMemberSourceMarked.clear();renderInputMemberTransfer(); }
    else if (action === "input-member-transfer-remove") { ui.inputMemberTargetMarked.forEach((memberId)=>ui.inputMemberSelected.delete(memberId));ui.inputMemberTargetMarked.clear();renderInputMemberTransfer(); }
    else if (action === "add-candidate-row") { const task=getTask($("#completion-form")?.dataset.taskId); if(task){const count=$$(".candidate-draft-row",$("#completion-form")).length;$("#candidate-draft-list").insertAdjacentHTML("beforeend",candidateDraftRow(task,count));hydrateIcons($("#candidate-draft-list"));} }
    else if (action === "remove-candidate-row") { const rows=$$(".candidate-draft-row",$("#completion-form")); if(rows.length===1)return toast("至少保留一项候选交付物。","error");el.closest(".candidate-draft-row")?.remove(); }
    else if (action === "save-okr-sheet") saveOkrSheet();
    else if (action === "edit-okr-row") openOkrEditorDrawer(kind, id);
    else if (action === "save-okr-item") saveOkrItem();
    else if (action === "graph-view") { ui.graphView = value; renderPage(); }
    else if (action === "graph-mode") { ui.graphMode = value; if(value === "aggregate") ui.graphSelected = "KR1"; renderPage(); }
    else if (action === "graph-focus") focusGraph(id);
    else if (action === "focus-graph") { closeLayers(); focusGraph(id); }
    else if (action === "focus-relation") { const rel = data.relations.find((r)=>r.id===id); if(rel) focusGraph(rel.to); }
    else if (action === "graph-back") { const prev = ui.graphHistory.pop(); if(prev){ ui.graphMode=prev.mode;ui.graphSelected=prev.selected;renderPage(); } }
    else if (action === "clear-graph-selection") { ui.graphSelected="";renderPage(); }
    else if (action === "graph-zoom") { ui.graphZoom = Math.max(.45,Math.min(2.2,ui.graphZoom + (value === "in" ? .15 : -.15))); updateGraphTransform(); }
    else if (action === "graph-fit") { ui.graphZoom=1;ui.graphPan={x:0,y:0};updateGraphTransform();toast("已适应当前画布。","success"); }
    else if (action === "configure-input") configureInputModal(id);
    else if (action === "input-mode") setInputMode(value,el);
    else if (action === "save-input-source") saveInputSource();
    else if (action === "submit-completion") submitCompletionModal(id);
    else if (action === "confirm-completion") confirmCompletion();
    else if (action === "submit-new-task") submitNewTask();
    else if (action === "send-task-invites") sendTaskInvites();
    else if (action === "approve-entry") decideEntry(id,true);
    else if (action === "reject-entry") decideEntry(id,false);
    else if (action === "approve-change") decideChange(id,true);
    else if (action === "reject-change") decideChange(id,false);
    else if (action === "approve-middle") decideMiddle(id,true);
    else if (action === "reject-middle") decideMiddle(id,false);
    else if (action === "approve-final") decideFinal(id,true);
    else if (action === "reject-final") decideFinal(id,false);
    else if (action === "accept-input") acceptInput(id);
    else if (action === "provide-input") provideInputModal(id);
    else if (action === "confirm-receipt") confirmReceipt(id);
    else if (action === "report-risk") reportRiskModal(id);
    else if (action === "review-decision") reviewDecisionModal(kind,id,value==="approve");
    else if (action === "confirm-review-decision") confirmReviewDecision();
    else if (action === "preview-file") previewFile(id);
    else if (action === "remind-input") toast("已发送带任务、缺失输入、截止时间和影响范围的提醒。","success");
    else if (action === "remind-risk") toast("已向当前待行动人发送带上下文提醒。","success");
    else if (action === "risk-actions") riskActionsModal(id);
    else if (action === "submit-risk") submitRisk();
    else if (action === "clear-artifact-selection") { ui.artifactSelected.clear();renderPage(); }
    else if (action === "confirm-package") confirmPackage();
    else if (action === "package-detail") packageDetail(id);
    else if (action === "download-package") downloadPackage(id);
    else if (action === "download-file") { const item=getDeliverable(id); if(!item)return toast("文件已按覆盖或退回规则删除。","error");downloadText(item.file || `${id}-demo.txt`,`这是脱敏原型中的模拟当前交付物，用于验证下载行为。`); }
    else if (action === "export-report") exportReport(el.dataset.type);
    else if (action === "publish-report") { saveData("发布项目报告固定版本");toast("已发布带时间戳的固定报告版本。","success"); }
    else if (action === "reset-data") resetData();
    else if (action === "confirm-import") { const file = $("#okr-import-file")?.files?.[0]; if (!file) return toast("请先上传一个 OKR 表格文件。", "error"); closeModal();toast(`${file.name} 已完成字段映射与导入预览。`,"success"); }
    else if (action === "upload-attachment") processAttachmentModal();
    else if (action === "confirm-process-upload") confirmProcessUpload();
    else if (action === "preview-local-upload") previewLocalUpload(el.dataset.inputId);
    else if (action === "clear-local-upload") clearLocalUpload(el.dataset.inputId);
    else if (action === "close-local-upload-preview") closeLocalUploadPreview();
    else if (action === "copy-okr") { navigator.clipboard?.writeText("O / KR 演示数据");toast("已复制脱敏 OKR 行。","success"); }
    else if (action === "more-feedback" || action === "project-menu" || action === "open-search" || action === "invite-member") genericFeedback(action);
    else if (action === "toggle-setting") el.classList.toggle("on");
    else if (action === "download-audit") downloadText("audit-v4.3.txt", data.audit.map((a)=>`${a.time}\t${memberName(a.actor)}\t${a.action.replace(/P\d{2}/g,(id)=>memberName(id))}`).join("\n"));
    else if (action === "go-input-request") { ui.myTab="todo";location.hash="mywork";closePopover(); }
  }

  function handleInput(event) {
    if (event.target.matches('.okr-sheet-row[data-type="o"] input[name="code"]')) refreshOkrParentOptions();
    if (event.target.id === "task-search") { ui.taskSearch = event.target.value; renderPage(); }
    if (event.target.id === "graph-search") {
      ui.graphSearch = event.target.value;
      const q = event.target.value.trim().toLowerCase();
      if (q.length > 1) {
        const match = [...data.tasks,...data.krs,...data.objectives].find((x) => `${x.id}${x.title}`.toLowerCase().includes(q));
        if (match) ui.graphSelected = match.id;
      }
    }
    if (event.target.id === "invite-member-source-search") filterInviteMemberTransfer("source", event.target.value);
    if (event.target.id === "invite-member-target-search") filterInviteMemberTransfer("target", event.target.value);
    if (event.target.id === "input-task-source-search") filterInputTransfer("task","source",event.target.value);
    if (event.target.id === "input-task-target-search") filterInputTransfer("task","target",event.target.value);
    if (event.target.id === "input-member-source-search") filterInputTransfer("member","source",event.target.value);
    if (event.target.id === "input-member-target-search") filterInputTransfer("member","target",event.target.value);
  }
  function handleChange(event) {
    if (event.target.matches("input[data-upload-control]")) refreshUploadControl(event.target);
    if (event.target.matches('.sheet-date-range input[name="cycleStart"]')) {
      const due = $('input[name="cycleDue"]', event.target.closest(".sheet-date-range"));
      if (due) { due.min = event.target.value; if (due.value < event.target.value) due.value = event.target.value; }
    }
    if (event.target.matches('.sheet-date-range input[name="cycleDue"]')) {
      const start = $('input[name="cycleStart"]', event.target.closest(".sheet-date-range"));
      if (start) { start.max = event.target.value; if (start.value > event.target.value) start.value = event.target.value; }
    }
    if (event.target.matches('.task-sheet-date-range input[name="start"]')) {
      const range=event.target.closest(".task-sheet-date-range"), due=$('input[name="due"]',range);
      if (due) { due.min=event.target.value; if (due.value<event.target.value) due.value=event.target.value; event.target.max=due.value; }
    }
    if (event.target.matches('.task-sheet-date-range input[name="due"]')) {
      const range=event.target.closest(".task-sheet-date-range"), start=$('input[name="start"]',range);
      if (start) { start.max=event.target.value; if (start.value>event.target.value) start.value=event.target.value; event.target.min=start.value; }
    }
    if (event.target.id === "task-kr") { ui.taskKr = event.target.value; renderPage(); }
    if (event.target.id === "task-status") { ui.taskStatus = event.target.value; renderPage(); }
    if (["graph-o","graph-kr","graph-person","graph-risk","graph-relation","graph-time"].includes(event.target.id)) {
      const map = {"graph-o":"graphO","graph-kr":"graphKr","graph-person":"graphPerson","graph-risk":"graphRisk","graph-relation":"graphRelation","graph-time":"graphTime"};
      ui[map[event.target.id]] = event.target.value;
      if (event.target.id === "graph-o") ui.graphKr = "all";
      renderPage();
    }
    if (event.target.classList.contains("artifact-check")) { event.target.checked ? ui.artifactSelected.add(event.target.value) : ui.artifactSelected.delete(event.target.value); renderPage(); }
    if (event.target.matches("[data-invite-source-member]")) { event.target.checked ? ui.inviteMemberSourceMarked.add(event.target.value) : ui.inviteMemberSourceMarked.delete(event.target.value); updateInviteTransferControls(); }
    if (event.target.matches("[data-invite-source-group]")) { inviteMemberCandidates().filter((member) => member.role === event.target.value && !ui.inviteMemberSelected.has(member.id)).forEach((member) => event.target.checked ? ui.inviteMemberSourceMarked.add(member.id) : ui.inviteMemberSourceMarked.delete(member.id)); renderInviteMemberTransfer(); }
    if (event.target.matches("[data-invite-target-member]")) { event.target.checked ? ui.inviteMemberTargetMarked.add(event.target.value) : ui.inviteMemberTargetMarked.delete(event.target.value); updateInviteTransferControls(); }
    if (event.target.matches("[data-input-task-source]")) { event.target.checked?ui.inputTaskSourceMarked.add(event.target.value):ui.inputTaskSourceMarked.delete(event.target.value);updateInputTransferControls("task"); }
    if (event.target.matches("[data-input-task-source-kr]")) { inputTaskCandidates($("#input-source-form")?.dataset.taskId).filter((task)=>task.krId===event.target.value&&!ui.inputTaskSelected.has(task.id)).forEach((task)=>event.target.checked?ui.inputTaskSourceMarked.add(task.id):ui.inputTaskSourceMarked.delete(task.id));renderInputTaskTransfer(); }
    if (event.target.matches("[data-input-task-source-o]")) { const krIds=data.krs.filter((kr)=>kr.objectiveId===event.target.value).map((kr)=>kr.id);inputTaskCandidates($("#input-source-form")?.dataset.taskId).filter((task)=>krIds.includes(task.krId)&&!ui.inputTaskSelected.has(task.id)).forEach((task)=>event.target.checked?ui.inputTaskSourceMarked.add(task.id):ui.inputTaskSourceMarked.delete(task.id));renderInputTaskTransfer(); }
    if (event.target.matches("[data-input-task-target]")) { event.target.checked?ui.inputTaskTargetMarked.add(event.target.value):ui.inputTaskTargetMarked.delete(event.target.value);updateInputTransferControls("task"); }
    if (event.target.matches("[data-input-member-source]")) { event.target.checked?ui.inputMemberSourceMarked.add(event.target.value):ui.inputMemberSourceMarked.delete(event.target.value);updateInputTransferControls("member"); }
    if (event.target.matches("[data-input-member-source-team]")) { inputMemberCandidates().filter((member)=>member.team===event.target.value&&!ui.inputMemberSelected.has(member.id)).forEach((member)=>event.target.checked?ui.inputMemberSourceMarked.add(member.id):ui.inputMemberSourceMarked.delete(member.id));renderInputMemberTransfer(); }
    if (event.target.matches("[data-input-member-target]")) { event.target.checked?ui.inputMemberTargetMarked.add(event.target.value):ui.inputMemberTargetMarked.delete(event.target.value);updateInputTransferControls("member"); }
  }

  function focusGraph(id) {
    if (getTask(id)?.status === "已完成") ui.graphTime = "all";
    ui.graphHistory.push({ mode: ui.graphMode, selected: ui.graphSelected });
    ui.graphMode = "focus"; ui.graphView = "graph"; ui.graphSelected = id; ui.graphZoom = 1; ui.graphPan = {x:0,y:0};
    if (route() !== "graph") location.hash = "graph"; else renderPage();
  }
  function updateGraphTransform() {
    const stage = $("#graph-stage"); if(stage) stage.setAttribute("transform",`translate(${ui.graphPan.x} ${ui.graphPan.y}) scale(${ui.graphZoom})`);
  }
  function bindGraphInteractions() {
    const canvas = $("#graph-canvas"), svg = $("#graph-svg"); if(!canvas || !svg) return;
    $$("[data-node-id]",svg).forEach((node) => node.addEventListener("click", (event) => { event.stopPropagation(); ui.graphSelected=node.dataset.nodeId;renderPage(); }));
    svg.addEventListener("wheel",(event)=>{event.preventDefault();ui.graphZoom=Math.max(.45,Math.min(2.2,ui.graphZoom+(event.deltaY<0?.08:-.08)));updateGraphTransform();},{passive:false});
    let dragging=false,start={x:0,y:0},origin={x:0,y:0};
    svg.addEventListener("pointerdown",(event)=>{if(event.target.closest(".node"))return;dragging=true;start={x:event.clientX,y:event.clientY};origin={...ui.graphPan};canvas.classList.add("dragging");svg.setPointerCapture(event.pointerId);});
    svg.addEventListener("pointermove",(event)=>{if(!dragging)return;ui.graphPan={x:origin.x+(event.clientX-start.x),y:origin.y+(event.clientY-start.y)};updateGraphTransform();});
    svg.addEventListener("pointerup",()=>{dragging=false;canvas.classList.remove("dragging")});
  }
  function setInputMode(mode, card) {
    $$(".radio-card",$("#modal-root")).forEach((x)=>x.classList.remove("active")); card.classList.add("active"); card.querySelector("input").checked=true;
    $("#input-task-panel").classList.toggle("hidden",mode!=="task"); $("#input-person-panel").classList.toggle("hidden",mode!=="person");
  }
  function saveInputSource() {
    const form=$("#input-source-form"), fd=new FormData(form), taskId=form.dataset.taskId, mode=fd.get("mode");
    if(mode==="task") {
      const sources=fd.getAll("sourceTask");if(!sources.length)return toast("请至少选择一个已有任务。","error");
      const freshSources=sources.filter((source)=>!data.relations.some((relation)=>relation.from===source&&relation.to===taskId&&relation.type==="input"));
      if(!freshSources.length)return toast("所选任务已与当前任务建立输入关系。","error");
      let nextRelation=Math.max(0,...data.relations.map((relation)=>Number(relation.id.replace(/\D/g,""))||0))+1;
      freshSources.forEach((source)=>{const sourceTask=getTask(source),deliverable=data.deliverables.find((item)=>item.taskId===source&&item.state==="已生效");data.relations.push({id:`R${String(nextRelation++).padStart(2,"0")}`,from:source,to:taskId,type:"input",necessity:"必要",label:sourceTask.outputName,pathId:null,state:deliverable?"已就绪":"等待当前交付物生效"});});
      saveData(`为 ${taskId} 绑定 ${freshSources.length} 个系统内输入来源`);closeModal();toast(`已建立 ${freshSources.length} 条任务输入关系。`,"success");
    } else {
      const providers=fd.getAll("provider");if(!providers.length)return toast("请至少选择一名提供成员。","error");
      let nextInput=Math.max(0,...data.inputRequests.map((request)=>Number(request.id.replace(/\D/g,""))||0))+1,nextChange=Math.max(0,...data.changeRequests.map((request)=>Number(request.id.replace(/\D/g,""))||0))+1;
      providers.forEach((provider)=>{const inputId=`IR${String(nextInput++).padStart(2,"0")}`;data.inputRequests.push({id:inputId,taskId,inputName:fd.get("inputName"),provider,requester:ui.identity,state:"待配置审批",due:fd.get("due"),necessity:"必要",createdAt:"刚刚",waitingDays:0,impact:`影响 ${taskId} 及其下游任务`});data.changeRequests.push({id:`CR${String(nextChange++).padStart(2,"0")}`,taskId,submitter:ui.identity,krOwner:getKr(getTask(taskId).krId).owner,state:"待审批",field:"输入来源",oldValue:"未配置",newValue:`${memberName(provider)} 提供 ${fd.get("inputName")}`,reason:"现有任务中没有合适输入源",inputRequestId:inputId});});
      saveData(`提交 ${taskId} 的 ${providers.length} 个人工输入来源审批`);closeModal();toast(`已提交 ${providers.length} 名成员的输入来源审批。`,"success");
    }
  }
  function submitNewTask() {
    const form = $("#create-task-form"); if (!form.reportValidity()) return;
    const rows = $$(".task-sheet-row", form), invite=(data.taskInvites || []).find((item)=>item.id===form.dataset.inviteId && item.invitee===ui.identity && item.state==="待处理"); if (!rows.length) return toast("请至少添加一项任务。", "error");
    const drafts = rows.map((row) => { const value = (name) => $(`[name="${name}"]`, row)?.value.trim() || ""; return { krId:value("krId"), title:value("title"), owner:resolveMemberInput(value("owner"), false), outputName:value("outputName"), start:value("start"), due:value("due") }; });
    if (drafts.some((draft) => !draft.owner)) return toast("请从匹配结果中选择有效任务负责人。", "error");
    if (drafts.some((draft) => !draft.start || !draft.due || draft.start>draft.due)) return toast("请填写有效任务周期，结束日期不能早于开始日期。", "error");
    let nextNumber = Math.max(0, ...data.tasks.map((task) => Number(task.id.replace(/\D/g, "")) || 0)) + 1;
    const created = drafts.map((draft) => {
      const id = `T${String(nextNumber++).padStart(2,"0")}`;
      const linkedInvite=invite?.krId===draft.krId?invite.id:"";
      data.tasks.push({ id, krId:draft.krId, title:draft.title, owner:draft.owner, participants:[], status:"待入池审批", progress:null, start:compactDate(draft.start), due:compactDate(draft.due), outputName:draft.outputName, receiver:getKr(draft.krId).owner, description:`创建 ${draft.title}，预期形成 ${draft.outputName || "任务成果"}。`, createdBy:ui.identity, inviteId:linkedInvite, updatedAt:"刚刚" });
      data.entryApprovals.push({ id:`EA${String(data.entryApprovals.length + 1).padStart(2,"0")}`, taskId:id, submitter:ui.identity, krOwner:getKr(draft.krId).owner, state:"待审批", reason:`建立 ${draft.title} 并纳入 ${draft.krId} 推进。`, inviteId:linkedInvite, createdAt:"刚刚", waitingDays:0 });
      return id;
    });
    if (invite && drafts.some((draft)=>draft.krId===invite.krId)) { invite.state="已完成"; invite.completedAt="刚刚"; }
    saveData(`创建 ${created.join("、")} 并提交所属 KR 负责人审批`); closeModal(); toast(`${created.length} 项任务已提交所属 KR 负责人审批，尚未进入执行池。`, "success"); if (["tasks","mywork"].includes(route())) renderPage();
  }
  function confirmCompletion() {
    const form=$("#completion-form"); if(!form?.reportValidity())return;
    const fd=new FormData(form),taskId=form.dataset.taskId,task=getTask(taskId),reviewers=fd.getAll("reviewer"),rows=$$(".candidate-draft-row",form);
    let nextNumber=Math.max(0,...data.deliverables.map((item)=>Number(item.id.replace(/\D/g,""))||0))+1;
    const candidateIds=rows.map((row)=>{const value=(name)=>$(`[name="${name}"]`,row)?.value.trim()||"",file=$("input[data-upload-control]",row)?.files?.[0],id=`D${String(nextNumber++).padStart(2,"0")}`;data.deliverables.push({id,krId:task.krId,taskId,relationId:data.relations.find((rel)=>rel.from===taskId)?.id||"待建立",name:value("name"),owner:task.owner,submittedBy:ui.identity,receiver:task.receiver,state:"审核中",formedAt:"刚刚",file:file?.name||"",fileType:file?uploadFileType(file):"文件",fileSize:file?formatUploadSize(file.size):"—",submittedAt:"刚刚",effectiveAt:"",uploadedByUser:true});return id;});
    const state=reviewers.length?"中间审核中":"待 KR 终审";
    const candidateSnapshot=candidateIds.map(getDeliverable).map((item)=>({id:item.id,name:item.name,file:item.file,fileType:item.fileType,fileSize:item.fileSize}));
    data.completionApprovals.push({id:`CA${String(data.completionApprovals.length+1).padStart(2,"0")}`,taskId,submitter:ui.identity,krOwner:getKr(task.krId).owner,state,createdAt:"刚刚",deliverableIds:candidateIds,candidateSnapshot,reviewers:reviewers.map((person)=>({person,state:"待审核"}))});
    task.status=reviewers.length?"待中间审核":"待 KR 终审";saveData(`提交 ${taskId} 的 ${candidateIds.length} 项候选交付物和完成申请`);closeModal();closeDrawer();toast(reviewers.length?"已进入或签中间审核；任一人通过即可流转。":"未配置中间审核，已直接进入 KR 终审。","success");renderPage();
  }
  function recordReview(item,approved,opinion) { item.handledBy=ui.identity;item.handledAt="刚刚";item.result=approved?"通过":"退回";item.opinion=opinion;item.reviewHistory ||= [];item.reviewHistory.push({actor:ui.identity,time:"刚刚",result:item.result,opinion}); }
  function deleteCompletionCandidates(item) { const ids=new Set(completionCandidateIds(item));data.deliverables=data.deliverables.filter((candidate)=>!ids.has(candidate.id)); }
  function decideEntry(id,approved,opinion="") {
    const item=data.entryApprovals.find((x)=>x.id===id);if(!item)return;item.state=approved?"已通过":"已退回";recordReview(item,approved,opinion);const task=getTask(item.taskId);task.status=approved?"未开始":"入池退回";saveData(`${approved?"通过":"退回"} ${task.id} 入池申请`);toast(approved?`${task.id} 已进入执行池，状态为“未开始”。`:`${task.id} 已退回创建人，状态为“入池退回”。`,approved?"success":"error");renderPage();
  }
  function decideChange(id,approved,opinion="") {
    const item=data.changeRequests.find((x)=>x.id===id);if(!item)return;item.state=approved?"已通过":"已退回";if(!approved)item.dismissed=false;recordReview(item,approved,opinion);if(approved&&item.proposed)Object.assign(getTask(item.taskId),item.proposed,{updatedAt:"刚刚"});else if(approved&&item.field==="截止时间")getTask(item.taskId).due=item.newValue;if(item.inputRequestId){const request=data.inputRequests.find((x)=>x.id===item.inputRequestId);if(request)request.state=approved?"待接收":"审批退回";}saveData(`${approved?"批准":"退回"} ${item.taskId} 关键字段变更`);const provider=data.inputRequests.find((x)=>x.id===item.inputRequestId)?.provider;toast(approved?(item.inputRequestId?`输入来源已生效，并已向 ${memberName(provider)} 发送站内通知。`:"新值已生效，旧值已保留在审计记录。"):(item.inputRequestId?"输入来源配置已退回，未通知对接人。":"拟议值已作废，旧值继续生效。"),approved?"success":"error");renderPage();
  }
  function decideMiddle(id,approved,opinion="") {
    const item=data.completionApprovals.find((x)=>x.id===id);if(!item)return;
    item.reviewers.forEach((r)=>{if(r.person===ui.identity)r.state=approved?"已通过":"已退回";else if(r.state==="待审核")r.state="已自动关闭";});
    recordReview(item,approved,opinion);item.state=approved?"待 KR 终审":"已退回";item.waitingDays=0;getTask(item.taskId).status=approved?"待 KR 终审":"进行中";if(!approved)deleteCompletionCandidates(item);saveData(`${approved?"通过":"退回"} ${item.taskId} 中间审核`);toast(approved?"或签已通过，其他审核人待办已关闭，现进入 KR 终审。":"完成申请已整体退回，候选文件已删除，原当前内容保持不变。",approved?"success":"error");renderPage();
  }
  function decideFinal(id,approved,opinion="") {
    const item=data.completionApprovals.find((x)=>x.id===id);if(!item)return;recordReview(item,approved,opinion);item.state=approved?"已闭环":"已退回";const task=getTask(item.taskId),candidates=completionCandidates(item);task.status=approved?"已完成":"进行中";task.progress=approved?100:task.progress;
    if(approved){const candidateIds=new Set(candidates.map((candidate)=>candidate.id)),replacements=[];candidates.forEach((candidate)=>{data.deliverables.filter((current)=>current.id!==candidate.id&&current.taskId===candidate.taskId&&current.name===candidate.name&&current.state==="已生效").forEach((current)=>replacements.push([current.id,candidate.id]));candidate.state="已生效";candidate.effectiveAt="刚刚";});const oldIds=new Set(replacements.map(([oldId])=>oldId));data.deliverables=data.deliverables.filter((deliverable)=>candidateIds.has(deliverable.id)||!oldIds.has(deliverable.id));data.packages.forEach((pkg)=>{pkg.deliverableIds=pkg.deliverableIds.map((deliverableId)=>replacements.find(([oldId])=>oldId===deliverableId)?.[1]||deliverableId);});data.relations.filter((relation)=>relation.from===task.id).forEach((relation)=>{relation.state="已就绪";});}
    else deleteCompletionCandidates(item);
    saveData(`${approved?"通过并闭环":"退回"} ${task.id} 完成申请`);toast(approved?`${task.id} 已完成，${candidates.length} 项候选内容已覆盖对应当前交付物，旧文件已永久删除。`:`${task.id} 已退回，候选文件已删除，原当前内容保持不变。`,approved?"success":"error");renderPage();
  }
  function refreshMyWorkDrawer(taskId,tab="overview") { if(route()==="mywork" && ui.taskDrawerSource==="mywork"){ $("#page").innerHTML=renderMyWork();hydrateIcons($("#page"));openTaskDrawer(taskId,tab,ui.taskDrawerContext); } else renderPage(); }
  function acceptInput(id){const r=data.inputRequests.find((x)=>x.id===id);if(!r)return;r.state="已接收";saveData(`${ui.identity} 同意接收输入请求 ${id}`);toast("已同意接收。现在可以填写内容或上传文件。","success");refreshMyWorkDrawer(r.taskId);}
  function confirmReceipt(id){const item=getDeliverable(id);if(!item||item.receiver!==ui.identity)return;item.receiptState="已确认";item.receivedAt="刚刚";saveData(`${ui.identity} 确认接收当前交付物 ${id}`);toast(`${item.name} 已确认接收。`,"success");refreshMyWorkDrawer(item.taskId);}
  function provideInputModal(id){const r=data.inputRequests.find((x)=>x.id===id);if(!r)return;openModal("提供任务输入",`${r.taskId} · ${r.inputName}`,`<form id="provide-input-form" data-id="${id}" class="form-grid"><div class="form-row full"><label>提供内容</label><textarea name="content" class="field" required>已完成现场核对，关键点位与时间窗口见附件。</textarea></div><div class="form-row full"><label>上传文件</label>${uploadControl("task-input-file",{confirmId:"confirm-provide-input-button"})}</div><div class="form-row full"><div class="notice">文件仅在点击“提交输入”后生效；关闭窗口不会保存本次选择。</div></div></form>`,`<button class="btn" data-action="close-modal">取消</button><button id="confirm-provide-input-button" class="btn primary" data-action="confirm-provide-input" disabled>提交输入</button>`);$("#modal-root .modal")?.classList.add("mywork-action-modal");}
  function confirmProvideInput(){const form=$("#provide-input-form"),r=data.inputRequests.find((x)=>x.id===form?.dataset.id),file=$("#task-input-file")?.files?.[0];if(!form?.reportValidity()||!r||!file)return toast("请先选择需要上传的文件。","error");const content=String(new FormData(form).get("content")||"").trim();r.state="已提供";r.providedContent=content;r.providedFile={name:file.name,size:file.size,type:file.type||"file"};const task=getTask(r.taskId);if(task?.status==="等待输入")task.status="进行中";saveData(`${ui.identity} 提交输入请求 ${r.id}`);closeModal();toast(`${file.name} 已提交；${r.taskId} 的“${r.inputName}”已提供。`,"success");refreshMyWorkDrawer(r.taskId);}
  function submitDiscussion(){const form=$("#task-discussion-form");if(!form?.reportValidity())return;const fd=new FormData(form),taskId=form.dataset.taskId,content=String(fd.get("content")||"").trim();if(!content)return;const mentions=data.members.filter((member)=>content.includes(`@${member.displayName}`)||content.includes(`@ ${member.displayName}`)).map((member)=>member.id);data.discussions ||= [];data.discussions.push({id:`DC${String(data.discussions.length+1).padStart(2,"0")}`,taskId,author:ui.identity,content,mentions,createdAt:"刚刚"});saveData(`在 ${taskId} 提交讨论意见`);openTaskDrawer(taskId,"discussion");const recipients=new Set([getTask(taskId)?.owner,...mentions].filter((id)=>id&&id!==ui.identity));toast(recipients.size?`意见已提交，并通知 ${[...recipients].map(memberName).join("、")}。`:"意见已提交。","success");}
  function confirmReviewDecision(){const form=$("#review-decision-form");if(!form?.reportValidity())return;const approved=form.dataset.approved==="true",kind=form.dataset.kind,id=form.dataset.id,opinion=new FormData(form).get("opinion")?.trim()||"";let taskId="";if(kind==="entry"){taskId=data.entryApprovals.find((item)=>item.id===id)?.taskId;decideEntry(id,approved,opinion);}else if(kind==="change"){taskId=data.changeRequests.find((item)=>item.id===id)?.taskId;decideChange(id,approved,opinion);}else if(kind==="middle"){taskId=data.completionApprovals.find((item)=>item.id===id)?.taskId;decideMiddle(id,approved,opinion);}else if(kind==="final"){taskId=data.completionApprovals.find((item)=>item.id===id)?.taskId;decideFinal(id,approved,opinion);}if(taskId&&["overview","tasks","mywork","graph","artifacts"].includes(ui.taskDrawerSource))openTaskDrawer(taskId,"audit");}
  function submitRisk(){const form=$("#risk-form"),fd=new FormData(form),id=`B${String(data.risks.length+1).padStart(2,"0")}`;data.risks.push({id,taskId:fd.get("taskId"),level:fd.get("level"),reason:`缺少“${fd.get("missing")}”：${fd.get("reason")}`,actionOwner:fd.get("actionOwner"),days:0,impact:`预计 ${fd.get("restore")} 恢复；影响当前任务及其下游`});saveData(`主动上报卡点 ${id}`);closeModal();toast(`卡点已上报，并已提醒 ${memberName(fd.get("actionOwner"))}。系统未自动改期。`,"success");renderPage();}
  function riskActionsModal(id){const risk=data.risks.find((x)=>x.id===id);openModal("卡点协调",`${risk.taskId} · ${riskReasonText(risk)}`,`<div class="radio-cards"><div class="radio-card active"><b>指定协调人</b><span>由总推进人协调上下游</span></div><div class="radio-card"><b>升级待决策</b><span>进入项目总负责人待决策</span></div></div><div class="notice warning" style="margin-top:12px">如需改期，请另行提交关键字段变更审批；本操作不自动修改日期。</div>`,`<button class="btn" data-action="close-modal">取消</button><button class="btn primary" data-action="confirm-risk-action" data-id="${id}">确认协调</button>`);}
  function confirmPackage(){const form=$("#package-form"),fd=new FormData(form),id=`PK${String(data.packages.length+1).padStart(2,"0")}`;data.packages.push({id,name:fd.get("name"),scope:fd.get("scope"),version:fd.get("version"),formedAt:"刚刚",owner:ui.identity,deliverableIds:[...ui.artifactSelected]});saveData(`生成阶段成果包 ${id}`);ui.artifactSelected.clear();closeModal();toast(`${id} 已生成固定目录和来源清单。`,"success");renderPage();}
  function packageDetail(id){const pkg=data.packages.find((x)=>x.id===id);if(!pkg)return;openDrawer(pkg.name,`${pkg.version} · ${pkg.formedAt}`,`<div class="notice">${esc(pkg.scope)} · 保留成果目录和来源事实；下载时使用各项交付物的当前内容。</div><div class="drawer-section" style="margin-top:16px"><h3>成果目录与来源清单</h3>${pkg.deliverableIds.map((dId,i)=>{const d=getDeliverable(dId);return d?`<div class="side-item"><strong>${i+1}. ${esc(d.name)}</strong><p>${d.krId} / ${d.taskId} / 关系边 ${d.relationId} / 当前内容</p></div>`:`<div class="side-item"><strong>${i+1}. 对应交付物待重新关联</strong></div>`}).join("")}</div>`,`<button class="btn primary" data-action="download-package" data-id="${id}">${icon("download")}模拟下载整包</button>`);}
  function downloadPackage(id){const pkg=data.packages.find((x)=>x.id===id);if(!pkg)return;downloadText(`${pkg.name}_${pkg.version}.txt`,`${pkg.name}\n${pkg.version}\n${pkg.scope}\n\n当前内容来源清单：\n${pkg.deliverableIds.map((dId,i)=>{const d=getDeliverable(dId);return `${i+1}. ${d?.name||dId} / ${d?.taskId||""} / ${d?.file||"待关联"}`}).join("\n")}`);toast("已使用各项交付物的当前内容生成脱敏成果包。","success");}
  function exportReport(type){if(type==="png")return exportReportPng();downloadPdf();toast("可打开的 PDF 演示文件已生成并下载。","success");}
  function downloadPdf(){const lines=["Collaboration Project Report V4.4",`Period: ${ui.reportPeriod}`,`Objectives: ${data.objectives.length} / KRs: ${data.krs.length} / Tasks: ${data.tasks.length}`,`Active deliverables: ${data.deliverables.filter((x)=>x.state==="已生效").length}`,"Anonymized interactive prototype export"];const stream=`BT /F1 18 Tf 72 760 Td (${lines[0]}) Tj 0 -34 Td /F1 11 Tf (${lines.slice(1).join(") Tj 0 -22 Td (")}) Tj ET`;const objects=["1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n","2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n","3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >> endobj\n","4 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\n",`5 0 obj << /Length ${stream.length} >> stream\n${stream}\nendstream endobj\n`];let pdf="%PDF-1.4\n",offsets=[0];const bytes=(text)=>new TextEncoder().encode(text).length;objects.forEach((obj)=>{offsets.push(bytes(pdf));pdf+=obj});const xref=bytes(pdf);pdf+=`xref\n0 ${objects.length+1}\n0000000000 65535 f \n${offsets.slice(1).map((n)=>`${String(n).padStart(10,"0")} 00000 n `).join("\n")}\ntrailer << /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF`;downloadText("project-report-v4.4.pdf",pdf,"application/pdf")}
  function exportReportPng(){const canvas=document.createElement("canvas"),ctx=canvas.getContext("2d");canvas.width=750;canvas.height=1500;ctx.fillStyle="#fff";ctx.fillRect(0,0,canvas.width,canvas.height);ctx.fillStyle="#5267df";ctx.fillRect(0,0,canvas.width,10);ctx.fillStyle="#1f2937";ctx.font='600 30px system-ui,"PingFang SC"';ctx.fillText("未来生活科技体验周筹备项目",45,75);ctx.font='400 18px system-ui,"PingFang SC"';ctx.fillStyle="#6b778c";ctx.fillText("移动端长图 · 脱敏演示报告",45,110);const lines=["整体判断","项目处于联合联调阶段，三项目标持续推进。","","进展摘要",`• ${data.krs.length} 个 KR、${data.tasks.length} 项任务在同一事实源维护`,`• ${data.deliverables.filter(x=>x.state==="已生效").length} 项当前交付物已生效`,"","卡点与待决策",...data.risks.slice(0,4).map(x=>`• ${x.taskId} ${riskReasonText(x)}`),"","下一步","• 完成负责人审批与 KR 最终闭环","• 协调互锁关系，不自动修改承诺日期","• 将新生效的当前成果纳入下一个阶段成果包"];let y=170;lines.forEach((line)=>{ctx.fillStyle=/^(整体|进展|卡点|下一步)/.test(line)?"#25365d":"#344152";ctx.font=/^(整体|进展|卡点|下一步)/.test(line)?'600 22px system-ui,"PingFang SC"':'400 17px system-ui,"PingFang SC"';wrapCanvasText(ctx,line,45,y,660,30);y+=line?(/^(整体|进展|卡点|下一步)/.test(line)?48:55):22});const a=document.createElement("a");a.download="project-report-mobile.png";a.href=canvas.toDataURL("image/png");a.click();toast("已导出适合手机阅读的长图。","success");}
  function wrapCanvasText(ctx,text,x,y,maxWidth,lineHeight){let line="";for(const char of text){const test=line+char;if(ctx.measureText(test).width>maxWidth){ctx.fillText(line,x,y);line=char;y+=lineHeight}else line=test}ctx.fillText(line,x,y)}
  function downloadText(filename,content,type="text/plain;charset=utf-8"){const blob=new Blob([content],{type}),url=URL.createObjectURL(blob),a=document.createElement("a");a.href=url;a.download=filename;document.body.appendChild(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),2000)}
  function genericFeedback(action){const messages={"invite-owner":"已向负责人发送完善任务信息的站内邀请。","upload-attachment":"已打开上传演示：过程文件将与正式成果严格区分。","add-okr":"已新增一行可连续编辑的 KR 草稿。","project-menu":"当前原型聚焦一个脱敏项目。","open-search":"可在各业务页面使用页面内搜索。","invite-member":"成员邀请已进入待确认状态。","more-feedback":"操作已记录为原型演示反馈。"};toast(messages[action]||"操作已响应。","success")}
  function resetData(){if(!window.confirm("确定重置所有演示交互状态吗？此前审批、输入请求和成果包操作将恢复。"))return;localStorage.removeItem(STORAGE_KEY);data=clone(seed);ui.artifactSelected.clear();saveData();renderPage();toast("演示数据已重置。","success")}
  function toast(message,type=""){const el=document.createElement("div");el.className=`toast ${type}`;el.textContent=message;$("#toast-root").appendChild(el);setTimeout(()=>el.remove(),3600)}

  document.addEventListener("click",(event)=>{
    const el=event.target.closest("[data-action]");if(!el)return;
    if(el.dataset.action==="confirm-provide-input")confirmProvideInput();
    if(el.dataset.action==="confirm-risk-action"){closeModal();toast("已指定总推进人协调，并保留原承诺日期。","success")}
  });

  init();
})();

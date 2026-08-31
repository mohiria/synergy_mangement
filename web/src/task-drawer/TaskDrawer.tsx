import { useEffect, useRef, useState } from "react";
import { Button, Drawer, Input, Mentions, Modal, Select, Slider, Tabs, message } from "antd";
import { client } from "../api/client";
import FileUploadField, { fileTypeLabel, formatFileSize } from "../FileUploadField";
import PeopleSelect from "./PeopleSelect";
import Icon from "../icons";
import type { components } from "../api/schema";
import { ACTIVITY_PREVIEW, STATUS_CLASS, fmtTime, pendingStructureChange } from "./shared";

type Task = components["schemas"]["Task"];
type ProjectMember = components["schemas"]["ProjectMember"];
type TaskDetail = components["schemas"]["TaskDetail"];
type TaskRelation = components["schemas"]["TaskRelation"];
type TaskFileKind = components["schemas"]["TaskFileKind"];

// 任务详情抽屉（AC-31/AC-34/AC-50/AC-51）：任务概况、审核、讨论三 Tab，默认进入任务概况。
// 页头承载任务编号、名称、所属 O／KR 与更新时间，正文不重复；空字段与空区块一律不显示（PRD §7.5）。
export default function TaskDrawer({
  projectId,
  task,
  code,
  taskCode,
  okrCode,
  members,
  activeTab,
  onTabChange,
  canGoBack,
  source,
  onClose,
  actions,
}: {
  projectId: number;
  task: Task | null;
  code?: string;
  taskCode: Map<number, string>;
  /** KR id → 「O 编号 / KR 编号」；页头只显编号，不展开标题（#99、AC-50）。 */
  okrCode: Map<number, string>;
  members: ProjectMember[];
  /** 当前 Tab 由页面持有：逐级返回时要回到点开下一级之前的那个 Tab（#101）。 */
  activeTab: string;
  onTabChange: (key: string) => void;
  /** 栈里还有上一级任务详情时，关闭按钮读作「返回」。 */
  canGoBack: boolean;
  source?: string;
  onClose: () => void;
  actions: {
    start: (t: Task) => void;
    submitPool: (t: Task) => void;
    approvePool: (t: Task) => void;
    openReject: (t: Task) => void;
    openCancel: (t: Task) => void;
    saveProgress: (t: Task, progress: number | null) => Promise<void>;
    openEdit: (t: Task) => void;
    approveFieldChange: (t: Task, changeId: number) => void;
    openFcReject: (t: Task, changeId: number) => void;
    abandonFieldChange: (t: Task, changeId: number) => void;
    openSubmitCompletion: (t: Task) => void;
    approveCompletion: (t: Task, reviewId: number, intermediate?: boolean) => void;
    openCrReject: (t: Task, reviewId: number) => void;
    openConfigureInput: (t: Task) => void;
    saveReviewers: (t: Task, userIds: number[]) => void;
    saveReceivers: (t: Task, scope: "none" | "members" | "all", userIds: number[]) => void;
    saveParticipants: (t: Task, userIds: number[]) => void;
    confirmReceipt: (t: Task) => void;
    startResultUpdate: (t: Task) => void;
    acceptInput: (requestId: number) => void;
    openProvide: (requestId: number) => void;
    openInputFile: (requestId: number) => void;
    remindBlocker: (blockerKey: string) => void;
    removeEdge: (edgeId: number) => void;
    openTask: (taskId: number) => void;
    openInGraph: (taskId: number) => void;
  };
}) {
  const [detail, setDetail] = useState<TaskDetail | null>(null);
  const drawerRef = useRef<HTMLDivElement>(null);
  const [progressDraft, setProgressDraft] = useState<number | null>(null);
  // #135：接收方就地多选——「所有项目成员」用固定首项（哨兵 -1）表达，
  // 选中即清空并禁用逐人选择；下拉收起时一次保存。
  const ALL_RECEIVERS = -1;
  const [receiverDraft, setReceiverDraft] = useState<number[]>([]);
  useEffect(() => {
    setReceiverDraft(
      task?.receiverScope === "all"
        ? [ALL_RECEIVERS]
        : (task?.receivers ?? []).map((r) => r.userId),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task?.id, task?.receiverScope, (task?.receivers ?? []).map((r) => r.userId).join(",")]);
  const commitReceivers = () => {
    if (!task) return;
    const current =
      task.receiverScope === "all"
        ? [ALL_RECEIVERS]
        : (task.receivers ?? []).map((r) => r.userId);
    if ([...receiverDraft].sort().join(",") === [...current].sort().join(",")) return;
    if (receiverDraft.includes(ALL_RECEIVERS)) actions.saveReceivers(task, "all", []);
    else if (receiverDraft.length === 0) actions.saveReceivers(task, "none", []);
    else actions.saveReceivers(task, "members", receiverDraft);
  };
  const memberOption = (m: ProjectMember) => (
    <span className="owner-cell">
      <span className="avatar">{m.displayName.slice(0, 1)}</span>
      <span className="cell-text">
        {m.displayName}（{m.username}）
      </span>
    </span>
  );
  const [addingDeliverable, setAddingDeliverable] = useState(false);
  const [newDeliverableFile, setNewDeliverableFile] = useState<File | null>(null);
  const [newDeliverableBusy, setNewDeliverableBusy] = useState(false);
  const [discussionDraft, setDiscussionDraft] = useState("");
  const [postingDiscussion, setPostingDiscussion] = useState(false);
  const [activityExpanded, setActivityExpanded] = useState(false);

  // 按来源分组落位（模块 PRD §6.2）：详情就绪后滚动到对应区块并短暂高亮，
  // 卡片正文因此不再展开原因、输入与影响（MW-01/03/05/06/14 新口径）。
  useEffect(() => {
    if (!detail || !source) return;
    const targets: Record<string, string[]> = {
      pending: ['[data-focus="inputs"]', ".audit-card.pending", '[data-focus="basic"]'],
      approvals: [".audit-card.pending", '[data-focus="basic"]'],
      receipts: ['[data-focus="receipts"]', '[data-focus="deliverables"]', '[data-focus="basic"]'],
      waiting: ['[data-focus="inputs"]', '[data-focus="basic"]'],
      // 协作关系已独立成 Tab，一次落位只能命中一个 Tab，故卡点组只定位卡点区块（MW §6.2）。
      blockers: ['[data-focus="blockers"]'],
    };
    const root = drawerRef.current;
    if (!root) return;
    const el = (targets[source] ?? [])
      .map((sel) => root.querySelector<HTMLElement>(sel))
      .find((node): node is HTMLElement => node != null);
    if (!el) return;
    el.scrollIntoView({ block: "nearest" });
    el.classList.add("focus-flash");
    const timer = window.setTimeout(() => el.classList.remove("focus-flash"), 2000);
    return () => window.clearTimeout(timer);
  }, [detail, source]);

  const postDiscussion = async () => {
    if (!task) return;
    // 被 @ 成员按文本中的 @姓名 匹配项目成员（原型「姓名匹配」交互）。
    const mentionUserIds = members
      .filter((m) => discussionDraft.includes(`@${m.displayName}`))
      .map((m) => m.userId);
    setPostingDiscussion(true);
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/discussions", {
      params: { path: { projectId, taskId: task.id } },
      body: { content: discussionDraft.trim(), mentionUserIds },
    });
    setPostingDiscussion(false);
    if (res.data) {
      message.success("意见已提交");
      setDiscussionDraft("");
      setRefreshTick((n) => n + 1);
    } else {
      message.error(res.error?.message ?? "提交失败");
    }
  };
  const [uploadingId, setUploadingId] = useState<number | null>(null);
  const [candidateFor, setCandidateFor] = useState<{ id: number; name: string } | null>(null);
  const [candidateFile, setCandidateFile] = useState<File | null>(null);
  // 过程文件与重要外部材料（§7.7）：与候选内容同一套两阶段提交，但不进审批、不影响就绪。
  const [taskFileKind, setTaskFileKind] = useState<TaskFileKind | null>(null);
  const [taskFileNote, setTaskFileNote] = useState("");
  const [taskFileValue, setTaskFileValue] = useState<File | null>(null);
  const [taskFileBusy, setTaskFileBusy] = useState(false);
  const [refreshTick, setRefreshTick] = useState(0);

  // 换一个任务时动态回到默认的最近 5 条（刷新同一任务不收起）。
  useEffect(() => setActivityExpanded(false), [task?.id]);

  useEffect(() => {
    if (!task) {
      setDetail(null);
      return;
    }
    let alive = true;
    client
      .GET("/projects/{projectId}/tasks/{taskId}", {
        params: { path: { projectId, taskId: task.id } },
      })
      .then((res) => {
        if (alive && res.data) setDetail(res.data);
      });
    return () => {
      alive = false;
    };
  }, [projectId, task, refreshTick]);

  const openFile = async (fileId: number) => {
    const res = await client.GET("/projects/{projectId}/files/{fileId}/download-url", {
      params: { path: { projectId, fileId } },
    });
    if (res.data) {
      // #117：预签名地址一律带 attachment，同页跳转即触发下载；
      // 不用 window.open——await 之后再开新窗会被部分浏览器当弹窗拦截。
      window.location.assign(res.data.url);
    } else {
      message.error(res.error?.message ?? "获取下载地址失败");
    }
  };

  // 上传过程文件／重要外部材料：登记 → 直传 → 确认，三步都成才算数（与候选内容同口径）。
  const closeTaskFile = () => {
    setTaskFileKind(null);
    setTaskFileNote("");
    setTaskFileValue(null);
  };

  const uploadTaskFile = async () => {
    if (!task || !taskFileKind || !taskFileValue) return;
    const file = taskFileValue;
    setTaskFileBusy(true);
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/files", {
      params: { path: { projectId, taskId: task.id } },
      body: {
        kind: taskFileKind,
        fileName: file.name,
        fileType: file.name.split(".").pop() ?? "",
        fileSize: file.size,
        note: taskFileNote.trim(),
      },
    });
    if (!res.data) {
      setTaskFileBusy(false);
      message.error(res.error?.message ?? "登记失败");
      return;
    }
    try {
      const put = await fetch(res.data.uploadUrl, { method: "PUT", body: file });
      if (!put.ok) throw new Error(`HTTP ${put.status}`);
      const commit = await client.POST("/projects/{projectId}/tasks/{taskId}/files/{fileId}/commit", {
        params: { path: { projectId, taskId: task.id, fileId: res.data.file.id } },
      });
      if (!commit.data) throw new Error(commit.error?.message ?? "确认失败");
      message.success(`${commit.data.kindLabel}已上传；它不进入完成审批，也不作为下游正式输入`);
      closeTaskFile();
    } catch {
      message.error("文件上传失败，请确认文件服务可用后重试");
    }
    setTaskFileBusy(false);
    setRefreshTick((n) => n + 1);
  };

  const removeTaskFile = async (fileId: number) => {
    if (!task) return;
    const res = await client.DELETE("/projects/{projectId}/tasks/{taskId}/files/{fileId}", {
      params: { path: { projectId, taskId: task.id, fileId } },
    });
    if (res.response.status === 204) {
      message.success("已删除");
      setRefreshTick((n) => n + 1);
    } else {
      message.error(res.error?.message ?? "删除失败");
    }
  };

  const openTaskFile = async (fileId: number) => {
    const res = await client.GET("/projects/{projectId}/task-files/{fileId}/download-url", {
      params: { path: { projectId, fileId } },
    });
    if (res.data) window.location.assign(res.data.url);
    else message.error(res.error?.message ?? "获取下载地址失败");
  };

  const closeAddDeliverable = () => {
    setAddingDeliverable(false);
    setNewDeliverableFile(null);
  };

  // 新增交付物项（裁决 G1）：入口就是选文件，一步建项并上传候选内容——项名由服务端按文件名派生，
  // 前端不自己算名字。已入池任务的新增是关键字段变更，进审批时项还没建出来，只能等审批通过再传内容。
  const addDeliverable = async () => {
    if (!task || !newDeliverableFile) return;
    const file = newDeliverableFile;
    setNewDeliverableBusy(true);
    const before = new Set((detail?.deliverables ?? []).map((d) => d.id));
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/deliverables", {
      params: { path: { projectId, taskId: task.id } },
      body: { fileName: file.name },
    });
    if (!res.data) {
      setNewDeliverableBusy(false);
      message.error(res.error?.message ?? "新增失败");
      return;
    }
    if (pendingStructureChange(res.data)) {
      setNewDeliverableBusy(false);
      closeAddDeliverable();
      message.success("已提交，待所属 KR 负责人审批后生效；通过后再上传内容");
      setRefreshTick((n) => n + 1);
      return;
    }
    // 项已建出来：取回详情认出新项（按 id 差集，不猜派生出来的项名），接着走候选内容两阶段上传。
    const after = await client.GET("/projects/{projectId}/tasks/{taskId}", {
      params: { path: { projectId, taskId: task.id } },
    });
    const created = (after.data?.deliverables ?? []).find((d) => !before.has(d.id));
    if (!created) {
      setNewDeliverableBusy(false);
      closeAddDeliverable();
      message.success("交付物项已新增");
      setRefreshTick((n) => n + 1);
      return;
    }
    const ok = await putCandidate(created.id, file);
    setNewDeliverableBusy(false);
    if (ok) {
      closeAddDeliverable();
      message.success(`交付物项「${created.name}」已新增，候选内容已上传`);
    }
    setRefreshTick((n) => n + 1);
  };

  // 候选内容上传（AC-52）：先在窗口内选择，点「确认上传」才登记并直传；
  // 关闭窗口丢弃本次选择，不产生任何业务事实。
  const closeCandidate = () => {
    setCandidateFor(null);
    setCandidateFile(null);
  };

  // 候选内容两阶段上传：登记 → 直传 → 确认，三步都成才算数。新增交付物项也复用这一段。
  const putCandidate = async (deliverableId: number, file: File): Promise<boolean> => {
    if (!task) return false;
    const res = await client.POST(
      "/projects/{projectId}/tasks/{taskId}/deliverables/{deliverableId}/candidate",
      {
        params: { path: { projectId, taskId: task.id, deliverableId } },
        body: {
          fileName: file.name,
          fileType: file.name.split(".").pop() ?? "",
          fileSize: file.size,
        },
      },
    );
    if (!res.data) {
      message.error(res.error?.message ?? "登记候选内容失败");
      return false;
    }
    try {
      const put = await fetch(res.data.uploadUrl, { method: "PUT", body: file });
      if (!put.ok) throw new Error(`HTTP ${put.status}`);
      // 两阶段提交第二步：服务端校验对象确已写入后，内容才成为候选。
      const commit = await client.POST(
        "/projects/{projectId}/tasks/{taskId}/deliverables/{deliverableId}/candidate/commit",
        {
          params: { path: { projectId, taskId: task.id, deliverableId } },
          body: { fileId: res.data.file.id },
        },
      );
      if (!commit.data) throw new Error(commit.error?.message ?? "确认失败");
      return true;
    } catch {
      message.error("文件上传失败，请确认文件服务可用后重试");
      return false;
    }
  };

  const uploadCandidate = async () => {
    if (!task || !candidateFor || !candidateFile) return;
    setUploadingId(candidateFor.id);
    if (await putCandidate(candidateFor.id, candidateFile)) {
      message.success("候选内容已上传；随完成申请提交后进入审核");
      closeCandidate();
    }
    setUploadingId(null);
    setRefreshTick((n) => n + 1);
  };

  if (!task) return null;
  const reviewers = detail?.reviewers ?? [];
  const receipts = detail?.receipts ?? [];
  // 接收方展示口径（模块 PRD §8.6）：不配置、指定成员名单，或「所有项目成员」。
  const receiverLabel =
    task.receiverScope === "all"
      ? "所有项目成员"
      : task.receiverScope === "members"
        ? (task.receivers ?? []).map((r) => r.displayName).join("、") || "未配置"
        : "未配置";
  const blockers = detail?.blockers ?? [];
  const inputs = detail?.inputs ?? [];
  const activities = detail?.activities ?? [];
  const discussions = detail?.discussions ?? [];
  const upstream = detail?.upstream ?? [];
  const downstream = detail?.downstream ?? [];
  // 受影响 O／KR 是系统沿下游硬前置边推导出来的，不是直接关系，单独计数与展示（CR PRD §8.1）。
  const impactedTargets = detail?.impactedTargets ?? [];
  const relationCount = upstream.length + downstream.length + impactedTargets.length;
  const requiredInputs = inputs.filter((e) => e.necessity === "required");
  const referenceInputs = inputs.filter((e) => e.necessity === "reference");
  // 上游未就绪卡点按边寻址（Blocker.key 形如 upstream_unready:edge:17），供任务输入行取缺失原因。
  const inputBlockers = new Map(
    blockers
      .filter((b) => b.key.startsWith("upstream_unready:edge:"))
      .map((b) => [Number(b.key.slice("upstream_unready:edge:".length)), b] as const),
  );
  const deliverables = detail?.deliverables ?? [];
  const currentFiles = deliverables.filter((d) => d.current);
  // AC-67：「审核中」以内容状态为准——候选传了没提交时后端给 pending_submit，
  // 顶部不得声称在审（此前按 d.candidate 计数，把待提交也算成了审核中）。
  const reviewingCount = deliverables.filter(
    (d) => d.contentState === "reviewing" || d.contentState === "updating",
  ).length;
  const pendingSubmitCount = deliverables.filter((d) => d.contentState === "pending_submit").length;
  const taskFiles = detail?.files ?? [];
  // 未决审批计数由后端派生（F1），前端不再把三类审批单各自过滤后相加。
  const pendingReviews = detail?.task.pendingReviewCount ?? 0;

  // 摘要一组关系（AC-41）：对方任务编号与名称、所属 KR、关系类型、负责人、状态与就绪；
  // 组为空时不渲染该分组（词汇表「协作关系摘要」）。
  const relationGroup = (title: string, rels: TaskRelation[]) =>
    rels.length === 0 ? null : (
      <div style={{ marginBottom: 8 }}>
        <b style={{ fontSize: 12, color: "var(--muted)" }}>
          {title} · {rels.length}
        </b>
        {rels.map((rel) => (
          <button
            key={`${title}-${rel.taskId}-${rel.edgeType}`}
            type="button"
            className="fact-card fact-card-link"
            onClick={() => actions.openTask(rel.taskId)}
          >
            <span>
              <b title={`${taskCode.get(rel.taskId) ?? ""} · ${rel.taskName}`}>
                {taskCode.get(rel.taskId) ?? ""} · {rel.taskName}
              </b>
              <small
                title={`${rel.krDescription} · ${rel.edgeTypeLabel ?? ""} · 负责人 ${rel.ownerName} · ${rel.taskStatusLabel}`}
              >
                {rel.krDescription} · {rel.edgeTypeLabel ?? ""} · 负责人 {rel.ownerName} ·{" "}
                {rel.taskStatusLabel}
              </small>
            </span>
            <span className={`status-pill ${rel.ready ? "completed" : "warning"}`}>
              {rel.ready ? "已就绪" : "未就绪"}
            </span>
          </button>
        ))}
      </div>
    );

  // 参与人只作展示与检索，不参与任何判定，直接读契约派生字段（词汇表「参与人」）。
  const participants = task.participants ?? [];

  const overview = (
    <>
      {/* AC-50 基础信息：按 PRD §7.5 顺序紧凑排列，不重复页头已有的编号与 O／KR；
          空字段不显示。当前环节／待行动人不再单列——statusLabel 已按 AC-04 表达审批等待，属重复提示。 */}
      <section className="drawer-section" data-focus="basic" style={{ marginTop: 4 }}>
        <h3>基础信息</h3>
        <div className="task-info-list">
          <div className="task-info-row">
            <span>负责人</span>
            <strong>{task.ownerName}</strong>
          </div>
          {/* 参与人（PRD §9.2 按需字段）：空名单按 AC-50「空字段不显示」隐藏，
              但本人可配置时保留该行，否则首次添加没有入口。 */}
          {/* #135：参与人／接收方／成果审核人并列基础信息，就地多选保存（无弹窗）。 */}
          {(participants.length > 0 || task.canManageParticipants) && (
            <div className="task-info-row">
              <span>参与人</span>
              {task.canManageParticipants ? (
                <PeopleSelect
                  value={participants.map((p) => p.userId)}
                  options={members.filter((m) => m.userId !== task.ownerId)}
                  placeholder="未设置"
                  onSave={(ids) => actions.saveParticipants(task, ids)}
                />
              ) : (
                <strong className={participants.length ? "" : "muted"}>
                  {participants.map((p) => p.displayName).join("、") || "未设置"}
                </strong>
              )}
            </div>
          )}
          {(task.receiverScope !== "none" || task.canManageReceivers) && (
            <div className="task-info-row">
              <span>接收方</span>
              {task.canManageReceivers ? (
                <Select
                  mode="multiple"
                  size="small"
                  style={{ minWidth: 220, maxWidth: 420, flex: 1 }}
                  placeholder="未配置：选择成员或「所有项目成员」"
                  value={receiverDraft}
                  onChange={(ids: number[]) => {
                    if (ids.includes(ALL_RECEIVERS) && !receiverDraft.includes(ALL_RECEIVERS)) {
                      setReceiverDraft([ALL_RECEIVERS]);
                    } else {
                      setReceiverDraft(ids.filter((v) => v !== ALL_RECEIVERS));
                    }
                  }}
                  onDropdownVisibleChange={(o) => {
                    if (!o) commitReceivers();
                  }}
                  optionFilterProp="label"
                  options={[
                    { value: ALL_RECEIVERS, label: "所有项目成员" },
                    ...members.map((m) => ({
                      value: m.userId,
                      label: `${m.displayName}（${m.username}）`,
                      disabled: receiverDraft.includes(ALL_RECEIVERS),
                    })),
                  ]}
                  optionRender={(opt) => {
                    const m = members.find((c) => c.userId === opt.value);
                    return m ? memberOption(m) : opt.label;
                  }}
                />
              ) : (
                <strong className={task.receiverScope === "none" ? "muted" : ""}>
                  {receiverLabel}
                </strong>
              )}
            </div>
          )}
          {(reviewers.length > 0 || task.canManageReviewers) && (
            <div className="task-info-row">
              <span>成果审核人</span>
              {task.canManageReviewers ? (
                <PeopleSelect
                  value={reviewers.map((r) => r.userId)}
                  options={members.filter((m) => m.role !== "viewer")}
                  placeholder="未配置（或签：任一人通过即进入待 KR 终审）"
                  onSave={(ids) => actions.saveReviewers(task, ids)}
                />
              ) : (
                <strong className={reviewers.length ? "" : "muted"}>
                  {reviewers.map((r) => r.displayName).join("、") || "未配置"}
                </strong>
              )}
            </div>
          )}
          <div className="task-info-row">
            <span>周期</span>
            <strong>
              {task.startDate} — {task.endDate}
            </strong>
          </div>
          <div className="task-info-row">
            <span>执行状态</span>
            <div>
              <span className={`status-pill ${STATUS_CLASS[task.status]}`}>
                {task.statusLabel}
              </span>
              {task.status === "cancelled" && task.cancelReason && (
                <span className="muted" style={{ marginLeft: 8, fontSize: 12 }}>
                  原因：{task.cancelReason}
                </span>
              )}
            </div>
          </div>
          {/* #119：进度改为行内可拖进度条（刻度 1%），canUpdateProgress 为假时只读；
              拖动结束（onChangeComplete）才落库，保存失败时清掉草稿回滚显示。
              进度未填且无权限时该行按 AC-50 隐藏；终态 100 由后端派生（#76），前端只消费。 */}
          {(task.progress != null || task.canUpdateProgress) && (
            <div className="task-info-row">
              <span>进度</span>
              <div style={{ display: "flex", alignItems: "center", gap: 12, flex: 1, maxWidth: 340 }}>
                <Slider
                  style={{ flex: 1, margin: "0 4px" }}
                  min={0}
                  max={100}
                  step={1}
                  disabled={!task.canUpdateProgress}
                  value={progressDraft ?? task.progress ?? 0}
                  tooltip={{ formatter: (v) => `${v}%` }}
                  onChange={(v) => setProgressDraft(v)}
                  onChangeComplete={async (v) => {
                    await actions.saveProgress(task, v);
                    setProgressDraft(null);
                  }}
                />
                <strong style={{ minWidth: 40, textAlign: "right" }}>
                  {progressDraft ?? task.progress ?? 0}%
                </strong>
              </div>
            </div>
          )}
          {task.description && (
            <div className="task-info-row">
              <span>任务说明</span>
              <strong>{task.description}</strong>
            </div>
          )}
          {task.completionCriteria && (
            <div className="task-info-row">
              <span>量化标准</span>
              <strong>{task.completionCriteria}</strong>
            </div>
          )}
        </div>
      </section>
      {/* 输入源（§7.5、#101）：必要与参考同区块展示，合并只合展示不合语义——
          只有必要输入未就绪才派生卡点与「等待他人」，参考输入永远只提示，故每行必须标出类别。
          每条输入是一行事实：来源为已有任务时读作「编号 · 标题 · 提供人 · 关系类型」并可点开来源任务；
          来源为指定项目成员时没有来源任务，保留「对接人 · 所需内容」的读法。 */}
      {inputs.length > 0 && (
        <section className="drawer-section" data-focus="inputs">
          <h3>
            输入源{" "}
            <span className="section-count">
              必要 {requiredInputs.length} 项 · 参考 {referenceInputs.length} 项
            </span>
          </h3>
          {[...requiredInputs, ...referenceInputs].map((e) => {
            const required = e.necessity === "required";
            // 未就绪的必要输入才补缺失原因与待行动人，取同一条边派生的上游未就绪卡点。
            const blocker = required && !e.ready ? inputBlockers.get(e.id) : undefined;
            const fact = e.inputRequest
              ? `对接人 ${e.inputRequest.providerName}` +
                (e.inputRequest.contentNote ? ` · ${e.inputRequest.contentNote}` : "")
              : [
                  e.sourceTaskCode,
                  e.sourceTaskName,
                  e.sourceOwnerName ? `提供人 ${e.sourceOwnerName}` : "",
                  e.edgeTypeLabel ?? "",
                ]
                  .filter(Boolean)
                  .join(" · ");
            const openSource = e.sourceTaskId ? () => actions.openTask(e.sourceTaskId!) : undefined;
            return (
              <div key={e.id} className="fact-card fact-card-aux input-row">
                <div className="input-row-text">
                  {openSource ? (
                    <button type="button" className="input-row-main is-link" onClick={openSource} title={fact}>
                      <span className={`necessity-tag ${required ? "required" : "reference"}`}>
                        {required ? "必要" : "参考"}
                      </span>
                      <span className="cell-text">{fact}</span>
                    </button>
                  ) : (
                    <div className="input-row-main" title={fact}>
                      <span className={`necessity-tag ${required ? "required" : "reference"}`}>
                        {required ? "必要" : "参考"}
                      </span>
                      <span className="cell-text">{fact}</span>
                    </div>
                  )}
                  {e.inputRequest?.state === "provided" && (
                    <small className="input-row-note">
                      已提供:{e.inputRequest.providedText || ""}
                      {e.inputRequest.providedFileName && (
                        <span
                          className="file-link"
                          style={{ marginLeft: 6 }}
                          onClick={() => actions.openInputFile(e.inputRequest!.id)}
                        >
                          {e.inputRequest.providedFileName}
                        </span>
                      )}
                    </small>
                  )}
                  {blocker && (
                    <small
                      className="input-row-note"
                      title={`缺失原因:${blocker.reason} · 待行动人 ${blocker.actionOwnerNames.join("、") || "—"}`}
                    >
                      缺失原因:{blocker.reason} · 待行动人 {blocker.actionOwnerNames.join("、") || "—"}
                    </small>
                  )}
                </div>
                <div className="fact-card-actions">
                  {e.inputRequest ? (
                    <span
                      className={`status-pill ${
                        e.inputRequest.state === "provided"
                          ? "completed"
                          : e.inputRequest.state === "accepted"
                            ? "review"
                            : "warning"
                      }`}
                    >
                      {e.inputRequest.state === "provided"
                        ? "已提供"
                        : e.inputRequest.state === "accepted"
                          ? "已接收"
                          : "待接收"}
                    </span>
                  ) : (
                    <span className={`status-pill ${e.ready ? "completed" : "warning"}`}>
                      {e.ready ? "已就绪" : "未就绪"}
                    </span>
                  )}
                  {e.inputRequest?.canAccept && (
                    <Button size="small" onClick={() => actions.acceptInput(e.inputRequest!.id)}>
                      同意接收
                    </Button>
                  )}
                  {e.inputRequest?.canProvide && (
                    <Button size="small" type="primary" onClick={() => actions.openProvide(e.inputRequest!.id)}>
                      提交内容
                    </Button>
                  )}
                  {e.canRemove && (
                    <Button size="small" type="text" onClick={() => actions.removeEdge(e.id)}>
                      解除
                    </Button>
                  )}
                </div>
              </div>
            );
          })}
        </section>
      )}
      {/* 交付物（AC-50/AC-51）：交付物项与当前内容合成一块，每项一行事实——
          当前文件或「尚未提交交付物」、候选提示，有权限时给上传／新增。
          无交付物项且无配置权限时整块隐藏，否则负责人失去唯一的新增／上传候选内容入口。 */}
      {(deliverables.length > 0 || task.canManageDeliverables) && (
        <section className="drawer-section" data-focus="deliverables">
          <h3>
            交付物{" "}
            <span className="section-count">
              {deliverables.length} 项 · 已有当前内容 {currentFiles.length} 项
            </span>
          </h3>
          {reviewingCount > 0 && (
            <div className="notice warning" style={{ marginBottom: 10 }}>
              有 {reviewingCount} 项更新审核中，候选内容请在“审核”Tab 查看；当前内容继续有效。
            </div>
          )}
          {pendingSubmitCount > 0 && (
            <div className="notice" style={{ marginBottom: 10 }}>
              有 {pendingSubmitCount} 项候选内容待提交审核：尚未随完成申请提交，不占用任何人的待办。
            </div>
          )}
          {task.resultUpdate === "open" && (
            <div className="notice" style={{ marginBottom: 10 }}>
              成果更新已发起：上传新的候选内容后提交完成申请，审批期间任务保持已完成、当前内容继续有效。
            </div>
          )}
          {deliverables.length === 0 && <div className="empty compact-empty">尚无交付物项</div>}
          {deliverables.map((d) => (
            <article key={d.id} className="fact-card">
              <div style={{ minWidth: 0 }}>
                <b>{d.name}</b>
                {d.current ? (
                  <>
                    <span className="file-link" onClick={() => openFile(d.current!.id)}>
                      {d.current.fileName}
                    </span>
                    <div className="muted" style={{ fontSize: 12 }}>
                      {fileTypeLabel(d.current.fileName)}
                      {d.current.fileSize ? ` · ${formatFileSize(d.current.fileSize)}` : ""}
                      {d.current.effectiveAt ? ` · 更新于 ${fmtTime(d.current.effectiveAt)}` : ""}
                    </div>
                  </>
                ) : (
                  <div className="muted" style={{ fontSize: 12 }}>
                    尚未提交交付物
                  </div>
                )}
                {d.candidate && (
                  <div className="muted" style={{ fontSize: 12 }}>
                    候选「{d.candidate.fileName}」{d.contentStateLabel}
                  </div>
                )}
              </div>
              <div className="fact-card-actions">
                {d.current && (
                  <Button size="small" onClick={() => openFile(d.current!.id)}>
                    下载
                  </Button>
                )}
                {task.canUploadCandidate && (
                  <Button
                    size="small"
                    loading={uploadingId === d.id}
                    onClick={() => setCandidateFor({ id: d.id, name: d.name })}
                  >
                    {d.candidate ? "重传候选内容" : "上传候选内容"}
                  </Button>
                )}
              </div>
            </article>
          ))}
          {task.canManageDeliverables && (
            <div style={{ display: "flex", gap: 8, marginTop: 8 }}>
              <Button size="small" onClick={() => setAddingDeliverable(true)}>
                ＋ 新增交付物项
              </Button>
              <span className="muted" style={{ fontSize: 12, alignSelf: "center" }}>
                选文件即可，项名取文件名
              </span>
            </div>
          )}
        </section>
      )}
      {/* 过程文件与重要外部材料（§7.7 文件对象边界表）：与交付物并列展示但边界不同——
          不进入完成审批、不作为下游正式输入，可按需选进成果包。 */}
      {(taskFiles.length > 0 || task.canManageDeliverables) && (
        <section className="drawer-section" data-focus="task-files">
          <h3>
            过程文件与外部材料 <span className="section-count">{taskFiles.length} 项</span>
          </h3>
          <div className="notice" style={{ marginBottom: 10 }}>
            这两类文件不进入完成审批，也不作为下游任务的正式输入；可按需选进成果包。
          </div>
          {taskFiles.length === 0 && <div className="empty compact-empty">尚无过程文件与外部材料</div>}
          {taskFiles.map((f) => (
            <article key={f.id} className="fact-card">
              <div style={{ minWidth: 0 }}>
                <b>{f.kindLabel}</b>
                <span className="file-link" onClick={() => openTaskFile(f.id)}>
                  {f.fileName}
                </span>
                <div className="muted" style={{ fontSize: 12 }}>
                  {fileTypeLabel(f.fileName)}
                  {f.fileSize ? ` · ${formatFileSize(f.fileSize)}` : ""}
                  {f.uploadedByName ? ` · ${f.uploadedByName}` : ""}
                  {f.uploadedAt ? ` · ${fmtTime(f.uploadedAt)}` : ""}
                </div>
                {f.note && (
                  <div className="muted" style={{ fontSize: 12 }}>
                    {f.note}
                  </div>
                )}
              </div>
              <div className="fact-card-actions">
                <Button size="small" onClick={() => openTaskFile(f.id)}>
                  下载
                </Button>
                {task.canManageDeliverables && (
                  <Button size="small" onClick={() => removeTaskFile(f.id)}>
                    删除
                  </Button>
                )}
              </div>
            </article>
          ))}
          {task.canManageDeliverables && (
            <div style={{ display: "flex", gap: 8, marginTop: 8 }}>
              <Button size="small" onClick={() => setTaskFileKind("process")}>
                ＋ 上传过程文件
              </Button>
              <Button size="small" onClick={() => setTaskFileKind("external")}>
                ＋ 录入外部材料
              </Button>
            </div>
          )}
        </section>
      )}
      {/* 交付物接收方（词汇表「接收方」「接收记录」；模块 PRD §8.6、MW-09）：
          未配置接收方且本人无配置权限时整块不显示；确认接收只对接收方本人显示，接收方无审核权、不提供退回。 */}
      {/* #135：接收方配置移入基础信息栏，本区块只保留待接收项／接收记录与确认动作。 */}
      {(receipts.length > 0 || task.canConfirmReceipt) && (
        <section className="drawer-section" data-focus="receipts">
          <h3>交付物接收方</h3>
          <div className="task-info-list">
            {receipts.map((rc) => (
              <div key={rc.id} className="task-info-row">
                <span>{rc.displayName}</span>
                <strong className={rc.confirmedAt ? "" : "muted"}>
                  {rc.confirmedAt ? `已确认接收 · ${fmtTime(rc.confirmedAt)}` : "待确认"}
                </strong>
              </div>
            ))}
          </div>
          {task.canConfirmReceipt && (
            <div
              className="notice"
              style={{
                marginTop: 10,
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                gap: 8,
              }}
            >
              <span>成果已交付，确认接收后本任务从「待我接收」移出并形成接收记录。</span>
              <Button type="primary" size="small" onClick={() => actions.confirmReceipt(task)}>
                确认接收
              </Button>
            </div>
          )}
        </section>
      )}
      {/* 当前卡点收在概况末尾（MW PRD §6.1 顺序）：无卡点不显示。 */}
      {blockers.length > 0 && (
        <section className="drawer-section" data-focus="blockers">
          <h3>
            当前卡点{" "}
            <span className="section-count">{blockers.length} 个（系统派生，条件消失即自动解除）</span>
          </h3>
          {blockers.map((b) => (
            <div key={b.key} className="fact-card fact-card-aux fact-card-risk">
              <div>
                <b>
                  {b.kindLabel}:缺 {b.missing}
                  <span className={`status-pill risk-${b.level}`} style={{ marginLeft: 8 }}>
                    {b.levelLabel}
                  </span>
                </b>
                <small>
                  {b.reason} · 待行动人 {b.actionOwnerNames.join("、") || "—"}
                  {b.impactNote ? ` · ${b.impactNote}` : ""}
                </small>
              </div>
              <div className="fact-card-actions">
                {b.canRemind && (
                  <Button size="small" onClick={() => actions.remindBlocker(b.key)}>
                    一键提醒
                  </Button>
                )}
              </div>
            </div>
          ))}
        </section>
      )}
    </>
  );

  // 协作关系 Tab（AC-41/AC-42、协作关系 PRD）：直接消费 API 的 upstream／downstream 派生分组，
  // 前端不再过滤或合并，也不插入交付物节点；两组皆空时 Tab 常驻并显示空态，保证 Tab 数量稳定。
  const relations = (
    <div style={{ paddingTop: 4 }}>
      <section className="drawer-section" style={{ marginTop: 4 }} data-focus="relations">
        <h3 style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          协作关系
          <Button type="link" size="small" onClick={() => actions.openInGraph(task.id)}>
            在关系图谱中查看 →
          </Button>
        </h3>
        {relationCount === 0 ? (
          <div className="empty compact-empty">本任务尚无直接上游或直接下游</div>
        ) : (
          <>
            {relationGroup("直接上游", upstream)}
            {relationGroup("直接下游", downstream)}
          </>
        )}
      </section>
      {/* 受影响 O／KR（CR PRD §8.1）：与页头的「所属 O／KR」是两回事——
          这里只沿下游硬前置边推导，反馈、参考输入与普通双向协作不计入，故标明「系统推导」。 */}
      {impactedTargets.length > 0 && (
        <section className="drawer-section" data-focus="impacted">
          <h3>
            受影响 O／KR <span className="section-count">系统推导 · {impactedTargets.length} 项</span>
          </h3>
          <div className="muted" style={{ fontSize: 12, marginBottom: 6 }}>
            沿下游硬前置交付物边推导；反馈、参考输入与普通双向协作不计入。
          </div>
          {impactedTargets.map((it) => (
            <button
              key={`impacted-${it.keyResultId}`}
              type="button"
              className="fact-card fact-card-link"
              onClick={() => actions.openInGraph(task.id)}
            >
              <span>
                <b title={it.krDescription}>{it.krDescription}</b>
                <small title={it.objectiveTitle}>所属 O：{it.objectiveTitle}</small>
              </span>
              <span className="muted" style={{ fontSize: 12 }}>
                展开影响路径 →
              </span>
            </button>
          ))}
        </section>
      )}
    </div>
  );

  // 动态与讨论 Tab：上段任务动态（ADR 0002）只读倒序、默认最近 5 条；下段讨论意见正序 + 输入框。
  // 两段同 Tab 但不同流：动态不收讨论意见，动态不折叠会把讨论压到很下面、通知跳回也会落错位置。
  const activityAndDiscussion = (
    <div style={{ paddingTop: 4 }}>
      <section className="drawer-section" style={{ marginTop: 4 }}>
        <h3>
          任务动态 <span className="section-count">{activities.length} 条</span>
        </h3>
        {activities.length === 0 ? (
          <div className="empty compact-empty">尚无任务动态</div>
        ) : (
          <>
            <ol className="activity-feed">
              {(activityExpanded ? activities : activities.slice(0, ACTIVITY_PREVIEW)).map((a) => (
                <li key={a.id}>
                  <span className="activity-dot" aria-hidden />
                  <div>
                    <b>{a.summary}</b>
                    <small>
                      {a.actorName ?? "系统"} · {fmtTime(a.occurredAt)}
                    </small>
                  </div>
                </li>
              ))}
            </ol>
            {activities.length > ACTIVITY_PREVIEW && (
              <Button type="link" size="small" onClick={() => setActivityExpanded((v) => !v)}>
                {activityExpanded ? "收起" : `展开全部 ${activities.length} 条`}
              </Button>
            )}
          </>
        )}
      </section>
      <section className="drawer-section">
        <h3>
          讨论意见 <span className="section-count">{discussions.length} 条</span>
        </h3>
        {discussions.length === 0 && <div className="empty compact-empty">尚无讨论意见</div>}
        {discussions.map((d) => (
          <article key={d.id} className="fact-card fact-card-top">
            <div style={{ minWidth: 0 }}>
              <b>
                {d.authorName}
                <span className="muted" style={{ fontWeight: 400, fontSize: 12, marginLeft: 8 }}>
                  {fmtTime(d.createdAt)}
                  {d.mentionNames && d.mentionNames.length > 0 && ` · @ ${d.mentionNames.join("、")}`}
                </span>
              </b>
              <div style={{ whiteSpace: "pre-wrap" }}>{d.content}</div>
            </div>
          </article>
        ))}
        {/* 讨论输入按派生字段显隐：公开项目的隐式访客只读，连讨论也不发（#111）。 */}
        {task.canDiscuss !== false && (
        <div style={{ marginTop: 12 }}>
          <Mentions
            rows={3}
            placeholder="输入文字意见，可使用 @姓名 提醒项目成员"
            value={discussionDraft}
            onChange={setDiscussionDraft}
            options={members.map((m) => ({ value: m.displayName, label: m.displayName }))}
          />
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
              marginTop: 8,
              gap: 8,
            }}
          >
            <span className="muted" style={{ fontSize: 12 }}>
              提交后不可编辑、不可删除；任务负责人和被 @ 成员会收到通知。
            </span>
            <Button
              type="primary"
              size="small"
              loading={postingDiscussion}
              disabled={!discussionDraft.trim()}
              onClick={postDiscussion}
            >
              提交讨论
            </Button>
          </div>
        </div>
        )}
      </section>
    </div>
  );

  const audit = (
    <div style={{ paddingTop: 4 }}>
      {/* #135：成果审核人配置移入基础信息栏，审核 Tab 只保留审核记录。 */}
      {(detail?.poolReviews ?? []).length === 0 &&
        (detail?.fieldChanges ?? []).length === 0 &&
        (detail?.completionReviews ?? []).length === 0 && (
          <div className="empty compact-empty">暂无审核记录</div>
        )}
      {(detail?.completionReviews ?? []).map((cr) => (
        <article
          key={"cr-" + cr.id}
          className={
            "audit-card " +
            (cr.state === "pending_final" || cr.state === "intermediate_review" ? "pending" : "")
          }
        >
          <div className="audit-card-head">
            <div>
              <b>完成申请</b>{" "}
              <span
                className={
                  "status-pill " +
                  (cr.state === "pending_final"
                    ? "review"
                    : cr.state === "approved"
                      ? "completed"
                      : cr.state === "rejected"
                        ? "danger"
                        : "warning")
                }
              >
                {cr.stateLabel}
              </span>
            </div>
            <span className="meta muted" style={{ fontSize: 12 }}>
              申请人 {cr.submittedByName} · {fmtTime(cr.submittedAt)}
            </span>
          </div>
          <div style={{ marginTop: 8, fontSize: 14 }}>
            <div className="muted" style={{ fontSize: 12 }}>
              提交说明:{cr.note}；本次 {cr.items.length} 项候选整体通过或退回。
              {cr.reviewers && cr.reviewers.length > 0 &&
                `或签组：${cr.reviewers.map((r) => r.displayName).join("、")}（任一人通过即进入待 KR 终审）。`}
            </div>
            {cr.intermediateByName && (
              <div className="handled-fact" style={{ marginTop: 6 }}>
                <b>
                  或签通过 · {cr.intermediateByName}
                  {cr.intermediateAt ? ` · ${fmtTime(cr.intermediateAt)}` : ""}
                </b>
                <div>{cr.intermediateOpinion || "未填写意见"}</div>
              </div>
            )}
            {cr.items.map((it) => (
              <div key={cr.id + "-" + it.deliverableId} style={{ marginTop: 4 }}>
                <b>{it.deliverableName}</b>：
                {it.fileId ? (
                  <span className="file-link" onClick={() => openFile(it.fileId!)}>
                    {it.fileName}
                  </span>
                ) : (
                  <span className="muted">{it.fileName}（文件已按覆盖／退回规则删除）</span>
                )}
              </div>
            ))}
          </div>
          {(cr.decidedByName || cr.opinion) && (
            <div className="handled-fact">
              {cr.decidedByName && (
                <b>
                  {cr.decidedByName}
                  {cr.decidedAt ? " · " + fmtTime(cr.decidedAt) : ""}
                </b>
              )}
              <div>{cr.opinion || "未填写意见"}</div>
            </div>
          )}
          {/* #116：或签（intermediate_review）与终审（pending_final）都要渲染动作行，canDecide 由后端派生 */}
          {(cr.state === "pending_final" || cr.state === "intermediate_review") && cr.canDecide && (
            <div className="audit-actions">
              <Button size="small" danger onClick={() => actions.openCrReject(task, cr.id)}>
                退回
              </Button>
              <Button
                size="small"
                type="primary"
                onClick={() =>
                  actions.approveCompletion(task, cr.id, cr.state === "intermediate_review")
                }
              >
                {cr.state === "intermediate_review" ? "通过（进入 KR 终审）" : "通过 / 闭环"}
              </Button>
            </div>
          )}
        </article>
      ))}
      {(detail?.fieldChanges ?? []).map((fc) => (
        <article key={`fc-${fc.id}`} className={`audit-card ${fc.state === "pending" ? "pending" : ""}`}>
          <div className="audit-card-head">
            <div>
              <b>{fc.changeType === "cancel" ? "任务关闭申请" : "关键字段修改"}</b>{" "}
              <span
                className={`status-pill ${
                  fc.state === "pending" ? "warning" : fc.state === "approved" ? "completed" : "danger"
                }`}
              >
                {fc.stateLabel}
              </span>
            </div>
            <span className="meta muted" style={{ fontSize: 12 }}>
              申请人 {fc.submittedByName} · {fmtTime(fc.submittedAt)}
            </span>
          </div>
          <div style={{ marginTop: 8, fontSize: 14 }}>
            {fc.changes.map((c) => (
              <div key={c.field}>
                <span className="muted">{c.label}：</span>
                {c.oldValue || "—"} → <b>{c.newValue}</b>
              </div>
            ))}
            {fc.reason && (
              <div className="muted" style={{ fontSize: 12, marginTop: 4 }}>
                {fc.changeType === "cancel"
                  ? `关闭原因:${fc.reason}；审批通过前任务照常执行。`
                  : `修改原因:${fc.reason}；审批完成前旧值继续生效。`}
              </div>
            )}
          </div>
          {(fc.decidedByName || fc.opinion) && (
            <div className="handled-fact">
              {fc.decidedByName && (
                <b>
                  {fc.decidedByName}
                  {fc.decidedAt ? ` · ${fmtTime(fc.decidedAt)}` : ""}
                </b>
              )}
              <div>{fc.opinion || "未填写意见"}</div>
            </div>
          )}
          {fc.state === "pending" && fc.canDecide && (
            <div className="audit-actions">
              <Button size="small" danger onClick={() => actions.openFcReject(task, fc.id)}>
                退回
              </Button>
              <Button size="small" type="primary" onClick={() => actions.approveFieldChange(task, fc.id)}>
                通过
              </Button>
            </div>
          )}
          {fc.state === "rejected" && !fc.resolved && fc.canAbandon && (
            <div className="audit-actions">
              <Button size="small" onClick={() => actions.abandonFieldChange(task, fc.id)}>
                放弃本次变更
              </Button>
              <Button size="small" type="primary" onClick={() => actions.openEdit(task)}>
                修改并重提
              </Button>
            </div>
          )}
        </article>
      ))}
      {(detail?.poolReviews ?? []).map((r, i) => (
        <article key={i} className={`audit-card ${r.status === "pending" ? "pending" : ""}`}>
          <div className="audit-card-head">
            <div>
              <b>创建入池审批</b>{" "}
              <span
                className={`status-pill ${
                  r.status === "pending" ? "warning" : r.status === "approved" ? "completed" : "danger"
                }`}
              >
                {r.statusLabel}
              </span>
            </div>
            <span className="meta muted" style={{ fontSize: 12 }}>
              申请人 {r.submittedByName} · {fmtTime(r.submittedAt)}
            </span>
          </div>
          {(r.decidedByName || r.opinion) && (
            <div className="handled-fact">
              {r.decidedByName && (
                <b>
                  {r.decidedByName}
                  {r.decidedAt ? ` · ${fmtTime(r.decidedAt)}` : ""}
                </b>
              )}
              <div>{r.opinion || "未填写意见"}</div>
            </div>
          )}
          {r.status === "pending" && task.canDecidePoolReview && (
            <div className="audit-actions">
              <Button size="small" danger onClick={() => actions.openReject(task)}>
                退回
              </Button>
              <Button size="small" type="primary" onClick={() => actions.approvePool(task)}>
                通过
              </Button>
            </div>
          )}
        </article>
      ))}
    </div>
  );

  return (
    <Drawer
      open={!!task}
      onClose={onClose}
      // 基线 §7：任务详情抽屉宽 min(740px, 100vw)。
      width="min(740px, 100vw)"
      // 关闭图标顶格在右上角、与标题分离（#99、AC-50）；命中区在 .task-drawer 里放大到 32×32。
      className="task-drawer"
      closable={false}
      extra={
        <button
          type="button"
          className="drawer-close"
          onClick={onClose}
          aria-label={canGoBack ? "返回上一个任务详情" : "关闭任务详情"}
          title={canGoBack ? "返回上一个任务详情" : "关闭任务详情"}
        >
          <Icon name={canGoBack ? "back" : "close"} size={16} />
        </button>
      }
      title={
        <div>
          {code} · {task.name}
          {/* 所属 O／KR 只显编号：展开标题会把页头挤满，更新时间是任务的、
              放在这里容易被误读成 O／KR 的更新时间（#99）。 */}
          <div className="drawer-sub">所属 O／KR：{okrCode.get(task.keyResultId) ?? "—"}</div>
        </div>
      }
      footer={
        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          {task.canProposeFieldChange && (
            <Button onClick={() => actions.openEdit(task)}>编辑任务</Button>
          )}
          {task.canManageDeliverables && (
            <Button onClick={() => actions.openConfigureInput(task)}>配置输入</Button>
          )}
          {task.canStart && <Button onClick={() => actions.start(task)}>开始执行</Button>}
          {task.canCancel && <Button onClick={() => actions.openCancel(task)}>关闭任务</Button>}
          {task.canSubmitPoolReview && (
            <Button type="primary" onClick={() => actions.submitPool(task)}>
              提交入池审批
            </Button>
          )}
          {task.canStartResultUpdate && (
            <Button onClick={() => actions.startResultUpdate(task)}>发起成果更新</Button>
          )}
          {task.canSubmitCompletion && (
            <Button type="primary" onClick={() => actions.openSubmitCompletion(task)}>
              {task.resultUpdate === "open" ? "提交成果更新" : "提交完成申请"}
            </Button>
          )}
        </div>
      }
    >
      <div ref={drawerRef}>
      <Tabs
        key={task.id}
        className="task-drawer-tabs"
        activeKey={activeTab}
        onChange={onTabChange}
        items={[
          { key: "overview", label: "任务概况", children: overview },
          { key: "relations", label: `协作关系 ${relationCount}`, children: relations },
          { key: "audit", label: `审核 ${pendingReviews}`, children: audit },
          {
            key: "discussion",
            label: `动态与讨论 ${discussions.length}`,
            children: activityAndDiscussion,
          },
        ]}
      />
      </div>
      <Modal
        title="新增交付物项"
        open={addingDeliverable}
        okText="确认上传"
        cancelText="取消"
        confirmLoading={newDeliverableBusy}
        okButtonProps={{ disabled: !newDeliverableFile }}
        onCancel={closeAddDeliverable}
        onOk={addDeliverable}
      >
        <FileUploadField value={newDeliverableFile} onChange={setNewDeliverableFile} />
        <div className="notice" style={{ marginTop: 8 }}>
          交付物项名取所选文件的文件名（不含扩展名），之后更新成果不会改变项名；同名的交付物项已存在时请在该项上「重传候选内容」。
        </div>
      </Modal>
      <Modal
        title={candidateFor ? `上传候选内容 · ${candidateFor.name}` : "上传候选内容"}
        open={!!candidateFor}
        okText="确认上传"
        cancelText="取消"
        confirmLoading={uploadingId != null}
        okButtonProps={{ disabled: !candidateFile }}
        onCancel={closeCandidate}
        onOk={uploadCandidate}
      >
        <FileUploadField value={candidateFile} onChange={setCandidateFile} />
        <div className="notice" style={{ marginTop: 8 }}>
          文件在点击「确认上传」后才登记为候选内容；关闭窗口不保留本次选择。候选内容随完成申请整体提交后进入审核。
        </div>
      </Modal>
      <Modal
        title={taskFileKind === "external" ? "录入重要外部材料" : "上传过程文件"}
        open={!!taskFileKind}
        okText="确认上传"
        cancelText="取消"
        confirmLoading={taskFileBusy}
        okButtonProps={{ disabled: !taskFileValue }}
        onCancel={closeTaskFile}
        onOk={uploadTaskFile}
      >
        <FileUploadField value={taskFileValue} onChange={setTaskFileValue} />
        <Input.TextArea
          rows={2}
          maxLength={500}
          style={{ marginTop: 8 }}
          placeholder="背景说明（选填）"
          value={taskFileNote}
          onChange={(e) => setTaskFileNote(e.target.value)}
        />
        <div className="notice" style={{ marginTop: 8 }}>
          {taskFileKind === "external"
            ? "外部材料由内部协调人代为录入：可作为输入证据，但不会把任何输入置为就绪，也不进入完成审批。"
            : "过程文件不进入完成审批，也不作为下游任务的正式输入；可按需选进成果包。"}
        </div>
      </Modal>
    </Drawer>
  );
}

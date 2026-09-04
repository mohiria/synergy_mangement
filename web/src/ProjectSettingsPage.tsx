import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Button, Dropdown, Input, InputNumber, Modal, Select, Spin, message } from "antd";
import type { MenuProps } from "antd";
import dayjs, { type Dayjs } from "dayjs";
import DateRangeField from "./DateRangeField";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";
import Icon from "./icons";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type ProjectMember = components["schemas"]["ProjectMember"];
type SkippedMember = components["schemas"]["SkippedMember"];
type AuditLog = components["schemas"]["AuditLog"];
type ImportRecord = components["schemas"]["ImportRecord"];
type MemberRole = components["schemas"]["MemberRole"];
type UserSummary = components["schemas"]["UserSummary"];
type ProjectSettings = components["schemas"]["ProjectSettings"];
type ProjectStatus = components["schemas"]["ProjectStatus"];
type ProjectVisibility = components["schemas"]["ProjectVisibility"];

// 项目状态候选项：下拉要列出全部取值，此时没有对象可取派生的 statusLabel，只能在前端枚举；
// 已有项目的状态显示一律取后端 statusLabel（与角色同口径，F1）。
const PROJECT_STATUS_LABEL: Record<ProjectStatus, string> = {
  not_started: "未开始",
  in_progress: "进行中",
  completed: "已完成",
  archived: "已归档",
};

// 项目可见性候选项：同上，下拉要列出全部取值；已有项目的显示一律取后端 visibilityLabel。
const PROJECT_VISIBILITY_LABEL: Record<ProjectVisibility, string> = {
  private: "私有项目",
  public: "公开项目",
};

// 项目基础信息表单的本地草稿（§7.9 项目设置首项）。
type BasicDraft = {
  name: string;
  ownerId?: number;
  status: ProjectStatus;
  stage: string;
  visibility: ProjectVisibility;
  plan?: [Dayjs | null, Dayjs | null];
};

function toBasicDraft(p: Project): BasicDraft {
  return {
    name: p.name,
    ownerId: p.ownerId,
    status: p.status,
    stage: p.stage ?? "",
    visibility: p.visibility,
    plan:
      p.plannedStartDate || p.plannedEndDate
        ? [
            p.plannedStartDate ? dayjs(p.plannedStartDate) : null,
            p.plannedEndDate ? dayjs(p.plannedEndDate) : null,
          ]
        : undefined,
  };
}

// 候选项文案：角色下拉要列出全部取值，此时没有对应成员可取派生字段，只能在前端枚举。
// 已有成员的角色显示一律取后端的 roleLabel（F1）。
const ROLE_LABEL: Record<MemberRole, string> = {
  admin: "项目管理员",
  member: "项目成员",
  viewer: "访客",
};

const ROLE_ORDER: MemberRole[] = ["admin", "member", "viewer"];
// 成员管理区里可以互相切换的两档；访客的进出走「转为访客／转为项目成员」（#108）。
const WORKING_ROLES: MemberRole[] = ["admin", "member"];

const roleOptions = ROLE_ORDER.map((r) => ({ value: r, label: ROLE_LABEL[r] }));

// 工作职责与系统权限的对应说明（PRD §3.1～3.4 原文口径，只读展示，不承载配置）。
const RESPONSIBILITY_NOTES: [string, string][] = [
  ["项目总负责人", "查看完整项目并处理决策"],
  ["总推进人", "项目录入、维护、协调与报告"],
  ["任务负责人／参与人", "执行、提交成果和处理输入"],
  ["访客", "查看完整上下文，不可修改"],
];

// 规则设置三项（主 PRD §7.9、我的工作 PRD §8.8；AC-60）：均有默认值，仅项目管理员可改。
// 阈值只在此处配置，卡点派生与我的工作读同一份值，前端不复算任何判定。
const RULE_FIELDS: {
  key: keyof Omit<ProjectSettings, "canEdit">;
  label: string;
  suffix: string;
  min: number;
  max: number;
  note: string;
}[] = [
  {
    key: "approvalTimeoutDays",
    label: "审批超时阈值",
    suffix: "天",
    min: 1,
    max: 30,
    note: "审批件在当前审批环节等待达到该天数即超时，三道审批共用；进入新环节重新计时。",
  },
  {
    key: "dueSoonDays",
    label: "临期阈值",
    suffix: "天",
    min: 1,
    max: 30,
    note: "距任务截止日期不足该天数即计入风险等级的「预警」判定。",
  },
  {
    key: "remindDailyLimit",
    label: "一键提醒频次上限",
    suffix: "次／天",
    min: 1,
    max: 20,
    note: "同一发起人对同一被提醒人的同一任务每天可提醒的次数；换一个被提醒人不受影响。",
  },
];

// 项目设置 → 成员管理（#29；PRD §7.9 将成员与权限归入项目设置）。
// 视觉按原型 renderSettings：card-head + member-grid 成员卡一种形态；
// 管理动作按 canManageMembers 派生字段显隐，前端不复算规则。
export default function ProjectSettingsPage({
  user,
  onLogout,
}: {
  user: CurrentUser;
  onLogout: () => void;
}) {
  const { projectId: projectIdParam } = useParams();
  const projectId = Number(projectIdParam);

  const [tab, setTab] = useState<
    "basic" | "members" | "permissions" | "rules" | "audit" | "imports"
  >("basic");
  // 项目基础信息（§7.9 首项）：与项目列表页的「编辑项目」弹窗复用同一个 PUT /projects/{id}。
  const [basic, setBasic] = useState<BasicDraft | null>(null);
  const [savingBasic, setSavingBasic] = useState(false);
  // 导入记录（§7.9、AC-68）：每次表格导入的操作人、时间、文件名、影响计数与结果，只读。
  const [importRecords, setImportRecords] = useState<ImportRecord[]>([]);
  // 操作审计（§10.4）：由后端写路径装饰器统一记录，这里只读展示。
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>([]);
  const [project, setProject] = useState<Project | null>(null);
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [users, setUsers] = useState<UserSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [inviteOpen, setInviteOpen] = useState(false);
  // 邀请成员一次可选多人（#93），本次选中的人共用同一个角色。
  const [addUserIds, setAddUserIds] = useState<number[]>([]);
  const [skipped, setSkipped] = useState<SkippedMember[]>([]);
  const [addRole, setAddRole] = useState<MemberRole>("member");
  const [saving, setSaving] = useState(false);
  const [settings, setSettings] = useState<ProjectSettings | null>(null);
  const [rules, setRules] = useState<ProjectSettings | null>(null);
  const [savingRules, setSavingRules] = useState(false);

  const load = useCallback(async () => {
    const [projectRes, membersRes, usersRes, settingsRes, auditRes, importRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      // #204：成员管理要看到停用成员并打「已停用」标签（人员选择器默认不带回）。
      client.GET("/projects/{projectId}/members", { params: { path: { projectId }, query: { includeDisabled: true } } }),
      client.GET("/users"),
      client.GET("/projects/{projectId}/settings", { params: { path: { projectId } } }),
      // 只有项目管理员能读审计；非管理员会拿到 403，此时留空即可（导航里也不显示这一节）。
      client.GET("/projects/{projectId}/audit-logs", { params: { path: { projectId } } }),
      // 导入记录同样只对项目管理员开放；非管理员拿到 403，留空即可。
      client.GET("/projects/{projectId}/import-records", { params: { path: { projectId } } }),
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
    setBasic(toBasicDraft(projectRes.data));
    setMembers(membersRes.data ?? []);
    setUsers(usersRes.data ?? []);
    setSettings(settingsRes.data ?? null);
    setRules(settingsRes.data ?? null);
    setAuditLogs(auditRes.data ?? []);
    setImportRecords(importRes.data ?? []);
    setLoading(false);
  }, [projectId, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  const canManage = project?.canManageMembers ?? false;

  const openInvite = () => {
    setAddUserIds([]);
    setAddRole("member");
    setSkipped([]);
    setError(null);
    setInviteOpen(true);
  };

  const add = async () => {
    if (addUserIds.length === 0) return;
    setSaving(true);
    setError(null);
    const res = await client.POST("/projects/{projectId}/members", {
      params: { path: { projectId } },
      body: { userIds: addUserIds, role: addRole },
    });
    setSaving(false);
    if (res.data) {
      // 逐人结果由后端给出：全部加入才关弹窗，有人被跳过时留在弹窗里说明原因。
      if (res.data.added.length > 0) message.success(`已加入 ${res.data.added.length} 人`);
      setSkipped(res.data.skipped);
      setAddUserIds([]);
      if (res.data.skipped.length === 0) setInviteOpen(false);
      load();
    } else {
      setError(res.error?.message ?? "加入成员失败");
    }
  };

  const changeRole = async (userId: number, role: MemberRole) => {
    setError(null);
    const res = await client.PUT("/projects/{projectId}/members/{userId}", {
      params: { path: { projectId, userId } },
      body: { role },
    });
    if (res.data) {
      load();
    } else {
      setError(res.error?.message ?? "调整角色失败");
    }
  };

  const remove = async (userId: number) => {
    setError(null);
    const res = await client.DELETE("/projects/{projectId}/members/{userId}", {
      params: { path: { projectId, userId } },
    });
    if (res.response.status === 204) {
      load();
    } else {
      setError(res.error?.message ?? "移出成员失败");
    }
  };

  const saveBasic = async () => {
    if (!basic || !basic.ownerId) return;
    setSavingBasic(true);
    setError(null);
    const res = await client.PUT("/projects/{projectId}", {
      params: { path: { projectId } },
      body: {
        name: basic.name.trim(),
        ownerId: basic.ownerId,
        status: basic.status,
        stage: basic.stage.trim() || undefined,
        visibility: basic.visibility,
        plannedStartDate: basic.plan?.[0]?.format("YYYY-MM-DD"),
        plannedEndDate: basic.plan?.[1]?.format("YYYY-MM-DD"),
      },
    });
    setSavingBasic(false);
    if (res.data) {
      setProject(res.data);
      setBasic(toBasicDraft(res.data));
    } else {
      setError(res.error?.message ?? "保存项目基础信息失败");
    }
  };

  const saveRules = async () => {
    if (!rules) return;
    setSavingRules(true);
    setError(null);
    const res = await client.PUT("/projects/{projectId}/settings", {
      params: { path: { projectId } },
      body: {
        approvalTimeoutDays: rules.approvalTimeoutDays,
        dueSoonDays: rules.dueSoonDays,
        remindDailyLimit: rules.remindDailyLimit,
      },
    });
    setSavingRules(false);
    if (res.data) {
      setSettings(res.data);
      setRules(res.data);
    } else {
      setError(res.error?.message ?? "保存规则设置失败");
    }
  };

  // 与规则设置同一套「改动过才可保存」口径。
  const basicDirty =
    !!basic &&
    !!project &&
    JSON.stringify({
      ...basic,
      plan: basic.plan?.map((d) => d?.format("YYYY-MM-DD") ?? null),
    }) !==
      JSON.stringify({
        ...toBasicDraft(project),
        plan: toBasicDraft(project).plan?.map((d) => d?.format("YYYY-MM-DD") ?? null),
      });

  const rulesDirty =
    !!rules &&
    !!settings &&
    RULE_FIELDS.some((f) => rules[f.key] !== settings[f.key]);

  const candidateOptions = users
    .filter((u) => !members.some((m) => m.userId === u.id))
    .map((u) => ({ value: u.id, label: `${u.displayName}（${u.username}）` }));

  // 两区各自排序：成员管理区按 管理员 → 项目成员，查看项目区只有访客（#108）。
  const workingMembers = members
    .filter((m) => m.role !== "viewer")
    .sort((a, b) => ROLE_ORDER.indexOf(a.role) - ROLE_ORDER.indexOf(b.role));
  const viewers = members.filter((m) => m.role === "viewer");

  // 两区共用同一张卡，只有「更多」菜单不同。
  const renderMemberCard = (m: ProjectMember, items: MenuProps["items"]) => (
    <div key={m.userId} className="member-card">
      <span className="avatar">{m.displayName.slice(0, 1)}</span>
      <div className="member-card-text">
        <b title={m.displayName}>
          {m.displayName}
          {m.disabled && (
            <span className="status-pill" style={{ marginLeft: 6 }}>
              已停用
            </span>
          )}
        </b>
        <span title={`${m.roleLabel ?? ""} · ${m.username}`}>
          {m.roleLabel ?? ""} · {m.username}
        </span>
      </div>
      {canManage && (
        <Dropdown trigger={["click"]} placement="bottomRight" menu={{ items }}>
          <button
            className="icon-btn member-card-more"
            type="button"
            aria-label={`管理成员 ${m.displayName}`}
          >
            <Icon name="more" size={16} />
          </button>
        </Dropdown>
      )}
    </div>
  );

  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="项目设置"
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
              <h1>项目设置</h1>
              <p>
                配置同一项目中的职责与操作权限；访客仍可查看完整上下文。
                {!canManage && "（你没有成员管理权限，以下为只读展示）"}
              </p>
            </div>
          </div>
          {error && <Alert type="error" message={error} style={{ marginBottom: 16 }} />}
          {/* settings-layout：左侧分节导航、右侧内容卡。原型的「进度权重」一节已随
              AC-63 裁决取消（KR 汇总固定任务等权）；导入记录另见 #68。 */}
          <div className="settings-layout">
            <aside className="settings-nav">
              <button
                type="button"
                className={tab === "basic" ? "active" : ""}
                onClick={() => setTab("basic")}
              >
                项目基础信息
              </button>
              <button
                type="button"
                className={tab === "members" ? "active" : ""}
                onClick={() => setTab("members")}
              >
                成员与职责
              </button>
              <button
                type="button"
                className={tab === "permissions" ? "active" : ""}
                onClick={() => setTab("permissions")}
              >
                系统权限
              </button>
              <button
                type="button"
                className={tab === "rules" ? "active" : ""}
                onClick={() => setTab("rules")}
              >
                规则设置
              </button>
              {project?.canEdit && (
                <button
                  type="button"
                  className={tab === "imports" ? "active" : ""}
                  onClick={() => setTab("imports")}
                >
                  导入记录
                </button>
              )}
              {project?.canEdit && (
                <button
                  type="button"
                  className={tab === "audit" ? "active" : ""}
                  onClick={() => setTab("audit")}
                >
                  操作审计
                </button>
              )}
            </aside>
            <section className="settings-panel">
              {tab === "basic" ? (
                <>
                  <div className="settings-panel-head">
                    <div>
                      <h2>项目基础信息</h2>
                      <span className="muted">
                        项目名称、负责人、状态、阶段与计划周期（§7.9 项目设置首项）。
                        {project && !project.canEdit && "（你没有配置权限，以下为只读展示）"}
                      </span>
                    </div>
                    {project?.canEdit && (
                      <Button
                        size="small"
                        type="primary"
                        loading={savingBasic}
                        disabled={!basicDirty}
                        onClick={saveBasic}
                      >
                        保存
                      </Button>
                    )}
                  </div>
                  {basic && (
                    <div className="settings-panel-body">
                      <div className="property">
                        <label>项目名称</label>
                        <Input
                          maxLength={100}
                          value={basic.name}
                          disabled={!project?.canEdit}
                          onChange={(e) => setBasic({ ...basic, name: e.target.value })}
                          style={{ width: 280, flex: "none" }}
                          aria-label="项目名称"
                        />
                      </div>
                      <div className="property">
                        <label>项目负责人</label>
                        <Select
                          value={basic.ownerId}
                          disabled={!project?.canEdit}
                          showSearch
                          optionFilterProp="label"
                          options={users.map((u) => ({
                            value: u.id,
                            label: `${u.displayName}（${u.username}）`,
                          }))}
                          onChange={(v) => setBasic({ ...basic, ownerId: v })}
                          style={{ width: 280, flex: "none" }}
                          aria-label="项目负责人"
                        />
                      </div>
                      <div className="property">
                        <label>
                          项目状态
                          <span className="muted" style={{ display: "block" }}>
                            与自由文本的「项目阶段」正交，由成员手工设置
                          </span>
                        </label>
                        <Select
                          value={basic.status}
                          disabled={!project?.canEdit}
                          options={(Object.keys(PROJECT_STATUS_LABEL) as ProjectStatus[]).map(
                            (v) => ({ value: v, label: PROJECT_STATUS_LABEL[v] }),
                          )}
                          onChange={(v) => setBasic({ ...basic, status: v })}
                          style={{ width: 160, flex: "none" }}
                          aria-label="项目状态"
                        />
                      </div>
                      <div className="property">
                        <label>项目阶段</label>
                        <Input
                          maxLength={50}
                          placeholder="业务里程碑，如：联合联调阶段（选填）"
                          value={basic.stage}
                          disabled={!project?.canEdit}
                          onChange={(e) => setBasic({ ...basic, stage: e.target.value })}
                          style={{ width: 280, flex: "none" }}
                          aria-label="项目阶段"
                        />
                      </div>
                      <div className="property">
                        <label>
                          项目可见性
                          <span className="muted" style={{ display: "block" }}>
                            公开后系统内任何登录用户都能只读本项目并下载文件，但不能做任何写动作，
                            也不会出现在成员列表与人员选择器里
                          </span>
                        </label>
                        <Select
                          value={basic.visibility}
                          disabled={!project?.canEdit}
                          options={(Object.keys(PROJECT_VISIBILITY_LABEL) as ProjectVisibility[]).map(
                            (v) => ({ value: v, label: PROJECT_VISIBILITY_LABEL[v] }),
                          )}
                          onChange={(v) => setBasic({ ...basic, visibility: v })}
                          style={{ width: 160, flex: "none" }}
                          aria-label="项目可见性"
                        />
                      </div>
                      <div className="property">
                        <label>计划周期</label>
                        <div style={{ width: 280, flex: "none" }}>
                          <DateRangeField
                            allowEmpty
                            value={basic.plan}
                            disabled={!project?.canEdit}
                            onChange={(v) => setBasic({ ...basic, plan: v ?? undefined })}
                            aria-label="计划周期"
                          />
                        </div>
                      </div>
                    </div>
                  )}
                </>
              ) : tab === "imports" ? (
                <>
                  <div className="settings-panel-head">
                    <div>
                      <h2>导入记录</h2>
                      <span className="muted">
                        每次表格导入留存操作人、时间、源文件名、本次新建的 O／KR／任务数量与结果（§7.9）；
                        失败的一次同样留记录，结果不写成功。只读。
                      </span>
                    </div>
                  </div>
                  {importRecords.length === 0 ? (
                    <div className="empty">暂无导入记录</div>
                  ) : (
                    <div className="data-table-wrap">
                      <table className="data-table">
                        <thead>
                          <tr>
                            <th style={{ width: 170 }}>时间</th>
                            <th style={{ width: 110 }}>操作人</th>
                            <th style={{ width: 200 }}>源文件</th>
                            <th style={{ width: 150 }}>新建 O／KR／任务</th>
                            <th style={{ width: 110 }}>结果</th>
                            <th>失败摘要</th>
                          </tr>
                        </thead>
                        <tbody>
                          {importRecords.map((rec) => (
                            <tr key={rec.id}>
                              <td className="mono">
                                {new Date(rec.importedAt).toLocaleString("zh-CN")}
                              </td>
                              <td title={rec.operatorName}>{rec.operatorName}</td>
                              <td
                                className={rec.sourceFileName ? "" : "muted"}
                                title={rec.sourceFileName || undefined}
                              >
                                {rec.sourceFileName || "—"}
                              </td>
                              <td className="mono">
                                {rec.objectiveCount} / {rec.keyResultCount} / {rec.taskCount}
                              </td>
                              <td>
                                <span
                                  className={`status-pill ${
                                    rec.result === "success" ? "completed" : "risk-high_risk"
                                  }`}
                                >
                                  {rec.resultLabel}
                                </span>
                              </td>
                              <td
                                className={rec.failureSummary ? "" : "muted"}
                                title={rec.failureSummary || undefined}
                              >
                                {rec.failureSummary || "—"}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </>
              ) : tab === "audit" ? (
                <>
                  <div className="settings-panel-head">
                    <div>
                      <h2>操作审计</h2>
                      <span className="muted">
                        项目内每一次成功的写操作都在这里留痕（§10.4）：谁、什么时候、对哪个对象做了什么。
                        由后端写路径统一记录，新增功能自动覆盖。
                      </span>
                    </div>
                  </div>
                  {auditLogs.length === 0 ? (
                    <div className="empty">暂无操作记录</div>
                  ) : (
                    <div className="data-table-wrap">
                      <table className="data-table">
                        <thead>
                          <tr>
                            <th style={{ width: 170 }}>时间</th>
                            <th style={{ width: 110 }}>操作人</th>
                            <th>动作</th>
                            <th style={{ width: 160 }}>对象</th>
                          </tr>
                        </thead>
                        <tbody>
                          {auditLogs.map((a) => (
                            <tr key={a.id}>
                              <td className="mono">{new Date(a.occurredAt).toLocaleString("zh-CN")}</td>
                              <td title={a.actorName ?? "系统"}>{a.actorName ?? "系统"}</td>
                              <td title={a.action}>{a.action}</td>
                              <td className="muted">
                                {a.objectType ? `${a.objectType}${a.objectId ? ` #${a.objectId}` : ""}` : "—"}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </>
              ) : tab === "rules" ? (
                <>
                  <div className="settings-panel-head">
                    <div>
                      <h2>规则设置</h2>
                      <span className="muted">
                        按项目生效，均有默认值；卡点派生、我的工作与一键提醒读同一份值。
                        {settings && !settings.canEdit && "（你没有配置权限，以下为只读展示）"}
                      </span>
                    </div>
                    {settings?.canEdit && (
                      <Button
                        size="small"
                        type="primary"
                        loading={savingRules}
                        disabled={!rulesDirty}
                        onClick={saveRules}
                      >
                        保存
                      </Button>
                    )}
                  </div>
                  <div className="settings-panel-body">
                    {rules &&
                      RULE_FIELDS.map((f) => (
                        <div key={f.key} className="property">
                          <label>
                            {f.label}
                            <span className="muted" style={{ display: "block" }}>
                              {f.note}
                            </span>
                          </label>
                          <InputNumber
                            min={f.min}
                            max={f.max}
                            precision={0}
                            value={rules[f.key]}
                            disabled={!settings?.canEdit}
                            onChange={(v) =>
                              setRules({ ...rules, [f.key]: v ?? settings?.[f.key] ?? f.min })
                            }
                            addonAfter={f.suffix}
                            style={{ width: 160, flex: "none" }}
                            aria-label={f.label}
                          />
                        </div>
                      ))}
                  </div>
                </>
              ) : tab === "permissions" ? (
                <>
                  <div className="settings-panel-head">
                    <div>
                      <h2>统一权限体系</h2>
                    </div>
                  </div>
                  <div className="settings-panel-body">
                    <div className="notice" style={{ marginBottom: 12 }}>
                      所有成员查看同一份项目事实；权限只决定创建、编辑、审批、闭环和配置操作。
                    </div>
                    {RESPONSIBILITY_NOTES.map(([role, note]) => (
                      <div key={role} className="property">
                        <label>{role}</label>
                        <strong>{note}</strong>
                      </div>
                    ))}
                  </div>
                </>
              ) : (
                <>
                  {/* 成员分两区（裁决 C1、#108）：「成员管理」放项目管理员与项目成员，
                      「查看项目」放访客；角色不混排，跨区调整用明确的转换动作。 */}
                  <div className="settings-panel-head">
                    <div>
                      <h2>成员与职责</h2>
                      <span className="muted">
                        成员角色决定编辑结构与配置的系统权限；KR
                        负责人等工作职责在 OKR 与任务中指定。
                        {!canManage && "（你没有成员管理权限，以下为只读展示）"}
                      </span>
                    </div>
                    {canManage && (
                      <Button size="small" icon={<Icon name="plus" size={14} />} onClick={openInvite}>
                        邀请成员
                      </Button>
                    )}
                  </div>
                  <div className="settings-panel-body">
                    <div className="member-zone">
                      <h3>
                        成员管理 <span className="section-count">{workingMembers.length} 人</span>
                      </h3>
                      <p className="muted">项目管理员与项目成员：可编辑项目事实并承担工作职责。</p>
                      <div className="member-grid">
                        {workingMembers.map((m) =>
                          renderMemberCard(m, [
                            {
                              key: "role",
                              type: "group",
                              label: "调整角色",
                              children: WORKING_ROLES.map((r) => ({
                                key: r,
                                label: ROLE_LABEL[r],
                                disabled: r === m.role,
                                onClick: () => changeRole(m.userId, r),
                              })),
                            },
                            { type: "divider" as const, key: "d1" },
                            {
                              key: "to-viewer",
                              label: "转为访客",
                              onClick: () => changeRole(m.userId, "viewer"),
                            },
                            { type: "divider" as const, key: "d2" },
                            {
                              key: "remove",
                              label: "移出项目",
                              danger: true,
                              onClick: () => remove(m.userId),
                            },
                          ]),
                        )}
                      </div>
                      {workingMembers.length === 0 && <div className="empty compact-empty">暂无成员</div>}
                    </div>
                    <div className="member-zone">
                      <h3>
                        查看项目 <span className="section-count">{viewers.length} 人</span>
                      </h3>
                      <p className="muted">访客：可看可下载，被指定为接收方时可确认接收，此外没有写入口。</p>
                      <div className="member-grid">
                        {viewers.map((m) =>
                          renderMemberCard(m, [
                            {
                              key: "to-member",
                              label: "转为项目成员",
                              onClick: () => changeRole(m.userId, "member"),
                            },
                            { type: "divider" as const, key: "d" },
                            {
                              key: "remove",
                              label: "移出项目",
                              danger: true,
                              onClick: () => remove(m.userId),
                            },
                          ]),
                        )}
                      </div>
                      {viewers.length === 0 && <div className="empty compact-empty">暂无访客</div>}
                    </div>
                  </div>
                </>
              )}
            </section>
          </div>
          {/* 邀请成员一次可选多人（#93）：候选列表已排除项目内成员，
              选中的人共用同一个角色；提交后的逐人结果由后端返回，前端不自行判断。 */}
          <Modal
            title="邀请成员"
            open={inviteOpen}
            confirmLoading={saving}
            okText={addUserIds.length > 1 ? `加入 ${addUserIds.length} 人` : "加入"}
            cancelText="取消"
            okButtonProps={{ disabled: addUserIds.length === 0 }}
            onOk={add}
            onCancel={() => setInviteOpen(false)}
            width={480}
            destroyOnHidden
          >
            <div className="form-stack">
              <label>
                <span>选择用户（可多选）</span>
                <Select
                  mode="multiple"
                  style={{ width: "100%" }}
                  options={candidateOptions}
                  value={addUserIds}
                  onChange={setAddUserIds}
                  showSearch
                  optionFilterProp="label"
                  placeholder="搜索姓名或用户名"
                  notFoundContent="没有可加入的用户"
                  maxTagCount="responsive"
                />
              </label>
              <label>
                <span>成员角色</span>
                <Select
                  style={{ width: "100%" }}
                  options={roleOptions}
                  value={addRole}
                  onChange={setAddRole}
                />
              </label>
              {skipped.length > 0 && (
                <Alert
                  type="warning"
                  message={`${skipped.length} 人未加入`}
                  description={
                    <ul className="skipped-list">
                      {skipped.map((sk) => (
                        <li key={sk.userId}>
                          {sk.displayName ?? `用户 #${sk.userId}`} · {sk.reasonLabel}
                        </li>
                      ))}
                    </ul>
                  }
                />
              )}
            </div>
          </Modal>
        </>
      )}
    </ProjectShell>
  );
}

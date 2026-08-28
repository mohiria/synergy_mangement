import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Button, Dropdown, InputNumber, Modal, Select, Spin } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";
import Icon from "./icons";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type ProjectMember = components["schemas"]["ProjectMember"];
type MemberRole = components["schemas"]["MemberRole"];
type UserSummary = components["schemas"]["UserSummary"];
type ProjectSettings = components["schemas"]["ProjectSettings"];

const ROLE_LABEL: Record<MemberRole, string> = {
  admin: "项目管理员",
  member: "普通成员",
  viewer: "只读成员",
};

const ROLE_ORDER: MemberRole[] = ["admin", "member", "viewer"];

const roleOptions = ROLE_ORDER.map((r) => ({ value: r, label: ROLE_LABEL[r] }));

// 工作职责与系统权限的对应说明（PRD §3.1～3.4 原文口径，只读展示，不承载配置）。
const RESPONSIBILITY_NOTES: [string, string][] = [
  ["项目总负责人", "查看完整项目并处理决策"],
  ["总推进人", "项目录入、维护、协调与报告"],
  ["KR 负责人", "入池、关键字段变更和完成终审"],
  ["任务负责人／参与人", "执行、提交成果和处理输入"],
  ["只读成员", "查看完整上下文，不可修改"],
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
    label: "一键提醒冷却",
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

  const [tab, setTab] = useState<"members" | "permissions" | "rules">("members");
  const [project, setProject] = useState<Project | null>(null);
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [users, setUsers] = useState<UserSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [inviteOpen, setInviteOpen] = useState(false);
  const [addUserId, setAddUserId] = useState<number | undefined>();
  const [addRole, setAddRole] = useState<MemberRole>("member");
  const [saving, setSaving] = useState(false);
  const [settings, setSettings] = useState<ProjectSettings | null>(null);
  const [rules, setRules] = useState<ProjectSettings | null>(null);
  const [savingRules, setSavingRules] = useState(false);

  const load = useCallback(async () => {
    const [projectRes, membersRes, usersRes, settingsRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/members", { params: { path: { projectId } } }),
      client.GET("/users"),
      client.GET("/projects/{projectId}/settings", { params: { path: { projectId } } }),
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
    setMembers(membersRes.data ?? []);
    setUsers(usersRes.data ?? []);
    setSettings(settingsRes.data ?? null);
    setRules(settingsRes.data ?? null);
    setLoading(false);
  }, [projectId, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  const canManage = project?.canManageMembers ?? false;

  const openInvite = () => {
    setAddUserId(undefined);
    setAddRole("member");
    setError(null);
    setInviteOpen(true);
  };

  const add = async () => {
    if (addUserId == null) return;
    setSaving(true);
    setError(null);
    const res = await client.POST("/projects/{projectId}/members", {
      params: { path: { projectId } },
      body: { userId: addUserId, role: addRole },
    });
    setSaving(false);
    if (res.data) {
      setInviteOpen(false);
      setAddUserId(undefined);
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

  const rulesDirty =
    !!rules &&
    !!settings &&
    RULE_FIELDS.some((f) => rules[f.key] !== settings[f.key]);

  const candidateOptions = users
    .filter((u) => !members.some((m) => m.userId === u.id))
    .map((u) => ({ value: u.id, label: `${u.displayName}（${u.username}）` }));

  const sortedMembers = [...members].sort(
    (a, b) => ROLE_ORDER.indexOf(a.role) - ROLE_ORDER.indexOf(b.role),
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
                配置同一项目中的职责与操作权限；只读成员仍可查看完整上下文。
                {!canManage && "（你没有成员管理权限，以下为只读展示）"}
              </p>
            </div>
          </div>
          {error && <Alert type="error" message={error} style={{ marginBottom: 16 }} />}
          {/* settings-layout：左侧分节导航、右侧内容卡。原型另有进度权重、导入记录与
              操作审计三节，本版没有对应数据模型，故不列入导航。 */}
          <div className="settings-layout">
            <aside className="settings-nav">
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
            </aside>
            <section className="settings-panel">
              {tab === "rules" ? (
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
                  <div className="settings-panel-head">
                    <div>
                      <h2>成员与职责</h2>
                      <span className="muted">
                        成员角色决定编辑结构与配置的系统权限；KR
                        负责人等工作职责在 OKR 与任务中指定。
                      </span>
                    </div>
                    {canManage && (
                      <Button size="small" icon={<Icon name="plus" size={14} />} onClick={openInvite}>
                        邀请成员
                      </Button>
                    )}
                  </div>
                  <div className="settings-panel-body">
                    <div className="member-grid">
                      {sortedMembers.map((m) => (
                        <div key={m.userId} className="member-card">
                          <span className="avatar">{m.displayName.slice(0, 1)}</span>
                          <div className="member-card-text">
                            <b>{m.displayName}</b>
                            <span>
                              {ROLE_LABEL[m.role]} · {m.username}
                            </span>
                          </div>
                          {canManage && (
                            <Dropdown
                              trigger={["click"]}
                              placement="bottomRight"
                              menu={{
                                items: [
                                  {
                                    key: "role",
                                    type: "group",
                                    label: "调整角色",
                                    children: ROLE_ORDER.map((r) => ({
                                      key: r,
                                      label: ROLE_LABEL[r],
                                      disabled: r === m.role,
                                      onClick: () => changeRole(m.userId, r),
                                    })),
                                  },
                                  { type: "divider" as const, key: "d" },
                                  {
                                    key: "remove",
                                    label: "移出项目",
                                    danger: true,
                                    onClick: () => remove(m.userId),
                                  },
                                ],
                              }}
                            >
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
                      ))}
                    </div>
                    {members.length === 0 && <div className="empty">暂无成员</div>}
                  </div>
                </>
              )}
            </section>
          </div>
          <Modal
            title="邀请成员"
            open={inviteOpen}
            confirmLoading={saving}
            okText="加入"
            cancelText="取消"
            okButtonProps={{ disabled: addUserId == null }}
            onOk={add}
            onCancel={() => setInviteOpen(false)}
            width={480}
            destroyOnHidden
          >
            <div className="form-stack">
              <label>
                <span>选择用户</span>
                <Select
                  style={{ width: "100%" }}
                  options={candidateOptions}
                  value={addUserId}
                  onChange={setAddUserId}
                  showSearch
                  optionFilterProp="label"
                  placeholder="搜索姓名或用户名"
                  notFoundContent="没有可加入的用户"
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
            </div>
          </Modal>
        </>
      )}
    </ProjectShell>
  );
}

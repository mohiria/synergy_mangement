import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { Alert, Button, Form, Input, Modal, Popover, Select, Table } from "antd";
import dayjs, { type Dayjs } from "dayjs";
import { client } from "./api/client";
import type { components } from "./api/schema";
import DateRangeField from "./DateRangeField";
import Icon from "./icons";
import NotificationBell from "./NotificationBell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type ProjectStatus = components["schemas"]["ProjectStatus"];
type UserSummary = components["schemas"]["UserSummary"];

// 候选项文案：下拉里要列出全部状态，此时还没有对应实体可取派生字案，只能在前端枚举。
// 已存在实体的显示文案一律取后端的 statusLabel（F1）。
const STATUS_LABEL: Record<ProjectStatus, string> = {
  not_started: "未开始",
  in_progress: "进行中",
  completed: "已完成",
  archived: "已归档",
};

type ProjectFormValues = {
  name: string;
  ownerId: number;
  status?: ProjectStatus;
  stage?: string;
  plan?: [Dayjs | null, Dayjs | null];
};

function toBody(values: ProjectFormValues) {
  return {
    name: values.name,
    ownerId: values.ownerId,
    stage: values.stage?.trim() || undefined,
    plannedStartDate: values.plan?.[0]?.format("YYYY-MM-DD"),
    plannedEndDate: values.plan?.[1]?.format("YYYY-MM-DD"),
  };
}

export default function ProjectsPage({
  user,
  onLogout,
}: {
  user: CurrentUser;
  onLogout: () => void;
}) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [users, setUsers] = useState<UserSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [modalOpen, setModalOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<ProjectStatus | "all">("all");
  // 归属：我参与的（成员／负责人）与公开可见的（我不是成员，凭项目公开才看得到）。
  const [scopeFilter, setScopeFilter] = useState<"all" | "mine" | "public">("all");
  const [form] = Form.useForm<ProjectFormValues>();

  const load = useCallback(async () => {
    setLoading(true);
    const [projectsRes, usersRes] = await Promise.all([
      client.GET("/projects"),
      client.GET("/users"),
    ]);
    if (projectsRes.response.status === 401 || usersRes.response.status === 401) {
      onLogout();
      return;
    }
    setProjects(projectsRes.data ?? []);
    setUsers(usersRes.data ?? []);
    setLoading(false);
  }, [onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  const logout = async () => {
    await client.POST("/auth/logout");
    onLogout();
  };

  const openCreate = () => {
    setSaveError(null);
    form.resetFields();
    setModalOpen(true);
  };

  const save = async (values: ProjectFormValues) => {
    setSaving(true);
    setSaveError(null);
    // 这里只建新项目；已有项目的基础信息在项目设置页改（§7.9、#85）。
    const result = await client.POST("/projects", { body: toBody(values) });
    setSaving(false);
    if (result.data) {
      setModalOpen(false);
      form.resetFields();
      load();
    } else {
      setSaveError(result.error?.message ?? "保存失败");
    }
  };

  const ownerOptions = users.map((u) => ({
    value: u.id,
    label: `${u.displayName}（${u.username}）`,
  }));

  // 搜索与筛选只作用于展示，不改变服务端返回的事实。
  // 「我参与的」排在前面：公开项目对全体登录用户可见，不先排一下会把个人列表淹掉（#111）。
  const visible = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return projects
      .filter((p) => {
        if (statusFilter !== "all" && p.status !== statusFilter) return false;
        if (scopeFilter === "mine" && p.implicitViewer) return false;
        if (scopeFilter === "public" && !p.implicitViewer) return false;
        if (!needle) return true;
        return (p.name + p.ownerName + (p.stage ?? "")).toLowerCase().includes(needle);
      })
      .sort((a, b) => Number(a.implicitViewer) - Number(b.implicitViewer));
  }, [projects, search, statusFilter, scopeFilter]);
  const publicCount = projects.filter((p) => p.implicitViewer).length;

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark">协</span>
          <div className="brand-name">
            <b>协同管理工具</b>
            <span>O／KR／任务协同推进</span>
          </div>
        </div>
        <nav>
          <button className="nav-row active" type="button">
            <Icon name="package" />
            <span>项目列表</span>
          </button>
        </nav>
      </aside>
      <section className="workspace">
        <header className="topbar">
          <div className="breadcrumbs">
            <b>项目列表</b>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <NotificationBell />
          <Popover
            trigger="click"
            placement="bottomRight"
            content={
              <div className="identity-popover">
                <div className="identity-popover-head">
                  <span className="avatar">{user.displayName.slice(0, 1)}</span>
                  <span>
                    <b>{user.displayName}</b>
                    <small>{user.username}</small>
                  </span>
                </div>
                <Button block onClick={logout}>
                  登出
                </Button>
              </div>
            }
          >
            <button className="identity" type="button" aria-label="当前身份">
              <span className="avatar">{user.displayName.slice(0, 1)}</span>
              <span className="who">
                <b>{user.displayName}</b>
                <small>{user.username}</small>
              </span>
              <Icon name="down" size={15} />
            </button>
          </Popover>
          </div>
        </header>
        <main className="page">
          <div className="page-head">
            <div>
              <h1>项目列表</h1>
              <p>组织多个 O 的协作空间；选择项目后进入项目总览与我的工作。</p>
            </div>
            <Button type="primary" onClick={openCreate} icon={<Icon name="plus" size={15} />}>
              新建项目
            </Button>
          </div>
          <div className="toolbar">
            <div className="toolbar-group">
              <Input
                allowClear
                prefix={<Icon name="search" size={15} />}
                style={{ width: 240 }}
                placeholder="搜索项目、负责人或阶段"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              <Select
                style={{ width: 150 }}
                value={statusFilter}
                onChange={setStatusFilter}
                options={[
                  { value: "all" as const, label: "全部状态" },
                  ...(Object.keys(STATUS_LABEL) as ProjectStatus[]).map((s) => ({
                    value: s,
                    label: STATUS_LABEL[s],
                  })),
                ]}
              />
              <Select
                style={{ width: 150 }}
                value={scopeFilter}
                onChange={setScopeFilter}
                options={[
                  { value: "all" as const, label: "全部项目" },
                  { value: "mine" as const, label: "我参与的" },
                  { value: "public" as const, label: `公开可见的${publicCount ? `（${publicCount}）` : ""}` },
                ]}
              />
            </div>
            <span className="muted" style={{ fontSize: "var(--type-aux)" }}>
              共 {visible.length} 个项目
            </span>
          </div>
          <div className="data-table-wrap">
            <Table<Project>
              className="flat-table"
              rowKey="id"
              loading={loading}
              dataSource={visible}
              locale={{ emptyText: projects.length ? "没有匹配的项目" : "暂无项目" }}
              pagination={false}
              columns={[
                // 列表字段一律单行截断、悬停看全称（#91）；ellipsis 同时把 antd 表格切到固定布局。
                {
                  title: "项目名称",
                  dataIndex: "name",
                  ellipsis: { showTitle: false },
                  render: (v: string, p) => (
                    <Link
                      className="project-name-cell"
                      to={`/projects/${p.id}`}
                      title={p.implicitViewer ? `${v}（${p.visibilityLabel} · 我不是成员，只读）` : v}
                    >
                      <span className={"project-dot " + p.status} />
                      <strong className="cell-text">{v}</strong>
                      {/* 只标「不是成员」这一种：自己参与的项目公开与否不影响他自己能做什么。 */}
                      {p.implicitViewer && <span className="status-pill">{p.visibilityLabel} · 只读</span>}
                    </Link>
                  ),
                },
                {
                  title: "负责人",
                  dataIndex: "ownerName",
                  width: 150,
                  ellipsis: { showTitle: false },
                  render: (v: string) => (
                    <span className="owner-cell" title={v}>
                      <span className="avatar">{v.slice(0, 1)}</span>
                      <span className="cell-text">{v}</span>
                    </span>
                  ),
                },
                {
                  title: "状态",
                  dataIndex: "status",
                  width: 110,
                  // 显示文案取后端派生字段；本页保留的 STATUS_LABEL 只用于筛选与新建的候选项。
                  render: (v: ProjectStatus, row: Project) => (
                    <span className={`status-pill ${v}`}>{row.statusLabel}</span>
                  ),
                },
                {
                  title: "阶段",
                  dataIndex: "stage",
                  width: 170,
                  ellipsis: true,
                  render: (v?: string) => v ?? <span className="muted">—</span>,
                },
                {
                  title: "计划周期",
                  width: 210,
                  render: (_, p) =>
                    p.plannedStartDate || p.plannedEndDate ? (
                      <span className="mono">
                        {p.plannedStartDate ?? "…"} ～ {p.plannedEndDate ?? "…"}
                      </span>
                    ) : (
                      <span className="muted">—</span>
                    ),
                },
                {
                  title: "创建时间",
                  dataIndex: "createdAt",
                  width: 160,
                  render: (v: string) => (
                    <span className="mono">{dayjs(v).format("YYYY-MM-DD HH:mm")}</span>
                  ),
                },
                {
                  title: "操作",
                  width: 110,
                  render: (_, p) => (
                    <span className="row-actions left">
                      <Link className="link-btn" to={`/projects/${p.id}`}>
                        进入
                      </Link>
                      {/* 项目基础信息的编辑入口收口到项目设置页（§7.9 首项、#85）：
                          两处口径一致，这里只留跳转，不再另开一份表单。 */}
                      {p.canEdit && (
                        <Link className="link-btn" to={`/projects/${p.id}/settings`}>
                          设置
                        </Link>
                      )}
                    </span>
                  ),
                },
              ]}
            />
          </div>
        </main>
      </section>
      <Modal
        title="新建项目"
        open={modalOpen}
        confirmLoading={saving}
        onOk={() => form.submit()}
        onCancel={() => {
          setModalOpen(false);
          setSaveError(null);
        }}
        okText="创建"
        cancelText="取消"
        destroyOnHidden
      >
        {saveError && <Alert type="error" message={saveError} style={{ marginBottom: 16 }} />}
        <Form form={form} layout="vertical" onFinish={save} requiredMark={false}>
          <Form.Item name="name" label="项目名称" rules={[{ required: true, message: "请输入项目名称" }]}>
            <Input maxLength={100} autoFocus placeholder="不超过 100 字" />
          </Form.Item>
          <Form.Item name="ownerId" label="项目负责人" rules={[{ required: true, message: "请选择项目负责人" }]}>
            <Select
              options={ownerOptions}
              showSearch
              optionFilterProp="label"
              placeholder="选择负责人"
            />
          </Form.Item>
          <Form.Item name="stage" label="阶段（选填）">
            <Input maxLength={50} placeholder="业务里程碑，如：联合联调阶段" />
          </Form.Item>
          <Form.Item name="plan" label="计划周期（选填）">
            <DateRangeField allowEmpty />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}

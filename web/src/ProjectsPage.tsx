import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Alert, Button, Form, Input, Modal, Popover, Select, Table } from "antd";
import dayjs, { type Dayjs } from "dayjs";
import { client } from "./api/client";
import type { components } from "./api/schema";
import DateRangeField from "./DateRangeField";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type ProjectStatus = components["schemas"]["ProjectStatus"];
type UserSummary = components["schemas"]["UserSummary"];

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
  const [editing, setEditing] = useState<Project | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
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
    setEditing(null);
    setSaveError(null);
    form.resetFields();
    setModalOpen(true);
  };

  const openEdit = (p: Project) => {
    setEditing(p);
    setSaveError(null);
    form.setFieldsValue({
      name: p.name,
      ownerId: p.ownerId,
      status: p.status,
      stage: p.stage ?? undefined,
      plan:
        p.plannedStartDate || p.plannedEndDate
          ? [
              p.plannedStartDate ? dayjs(p.plannedStartDate) : null,
              p.plannedEndDate ? dayjs(p.plannedEndDate) : null,
            ]
          : undefined,
    });
    setModalOpen(true);
  };

  const save = async (values: ProjectFormValues) => {
    setSaving(true);
    setSaveError(null);
    const result = editing
      ? await client.PUT("/projects/{projectId}", {
          params: { path: { projectId: editing.id } },
          body: { ...toBody(values), status: values.status ?? editing.status },
        })
      : await client.POST("/projects", { body: toBody(values) });
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
            项目列表
          </button>
        </nav>
      </aside>
      <section className="workspace">
        <header className="topbar">
          <div className="breadcrumbs">
            <b>项目列表</b>
          </div>
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
              <span>{user.displayName}</span>
            </button>
          </Popover>
        </header>
        <main className="page">
          <div className="page-head">
            <div>
              <h1>项目列表</h1>
              <p>组织多个 O 的协作空间；选择项目后进入项目总览与我的工作。</p>
            </div>
            <Button type="primary" onClick={openCreate}>
              新建项目
            </Button>
          </div>
          <Table<Project>
            rowKey="id"
            loading={loading}
            dataSource={projects}
            locale={{ emptyText: "暂无项目" }}
            pagination={false}
            columns={[
              {
                title: "项目名称",
                dataIndex: "name",
                render: (v: string, p) => (
                  <Link to={`/projects/${p.id}`}>
                    <strong>{v}</strong>
                  </Link>
                ),
              },
              {
                title: "负责人",
                dataIndex: "ownerName",
                width: 140,
                render: (v: string) => (
                  <span className="owner-cell">
                    <span className="avatar">{v.slice(0, 1)}</span>
                    {v}
                  </span>
                ),
              },
              {
                title: "状态",
                dataIndex: "status",
                width: 110,
                render: (v: ProjectStatus) => (
                  <span className={`status-pill ${v}`}>{STATUS_LABEL[v]}</span>
                ),
              },
              {
                title: "阶段",
                dataIndex: "stage",
                width: 160,
                render: (v?: string) => v ?? <span className="muted">—</span>,
              },
              {
                title: "计划周期",
                width: 220,
                render: (_, p) =>
                  p.plannedStartDate || p.plannedEndDate ? (
                    <span>
                      {p.plannedStartDate ?? "…"} ～ {p.plannedEndDate ?? "…"}
                    </span>
                  ) : (
                    <span className="muted">—</span>
                  ),
              },
              {
                title: "创建时间",
                dataIndex: "createdAt",
                width: 170,
                render: (v: string) => dayjs(v).format("YYYY-MM-DD HH:mm"),
              },
              {
                title: "操作",
                width: 130,
                render: (_, p) => (
                  <>
                    {p.canEdit && (
                      <Button type="link" size="small" onClick={() => openEdit(p)}>
                        编辑
                      </Button>
                    )}

                  </>
                ),
              },
            ]}
          />
        </main>
      </section>
      <Modal
        title={editing ? "编辑项目" : "新建项目"}
        open={modalOpen}
        confirmLoading={saving}
        onOk={() => form.submit()}
        onCancel={() => {
          setModalOpen(false);
          setSaveError(null);
        }}
        okText={editing ? "保存" : "创建"}
        cancelText="取消"
        destroyOnClose
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
          {editing && (
            <Form.Item name="status" label="状态" rules={[{ required: true, message: "请选择状态" }]}>
              <Select
                options={(Object.keys(STATUS_LABEL) as ProjectStatus[]).map((s) => ({
                  value: s,
                  label: STATUS_LABEL[s],
                }))}
              />
            </Form.Item>
          )}
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

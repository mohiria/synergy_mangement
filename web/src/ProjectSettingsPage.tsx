import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Button, Select, Space, Spin, Table } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type ProjectMember = components["schemas"]["ProjectMember"];
type MemberRole = components["schemas"]["MemberRole"];
type UserSummary = components["schemas"]["UserSummary"];

const ROLE_LABEL: Record<MemberRole, string> = {
  admin: "项目管理员",
  member: "普通成员",
  viewer: "只读成员",
};

const roleOptions = (Object.keys(ROLE_LABEL) as MemberRole[]).map((r) => ({
  value: r,
  label: ROLE_LABEL[r],
}));

// 项目设置 → 成员管理（#29；PRD §7.9 将成员与权限归入项目设置）。
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

  const [project, setProject] = useState<Project | null>(null);
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [users, setUsers] = useState<UserSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [addUserId, setAddUserId] = useState<number | undefined>();
  const [addRole, setAddRole] = useState<MemberRole>("member");
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    const [projectRes, membersRes, usersRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/members", { params: { path: { projectId } } }),
      client.GET("/users"),
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
    setLoading(false);
  }, [projectId, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  const canManage = project?.canManageMembers ?? false;

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

  const candidateOptions = users
    .filter((u) => !members.some((m) => m.userId === u.id))
    .map((u) => ({ value: u.id, label: `${u.displayName}（${u.username}）` }));

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
              <h1>项目设置 · 成员管理</h1>
              <p>
                成员角色决定编辑结构与配置的系统权限；KR 负责人等工作职责在 OKR 与任务中指定。
                {!canManage && "（你没有成员管理权限，以下为只读展示）"}
              </p>
            </div>
          </div>
          {error && <Alert type="error" message={error} style={{ marginBottom: 16 }} />}
          {canManage && (
            <Space.Compact block style={{ marginBottom: 16, maxWidth: 560 }}>
              <Select
                style={{ flex: 1 }}
                options={candidateOptions}
                value={addUserId}
                onChange={setAddUserId}
                showSearch
                optionFilterProp="label"
                placeholder="选择要加入的用户"
              />
              <Select style={{ width: 130 }} options={roleOptions} value={addRole} onChange={setAddRole} />
              <Button type="primary" onClick={add} loading={saving} disabled={addUserId == null}>
                加入
              </Button>
            </Space.Compact>
          )}
          <div className="data-table-wrap" style={{ maxWidth: 720 }}>
            <Table<ProjectMember>
              rowKey="userId"
              size="small"
              dataSource={members}
              locale={{ emptyText: "暂无成员" }}
              pagination={false}
              columns={[
                {
                  title: "成员",
                  render: (_, m) => (
                    <span className="owner-cell">
                      <span className="avatar">{m.displayName.slice(0, 1)}</span>
                      {m.displayName}
                      <span className="muted">（{m.username}）</span>
                    </span>
                  ),
                },
                {
                  title: "角色",
                  width: 150,
                  render: (_, m) =>
                    canManage ? (
                      <Select
                        size="small"
                        style={{ width: 130 }}
                        options={roleOptions}
                        value={m.role}
                        onChange={(role) => changeRole(m.userId, role)}
                      />
                    ) : (
                      ROLE_LABEL[m.role]
                    ),
                },
                ...(canManage
                  ? [
                      {
                        title: "操作",
                        width: 80,
                        render: (_: unknown, m: ProjectMember) => (
                          <Button type="link" size="small" danger onClick={() => remove(m.userId)}>
                            移出
                          </Button>
                        ),
                      },
                    ]
                  : []),
              ]}
            />
          </div>
        </>
      )}
    </ProjectShell>
  );
}

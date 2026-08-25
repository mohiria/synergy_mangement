import { useCallback, useEffect, useState } from "react";
import { Alert, Button, Modal, Select, Space, Table } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";

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

export default function ProjectMembersModal({
  project,
  users,
  onClose,
}: {
  project: Project | null;
  users: UserSummary[];
  onClose: () => void;
}) {
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [addUserId, setAddUserId] = useState<number | undefined>();
  const [addRole, setAddRole] = useState<MemberRole>("member");
  const [saving, setSaving] = useState(false);

  const projectId = project?.id;
  const canManage = project?.canManageMembers ?? false;

  const load = useCallback(async () => {
    if (projectId == null) return;
    setLoading(true);
    setError(null);
    const res = await client.GET("/projects/{projectId}/members", {
      params: { path: { projectId } },
    });
    setLoading(false);
    if (res.data) {
      setMembers(res.data);
    } else {
      setError(res.error?.message ?? "加载成员失败");
    }
  }, [projectId]);

  useEffect(() => {
    setMembers([]);
    setAddUserId(undefined);
    setAddRole("member");
    setError(null);
    load();
  }, [load]);

  const add = async () => {
    if (projectId == null || addUserId == null) return;
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
    if (projectId == null) return;
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
    if (projectId == null) return;
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
    <Modal
      title={project ? `项目成员 · ${project.name}` : "项目成员"}
      open={project != null}
      onCancel={onClose}
      footer={null}
      destroyOnClose
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 16 }} />}
      {canManage && (
        <Space.Compact block style={{ marginBottom: 16 }}>
          <Select
            style={{ flex: 1 }}
            options={candidateOptions}
            value={addUserId}
            onChange={setAddUserId}
            showSearch
            optionFilterProp="label"
            placeholder="选择要加入的用户"
          />
          <Select
            style={{ width: 130 }}
            options={roleOptions}
            value={addRole}
            onChange={setAddRole}
          />
          <Button type="primary" onClick={add} loading={saving} disabled={addUserId == null}>
            加入
          </Button>
        </Space.Compact>
      )}
      <Table<ProjectMember>
        rowKey="userId"
        size="small"
        loading={loading}
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
    </Modal>
  );
}

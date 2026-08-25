import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Spin, Tabs } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type MyWork = components["schemas"]["MyWork"];
type WorkItem = components["schemas"]["WorkItem"];

// 我的工作（AC-16）：五分组个人行动与等待事实；卡片入口一律打开任务详情抽屉。
export default function MyWorkPage({
  user,
  onLogout,
}: {
  user: CurrentUser;
  onLogout: () => void;
}) {
  const { projectId: projectIdParam } = useParams();
  const projectId = Number(projectIdParam);
  const navigate = useNavigate();

  const [project, setProject] = useState<Project | null>(null);
  const [work, setWork] = useState<MyWork | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, workRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/my-work", { params: { path: { projectId } } }),
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
    setWork(workRes.data ?? null);
    setLoading(false);
  }, [projectId, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  const openItem = (item: WorkItem) => {
    if (item.taskId) {
      navigate(`/projects/${projectId}/tasks?task=${item.taskId}&tab=${item.drawerTab ?? "overview"}`);
    } else {
      navigate(`/projects/${projectId}/tasks`);
    }
  };

  const renderGroup = (items: WorkItem[], emptyText: string) => (
    <div>
      {items.length === 0 && <div className="empty compact-empty">{emptyText}</div>}
      {items.map((it, i) => (
        <div
          key={`${it.kind}-${it.refId ?? it.taskId ?? i}`}
          className="input-fact"
          style={{ cursor: "pointer" }}
          onClick={() => openItem(it)}
        >
          <div style={{ minWidth: 0 }}>
            <b>
              {it.title}
              {it.overdue && (
                <span className="status-pill risk-high_risk" style={{ marginLeft: 8 }}>
                  超期
                </span>
              )}
            </b>
            <small>
              {it.stage ? `当前环节:${it.stage}` : ""}
              {it.dueDate ? `　截止/期望:${it.dueDate}` : ""}
              {it.waitingDays != null ? `　已等待 ${it.waitingDays} 天` : ""}
            </small>
            {it.unreadyNote && (
              <small style={{ color: "var(--amber)" }}>{it.unreadyNote}</small>
            )}
            {it.rejectedReason && (
              <small style={{ color: "var(--red)" }}>已退回:{it.rejectedReason}</small>
            )}
          </div>
          <span className="muted" style={{ fontSize: 12 }}>
            去处理 →
          </span>
        </div>
      ))}
    </div>
  );

  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="我的工作"
      onLogout={onLogout}
    >
      {notFound ? (
        <Alert type="error" message="项目不存在" description={<Link to="/">返回项目列表</Link>} />
      ) : loading || !work ? (
        <Spin />
      ) : (
        <>
          <div className="page-head">
            <div>
              <h1>我的工作</h1>
              <p>按当前职责派生的个人行动与等待事实；处理动作在任务详情抽屉中完成。</p>
            </div>
          </div>
          <Tabs
            items={[
              {
                key: "pending",
                label: `待我处理 ${work.pending.length}`,
                children: renderGroup(work.pending, "暂无需要你处理的事项"),
              },
              {
                key: "approvals",
                label: `待我审批 ${work.approvals.length}`,
                children: renderGroup(work.approvals, "暂无等待你审批的事项"),
              },
              {
                key: "receipts",
                label: `待我接收 ${work.receipts.length}`,
                children: renderGroup(work.receipts, "暂无待你确认接收的交付物"),
              },
              {
                key: "waiting",
                label: `等待他人 ${work.waiting.length}`,
                children: renderGroup(work.waiting, "没有停在他人手里的事项"),
              },
              {
                key: "blockers",
                label: `与我相关的卡点 ${work.blockers.length}`,
                children: renderGroup(work.blockers, "没有与你相关的卡点"),
              },
            ]}
          />
        </>
      )}
    </ProjectShell>
  );
}

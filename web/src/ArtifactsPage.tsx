import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Checkbox, Input, Modal, Spin, message } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type ArtifactObjective = components["schemas"]["ArtifactObjective"];
type ArtifactPackage = components["schemas"]["ArtifactPackage"];

const fmtTime = (s?: string) => (s ? s.slice(0, 16).replace("T", " ") : "");

// 成果、归档与成果包（AC-17/18）：按 O/KR/任务查看当前成果；勾选当前成果生成轻量成果包。
export default function ArtifactsPage({
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
  const [artifacts, setArtifacts] = useState<ArtifactObjective[]>([]);
  const [packages, setPackages] = useState<ArtifactPackage[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [pkgModal, setPkgModal] = useState(false);
  const [pkgName, setPkgName] = useState("");
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, artifactsRes, packagesRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/artifacts", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/packages", { params: { path: { projectId } } }),
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
    setArtifacts(artifactsRes.data ?? []);
    setPackages(packagesRes.data ?? []);
    setLoading(false);
  }, [projectId, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  const openFile = async (fileId: number) => {
    const res = await client.GET("/projects/{projectId}/files/{fileId}/download-url", {
      params: { path: { projectId, fileId } },
    });
    if (res.data) window.open(res.data.url, "_blank");
    else message.error(res.error?.message ?? "获取下载地址失败");
  };

  const toggle = (deliverableId: number) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(deliverableId)) next.delete(deliverableId);
      else next.add(deliverableId);
      return next;
    });

  const createPackage = async () => {
    setSaving(true);
    const res = await client.POST("/projects/{projectId}/packages", {
      params: { path: { projectId } },
      body: { name: pkgName.trim(), deliverableIds: [...selected] },
    });
    setSaving(false);
    if (res.data) {
      message.success("成果包已生成");
      setPkgModal(false);
      setPkgName("");
      setSelected(new Set());
      load();
    } else {
      message.error(res.error?.message ?? "生成失败");
    }
  };

  const canCreate = !!project?.canEdit;

  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="成果"
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
              <h1>成果、归档与成果包</h1>
              <p>只保留当前有效内容，不提供历史版本或旧文件入口；成果包目录引用当前内容，不复制文件。</p>
            </div>
            {canCreate && (
              <Button
                type="primary"
                disabled={selected.size === 0}
                onClick={() => setPkgModal(true)}
              >
                生成成果包（已选 {selected.size} 项）
              </Button>
            )}
          </div>
          {artifacts.length === 0 && <div className="empty">尚无带交付物的任务</div>}
          {artifacts.map((o) => (
            <section key={o.objectiveId} className="objective">
              <div className="objective-head">
                <span className="objective-code">O</span>
                <div>
                  <h2>{o.title}</h2>
                </div>
                <span />
              </div>
              {o.krs.map((kr) => (
                <div key={kr.keyResultId} style={{ padding: "10px 16px", borderTop: "1px solid #edf0f2" }}>
                  <b style={{ fontSize: 13 }}>{kr.description}</b>
                  {kr.tasks.map((t) => (
                    <div key={t.taskId} style={{ margin: "8px 0 4px 12px" }}>
                      <div style={{ fontSize: 13, marginBottom: 4 }}>
                        {t.name}
                        <Button
                          type="link"
                          size="small"
                          onClick={() =>
                            navigate(`/projects/${projectId}/tasks?task=${t.taskId}&tab=audit`)
                          }
                        >
                          审批记录 {t.reviewCount} 条
                        </Button>
                      </div>
                      {t.deliverables.map((d) => (
                        <div key={d.id} className="deliverable-card" style={{ marginLeft: 12 }}>
                          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                            {canCreate && d.current && (
                              <Checkbox
                                checked={selected.has(d.id)}
                                onChange={() => toggle(d.id)}
                              />
                            )}
                            <div>
                              <b>{d.name}</b>
                              {d.current ? (
                                <>
                                  <span className="file-link" onClick={() => openFile(d.current!.id)}>
                                    {d.current.fileName}
                                  </span>
                                  <div className="muted" style={{ fontSize: 12 }}>
                                    {d.current.effectiveAt
                                      ? `生效于 ${fmtTime(d.current.effectiveAt)}`
                                      : "已生效"}
                                    {d.candidate && " · 有更新审核中（候选内容见任务审核 Tab）"}
                                  </div>
                                </>
                              ) : (
                                <div className="muted" style={{ fontSize: 12 }}>
                                  尚无已生效当前内容
                                  {d.candidate && " · 候选审核中"}
                                </div>
                              )}
                            </div>
                          </div>
                          {d.current && (
                            <Button size="small" onClick={() => openFile(d.current!.id)}>
                              下载
                            </Button>
                          )}
                        </div>
                      ))}
                    </div>
                  ))}
                </div>
              ))}
            </section>
          ))}
          <section className="drawer-section" style={{ marginTop: 24 }}>
            <h3>成果包</h3>
            {packages.length === 0 && <div className="empty compact-empty">尚未生成成果包</div>}
            {packages.map((p) => (
              <article key={p.id} className="audit-card">
                <div className="audit-card-head">
                  <b>{p.name}</b>
                  <span className="meta muted" style={{ fontSize: 12 }}>
                    {p.createdByName} · {fmtTime(p.createdAt)}
                  </span>
                </div>
                <div style={{ marginTop: 6, fontSize: 13 }}>
                  {p.items.map((it) => (
                    <div key={it.deliverableId}>
                      <span className="muted">{it.taskName} / </span>
                      {it.deliverableName}
                      {it.fileName ? (
                        <span className="muted"> → {it.fileName}</span>
                      ) : (
                        <span className="muted">（暂无已生效当前内容）</span>
                      )}
                    </div>
                  ))}
                </div>
                <div className="audit-actions">
                  <Button
                    size="small"
                    onClick={() =>
                      window.open(`/api/v1/projects/${projectId}/packages/${p.id}/download`, "_blank")
                    }
                  >
                    整包下载
                  </Button>
                </div>
              </article>
            ))}
          </section>
          <Modal
            title="生成成果包"
            open={pkgModal}
            okText="生成"
            cancelText="取消"
            confirmLoading={saving}
            okButtonProps={{ disabled: !pkgName.trim() }}
            onCancel={() => setPkgModal(false)}
            onOk={createPackage}
          >
            <p className="muted" style={{ marginTop: 0 }}>
              目录只引用勾选的当前成果；交付物被覆盖后，包内对应项自动解析为新的当前内容。
            </p>
            <Input
              maxLength={100}
              placeholder="成果包名称（如：联调阶段成果）"
              value={pkgName}
              onChange={(e) => setPkgName(e.target.value)}
            />
          </Modal>
        </>
      )}
    </ProjectShell>
  );
}

import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Checkbox, Input, Modal, Select, Spin, message } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";
import ProjectShell from "./ProjectShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type ArtifactObjective = components["schemas"]["ArtifactObjective"];
type ArtifactKr = components["schemas"]["ArtifactKr"];
type ArtifactTask = components["schemas"]["ArtifactTask"];
type ArtifactPackage = components["schemas"]["ArtifactPackage"];
type Deliverable = components["schemas"]["Deliverable"];
type ContentState = Deliverable["contentState"];

const fmtTime = (s?: string) => (s ? s.slice(0, 16).replace("T", " ") : "");

// 内容状态 pill 的配色档位；文案一律取后端派生的 contentStateLabel，不在前端拼。
const CONTENT_STATE_CLASS: Record<ContentState, string> = {
  effective: "completed",
  updating: "review",
  reviewing: "review",
  empty: "archived",
};

const KB = 1024;
// 文件列展示「类型 · 大小」，与原型一致；大小只作展示换算。
const fmtSize = (bytes?: number) => {
  if (!bytes) return "—";
  if (bytes < KB) return `${bytes} B`;
  if (bytes < KB * KB) return `${(bytes / KB).toFixed(1)} KB`;
  return `${(bytes / KB / KB).toFixed(1)} MB`;
};

// 归档视角的一行：交付物项本身，外加它所属任务与 KR 的事实（都来自后端派生字段）。
type Row = { deliverable: Deliverable; task: ArtifactTask };
type Group = { objective: ArtifactObjective; kr: ArtifactKr; rows: Row[] };

// 成果、归档与成果包（AC-17/18）：按 KR 归集当前成果、候选状态与来源关系边；
// 勾选已生效的当前成果生成轻量成果包。
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
  const [sourceOf, setSourceOf] = useState<ArtifactPackage | null>(null);
  const [search, setSearch] = useState("");
  const [krFilter, setKrFilter] = useState<number | "all">("all");
  const [stateFilter, setStateFilter] = useState<ContentState | "all">("all");

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

  // 分组维度是 KR（AC-17）：一个 KR 一张表，组头给出 KR 负责人与交付物数量。
  const groups = useMemo<Group[]>(() => {
    const kw = search.trim().toLowerCase();
    const out: Group[] = [];
    for (const o of artifacts) {
      for (const kr of o.krs) {
        const rows: Row[] = [];
        for (const task of kr.tasks) {
          for (const deliverable of task.deliverables) {
            if (krFilter !== "all" && kr.keyResultId !== krFilter) continue;
            if (stateFilter !== "all" && deliverable.contentState !== stateFilter) continue;
            if (
              kw &&
              ![deliverable.name, task.name, task.code, task.ownerName].some((t) =>
                t.toLowerCase().includes(kw),
              )
            )
              continue;
            rows.push({ deliverable, task });
          }
        }
        if (rows.length > 0) out.push({ objective: o, kr, rows });
      }
    }
    return out;
  }, [artifacts, search, krFilter, stateFilter]);

  const krOptions = useMemo(
    () =>
      artifacts.flatMap((o) =>
        o.krs.map((kr) => ({ value: kr.keyResultId, label: `${o.code} · ${kr.code}` })),
      ),
    [artifacts],
  );

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
              <h1>成果与归档</h1>
              <p>
                按 O／KR 归集当前交付物、审核中的候选内容和来源关系；只保留当前有效内容，不提供历史版本或旧文件入口。
              </p>
            </div>
          </div>
          <div className="toolbar">
            <div className="toolbar-group">
              <Input
                allowClear
                prefix={<Icon name="search" size={15} />}
                style={{ width: 240 }}
                placeholder="搜索交付物、任务或负责人"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              <Select
                style={{ width: 170 }}
                value={krFilter}
                onChange={setKrFilter}
                options={[{ value: "all" as const, label: "全部 O / KR" }, ...krOptions]}
              />
              <Select
                style={{ width: 170 }}
                value={stateFilter}
                onChange={setStateFilter}
                options={[
                  { value: "all" as const, label: "全部内容状态" },
                  { value: "effective" as const, label: "已生效" },
                  { value: "updating" as const, label: "已生效 · 有更新审核中" },
                  { value: "reviewing" as const, label: "审核中" },
                  { value: "empty" as const, label: "未提交" },
                ]}
              />
            </div>
            <span className="muted">仅当前已生效交付物可进入成果包</span>
          </div>
          {artifacts.length === 0 ? (
            <div className="empty">尚无带交付物的任务</div>
          ) : (
            groups.length === 0 && <div className="empty">没有符合筛选条件的交付物</div>
          )}
          {/* 一个 KR 一张分组表；9 列与原型一致，候选状态与来源关系边在列表层就可见可点。 */}
          {groups.map(({ objective, kr, rows }) => (
            <section key={kr.keyResultId} className="artifact-group">
              <div className="artifact-group-head">
                <h3>
                  {kr.code} · {kr.description}
                </h3>
                <span className="muted">
                  {objective.code} · KR 负责人 {kr.ownerName || "未指定"} · {kr.deliverableCount} 项交付物
                </span>
              </div>
              <div className="data-table-wrap">
                <table className="data-table artifact-table">
                  <thead>
                    <tr>
                      {canCreate && <th style={{ width: 40 }} />}
                      <th style={{ width: 190 }}>交付物</th>
                      <th style={{ width: 190 }}>来源任务</th>
                      <th style={{ width: 100 }}>任务负责人</th>
                      <th style={{ width: 170 }}>文件</th>
                      <th style={{ width: 150 }}>内容状态</th>
                      <th style={{ width: 140 }}>接收方</th>
                      <th style={{ width: 140 }}>提交／生效时间</th>
                      <th style={{ width: 170 }}>来源关系边</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map(({ deliverable: d, task: t }) => (
                      <tr key={d.id}>
                        {canCreate && (
                          <td>
                            {/* 只有已生效的当前内容可进包（AC-18）。 */}
                            {d.current && (
                              <Checkbox checked={selected.has(d.id)} onChange={() => toggle(d.id)} />
                            )}
                          </td>
                        )}
                        <td>{d.name}</td>
                        <td>
                          <span
                            className="file-link"
                            onClick={() =>
                              navigate(`/projects/${projectId}/tasks?task=${t.taskId}`)
                            }
                          >
                            {t.code} {t.name}
                          </span>
                        </td>
                        <td>{t.ownerName}</td>
                        <td>
                          {d.current || d.candidate ? (
                            <span
                              className="file-link"
                              onClick={() => openFile((d.current ?? d.candidate)!.id)}
                            >
                              {(d.current ?? d.candidate)!.fileName}
                              <span className="muted">
                                {" "}
                                · {fmtSize((d.current ?? d.candidate)!.fileSize)}
                              </span>
                            </span>
                          ) : (
                            <span className="muted">—</span>
                          )}
                        </td>
                        <td>
                          <span className={`status-pill ${CONTENT_STATE_CLASS[d.contentState]}`}>
                            {d.contentStateLabel}
                          </span>
                        </td>
                        <td className={t.receiverLabel === "不配置" ? "muted" : ""}>
                          {t.receiverLabel}
                        </td>
                        <td className="task-date">{fmtTime(d.contentStateAt) || "—"}</td>
                        <td>
                          {d.edges.length === 0 ? (
                            <span className="muted">—</span>
                          ) : (
                            d.edges.map((e) => (
                              <div key={e.edgeId}>
                                <span
                                  className="file-link"
                                  onClick={() =>
                                    navigate(
                                      `/projects/${projectId}/collaboration?task=${e.targetTaskId}`,
                                    )
                                  }
                                >
                                  {e.name}
                                </span>
                                <div className="muted" style={{ fontSize: 12 }}>
                                  {e.edgeTypeLabel} → {e.targetTaskName}
                                </div>
                              </div>
                            ))
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          ))}
          <section className="package-section">
            <div className="page-head">
              <div>
                <h1>已形成的阶段成果包</h1>
                <p>保留成果目录与来源事实；下载时使用各项交付物的当前内容，不复制旧文件。</p>
              </div>
            </div>
            {packages.length === 0 && <div className="empty compact-empty">尚未生成成果包</div>}
            <div className="package-list">
              {packages.map((p) => (
                <article key={p.id} className="package-item">
                  <div className="package-item-head">
                    <h3>{p.name}</h3>
                    {/* 数据模型不保留成果包版本号（PRD §5.4 只引用当前内容），
                        徽章改用目录项数，与原型的版本徽章占同一位置。 */}
                    <span className="status-pill archived">{p.items.length} 项成果</span>
                  </div>
                  <div className="package-item-body">
                    <p className="muted">
                      {p.createdByName} · {fmtTime(p.createdAt)}
                    </p>
                  </div>
                  <div className="package-item-actions">
                    <Button size="small" onClick={() => setSourceOf(p)}>
                      查看来源
                    </Button>
                    <Button
                      size="small"
                      onClick={() =>
                        window.open(
                          `/api/v1/projects/${projectId}/packages/${p.id}/download`,
                          "_blank",
                        )
                      }
                    >
                      整包下载
                    </Button>
                  </div>
                </article>
              ))}
            </div>
          </section>
          {/* 固定底部选择条：勾选后才出现，与原型 selection-bar 一致。 */}
          {canCreate && selected.size > 0 && (
            <div className="selection-bar">
              <span>
                已选择 <b>{selected.size}</b> 项已生效交付物
              </span>
              <div className="selection-bar-actions">
                <Button size="small" onClick={() => setSelected(new Set())}>
                  取消
                </Button>
                <Button
                  size="small"
                  type="primary"
                  icon={<Icon name="package" size={14} />}
                  onClick={() => setPkgModal(true)}
                >
                  生成阶段成果包
                </Button>
              </div>
            </div>
          )}
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
          <Modal
            title={sourceOf ? `${sourceOf.name} · 来源清单` : ""}
            open={!!sourceOf}
            footer={null}
            onCancel={() => setSourceOf(null)}
          >
            <div className="package-item-body">
              {sourceOf?.items.map((it) => (
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
          </Modal>
        </>
      )}
    </ProjectShell>
  );
}

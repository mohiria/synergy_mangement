import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Alert,
  Button,
  Checkbox,
  Input,
  Modal,
  Select,
  Spin,
  message,
} from "antd";
import { client } from "./api/client";
import DateRangeField, { type DateRange } from "./DateRangeField";
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
type TaskFile = components["schemas"]["TaskFile"];
type ContentState = Deliverable["contentState"];
// 「文件类型」筛选维（§7.7 文件对象边界表四类；F-08／#79）。
type FileKind = "current" | "candidate" | "process" | "external";

const fmtTime = (s?: string) => (s ? s.slice(0, 16).replace("T", " ") : "");

// 内容状态 pill 的配色档位；文案一律取后端派生的 contentStateLabel，不在前端拼。
const CONTENT_STATE_CLASS: Record<ContentState, string> = {
  effective: "completed",
  updating: "review",
  reviewing: "review",
  // 待提交审核不是审核态：内容已上传但没进任何审批，配色与「未提交」同档（AC-67）。
  pending_submit: "archived",
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

// 归档视角的一行：交付物项，或任务下的过程文件／重要外部材料；
// 两种行都带所属任务与 KR 的事实（都来自后端派生字段）。
type Row =
  | { kind: "deliverable"; deliverable: Deliverable; task: ArtifactTask }
  | { kind: "taskFile"; file: TaskFile; task: ArtifactTask };
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
  // AC-18：成果包可勾选当前成果「和必要过程文件」，两类勾选分开记（后端也是两个数组）。
  const [selectedFiles, setSelectedFiles] = useState<Set<number>>(new Set());
  const [pkgModal, setPkgModal] = useState(false);
  const [pkgName, setPkgName] = useState("");
  const [saving, setSaving] = useState(false);
  const [sourceOf, setSourceOf] = useState<ArtifactPackage | null>(null);
  const [search, setSearch] = useState("");
  const [krFilter, setKrFilter] = useState<number | "all">("all");
  const [stateFilter, setStateFilter] = useState<ContentState | "all">("all");
  const [kindFilter, setKindFilter] = useState<FileKind | "all">("all");
  // 「时间」筛选维（§7.7、#86）：服务端裁剪，改动后重新拉取，不在前端过滤。
  const [range, setRange] = useState<DateRange>(null);

  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, artifactsRes, packagesRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/artifacts", {
        params: { path: { projectId } },
      }),
      client.GET("/projects/{projectId}/packages", {
        params: { path: { projectId } },
      }),
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
  }, [projectId, onLogout, range]);

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
          if (krFilter !== "all" && kr.keyResultId !== krFilter) continue;
          for (const deliverable of task.deliverables) {
            if (
              stateFilter !== "all" &&
              deliverable.contentState !== stateFilter
            )
              continue;
            // 「文件类型」维（§7.7）：交付物行按它现在有哪种内容匹配。
            if (kindFilter === "current" && !deliverable.current) continue;
            if (kindFilter === "candidate" && !deliverable.candidate) continue;
            if (kindFilter === "process" || kindFilter === "external") continue;
            if (
              kw &&
              ![deliverable.name, task.name, task.code, task.ownerName].some(
                (t) => t.toLowerCase().includes(kw),
              )
            )
              continue;
            rows.push({ kind: "deliverable", deliverable, task });
          }
          for (const file of task.files ?? []) {
            // 过程文件与外部材料没有内容状态，选了内容状态维就不参与匹配。
            if (stateFilter !== "all") continue;
            if (kindFilter === "current" || kindFilter === "candidate")
              continue;
            if (kindFilter !== "all" && file.kind !== kindFilter) continue;
            if (
              kw &&
              ![
                file.fileName,
                file.kindLabel,
                task.name,
                task.code,
                task.ownerName,
              ].some((t) => t.toLowerCase().includes(kw))
            )
              continue;
            rows.push({ kind: "taskFile", file, task });
          }
        }
        if (rows.length > 0) out.push({ objective: o, kr, rows });
      }
    }
    return out;
  }, [artifacts, search, krFilter, stateFilter, kindFilter]);

  // 交付物行与任务文件行共用同一张表；交付物行体单独成函数，免得两种行的 JSX 缠在一起。
  const renderDeliverableRow = (d: Deliverable, t: ArtifactTask) => (
    <tr key={d.id}>
      {canCreate && (
        <td>
          {/* 只有已生效的当前内容可进包（AC-18）。 */}
          {d.current && (
            <Checkbox
              checked={selected.has(d.id)}
              onChange={() => toggle(d.id)}
            />
          )}
        </td>
      )}
      <td title={d.name}>{d.name}</td>
      <td title={`${t.code} ${t.name}`}>
        <span
          className="file-link"
          onClick={() =>
            navigate(`/projects/${projectId}/tasks?task=${t.taskId}`)
          }
        >
          {t.code} {t.name}
        </span>
      </td>
      <td title={t.ownerName}>{t.ownerName}</td>
      <td title={
        d.current || d.candidate
          ? `${(d.current ?? d.candidate)!.fileName} · ${fmtSize((d.current ?? d.candidate)!.fileSize)}`
          : undefined
      }>
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
      <td className={t.receiverLabel === "不配置" ? "muted" : ""} title={t.receiverLabel}>
        {t.receiverLabel}
      </td>
      <td className="task-date">{fmtTime(d.contentStateAt) || "—"}</td>
      {/* 关系边一律排成一行（#91）：每条边仍各自可点，完整清单看 title。 */}
      <td
        title={
          d.edges.length === 0
            ? undefined
            : d.edges.map((e) => `${e.edgeTypeLabel} → ${e.targetTaskName}`).join("；")
        }
      >
        {d.edges.length === 0 ? (
          <span className="muted">—</span>
        ) : (
          d.edges.map((e, i) => (
            <span key={e.edgeId}>
              {i > 0 && "、"}
              <span
                className="file-link"
                onClick={() =>
                  navigate(
                    `/projects/${projectId}/collaboration?task=${e.targetTaskId}`,
                  )
                }
              >
                {e.targetTaskName}
              </span>
            </span>
          ))
        )}
      </td>
    </tr>
  );

  const krOptions = useMemo(
    () =>
      artifacts.flatMap((o) =>
        o.krs.map((kr) => ({
          value: kr.keyResultId,
          label: `${o.code} · ${kr.code}`,
        })),
      ),
    [artifacts],
  );

  const openFile = async (fileId: number) => {
    const res = await client.GET(
      "/projects/{projectId}/files/{fileId}/download-url",
      {
        params: { path: { projectId, fileId } },
      },
    );
    if (res.data) window.open(res.data.url, "_blank");
    else message.error(res.error?.message ?? "获取下载地址失败");
  };

  // 过程文件与外部材料的下载走它们自己的入口（与交付物文件各自一套 id）。
  const openTaskFile = async (fileId: number) => {
    const res = await client.GET("/projects/{projectId}/task-files/{fileId}/download-url", {
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

  const toggleFile = (taskFileId: number) =>
    setSelectedFiles((prev) => {
      const next = new Set(prev);
      if (next.has(taskFileId)) next.delete(taskFileId);
      else next.add(taskFileId);
      return next;
    });

  const createPackage = async () => {
    setSaving(true);
    const res = await client.POST("/projects/{projectId}/packages", {
      params: { path: { projectId } },
      body: { name: pkgName.trim(), deliverableIds: [...selected], taskFileIds: [...selectedFiles] },
    });
    setSaving(false);
    if (res.data) {
      message.success("成果包已生成");
      setPkgModal(false);
      setPkgName("");
      setSelected(new Set());
      setSelectedFiles(new Set());
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
        <Alert
          type="error"
          message="项目不存在"
          description={<Link to="/">返回项目列表</Link>}
        />
      ) : loading || !project ? (
        <Spin />
      ) : (
        <>
          <div className="page-head">
            <div>
              <h1>成果与归档</h1>
              <p>
                按 O／KR
                归集当前交付物、审核中的候选内容和来源关系；只保留当前有效内容，不提供历史版本或旧文件入口。
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
                options={[
                  { value: "all" as const, label: "全部 O / KR" },
                  ...krOptions,
                ]}
              />
              <div style={{ width: 250 }}>
                <DateRangeField
                  allowEmpty
                  value={range}
                  onChange={setRange}
                  aria-label="时间区间"
                />
              </div>
              <Select
                style={{ width: 150 }}
                value={kindFilter}
                onChange={setKindFilter}
                options={[
                  { value: "all" as const, label: "全部文件类型" },
                  { value: "current" as const, label: "当前交付物" },
                  { value: "candidate" as const, label: "候选交付物" },
                  { value: "process" as const, label: "过程文件" },
                  { value: "external" as const, label: "重要外部材料" },
                ]}
              />
              <Select
                style={{ width: 170 }}
                value={stateFilter}
                onChange={setStateFilter}
                options={[
                  { value: "all" as const, label: "全部内容状态" },
                  { value: "effective" as const, label: "已生效" },
                  {
                    value: "updating" as const,
                    label: "已生效 · 有更新审核中",
                  },
                  { value: "reviewing" as const, label: "审核中" },
                  { value: "pending_submit" as const, label: "待提交审核" },
                  { value: "empty" as const, label: "未提交" },
                ]}
              />
            </div>
            <span className="muted">当前已生效交付物与过程文件／外部材料可进入成果包</span>
          </div>
          {artifacts.length === 0 ? (
            // 时间维在服务端裁剪：筛空时返回的就是空数组，此时不能说「尚无带交付物的任务」。
            <div className="empty">
              {range?.[0] || range?.[1] ? "该时间区间内没有成果与文件" : "尚无带交付物的任务"}
            </div>
          ) : (
            groups.length === 0 && (
              <div className="empty">没有符合筛选条件的交付物</div>
            )
          )}
          {/* 一个 KR 一张分组表；9 列与原型一致，候选状态与来源关系边在列表层就可见可点。 */}
          {groups.map(({ objective, kr, rows }) => (
            <section key={kr.keyResultId} className="artifact-group">
              <div className="artifact-group-head">
                <h3>
                  {kr.code} · {kr.description}
                </h3>
                <span className="muted">
                  {objective.code} · KR 负责人 {kr.ownerName || "未指定"} ·{" "}
                  {kr.deliverableCount} 项交付物
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
                    {rows.map((row) =>
                      row.kind === "taskFile" ? (
                        // 过程文件／重要外部材料行（§7.7）：没有交付物项、没有内容状态与关系边，
                        // 「内容状态」列改说边界事实——它们不进任何审批。
                        <tr key={`f${row.file.id}`}>
                          {canCreate && (
                            <td>
                              <Checkbox
                                checked={selectedFiles.has(row.file.id)}
                                onChange={() => toggleFile(row.file.id)}
                              />
                            </td>
                          )}
                          <td className="muted" title={row.file.kindLabel}>{row.file.kindLabel}</td>
                          <td title={`${row.task.code} ${row.task.name}`}>
                            <span
                              className="file-link"
                              onClick={() =>
                                navigate(
                                  `/projects/${projectId}/tasks?task=${row.task.taskId}`,
                                )
                              }
                            >
                              {row.task.code} {row.task.name}
                            </span>
                          </td>
                          <td title={row.task.ownerName}>{row.task.ownerName}</td>
                          <td title={`${row.file.fileName} · ${fmtSize(row.file.fileSize)}`}>
                            <span
                              className="file-link"
                              onClick={() => openTaskFile(row.file.id)}
                            >
                              {row.file.fileName}
                              <span className="muted">
                                {" "}
                                · {fmtSize(row.file.fileSize)}
                              </span>
                            </span>
                          </td>
                          <td>
                            <span className="status-pill archived">
                              不进审批
                            </span>
                          </td>
                          <td
                            className={
                              row.task.receiverLabel === "不配置" ? "muted" : ""
                            }
                            title={row.task.receiverLabel}
                          >
                            {row.task.receiverLabel}
                          </td>
                          <td className="task-date">
                            {fmtTime(row.file.uploadedAt) || "—"}
                          </td>
                          <td className="muted">—</td>
                        </tr>
                      ) : (
                        renderDeliverableRow(row.deliverable, row.task)
                      ),
                    )}
                  </tbody>
                </table>
              </div>
            </section>
          ))}
          <section className="package-section">
            <div className="page-head">
              <div>
                <h1>已形成的阶段成果包</h1>
                <p>
                  保留成果目录与来源事实；下载时使用各项交付物的当前内容，不复制旧文件。
                </p>
              </div>
            </div>
            {packages.length === 0 && (
              <div className="empty compact-empty">尚未生成成果包</div>
            )}
            <div className="package-list">
              {packages.map((p) => (
                <article key={p.id} className="package-item">
                  <div className="package-item-head">
                    <h3>{p.name}</h3>
                    {/* 数据模型不保留成果包版本号（PRD §5.4 只引用当前内容），
                        徽章改用目录项数，与原型的版本徽章占同一位置。 */}
                    <span className="status-pill archived">
                      {p.items.length} 项成果
                    </span>
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
          {canCreate && selected.size + selectedFiles.size > 0 && (
            <div className="selection-bar">
              <span>
                已选择 <b>{selected.size}</b> 项已生效交付物
                {selectedFiles.size > 0 && (
                  <>
                    {" "}
                    · <b>{selectedFiles.size}</b> 份过程文件／外部材料
                  </>
                )}
              </span>
              <div className="selection-bar-actions">
                <Button
                  size="small"
                  onClick={() => {
                    setSelected(new Set());
                    setSelectedFiles(new Set());
                  }}
                >
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
              目录引用勾选的当前成果与过程文件／外部材料；交付物被覆盖后，包内对应项自动解析为新的当前内容。
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
              {sourceOf?.items.map((it, i) => (
                <div key={`${it.deliverableId ?? "f"}-${it.taskFileId ?? i}`}>
                  <span className="muted">{it.taskName} / </span>
                  {it.deliverableName}
                  {it.fileKind && (
                    <span className="muted">
                      （{it.fileKind === "external" ? "重要外部材料" : "过程文件"}）
                    </span>
                  )}
                  {it.sourceDeleted ? (
                    <span className="muted">（来源文件已删除）</span>
                  ) : it.fileName ? (
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

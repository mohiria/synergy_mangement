import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Input, Select, Spin, message } from "antd";
import { client } from "./api/client";
import DateRangeField, { type DateRange } from "./DateRangeField";
import type { components } from "./api/schema";
import Icon from "./icons";
import ProjectShell from "./ProjectShell";
import TaskDrawerHost from "./task-drawer";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type ArtifactObjective = components["schemas"]["ArtifactObjective"];
type ArtifactKr = components["schemas"]["ArtifactKr"];
type ArtifactTask = components["schemas"]["ArtifactTask"];
type Deliverable = components["schemas"]["Deliverable"];
type TaskFile = components["schemas"]["TaskFile"];
// 「文件类型」筛选维（裁决 G1，#140）：三类——交付物／过程文件／重要外部材料。
type FileKind = "deliverable" | "process" | "external";
// 「文件状态」两档（裁决 G1）：已发布（所属任务已完成）／未发布。
type FileState = "published" | "unpublished";

const fmtTime = (s?: string) => (s ? s.slice(0, 16).replace("T", " ") : "");

// 可内联预览的类型（#124）：浏览器原生能打开的 PDF／图片／纯文本；
// Office 文档内网离线无法在线预览，一律按下载处理，界面不承诺预览。
const PREVIEWABLE = new Set(["pdf", "png", "jpg", "jpeg", "gif", "webp", "txt"]);

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

  const [project, setProject] = useState<Project | null>(null);
  const [artifacts, setArtifacts] = useState<ArtifactObjective[]>([]);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [search, setSearch] = useState("");
  const [krFilter, setKrFilter] = useState<number | "all">("all");
  const [stateFilter, setStateFilter] = useState<FileState | "all">("all");
  const [kindFilter, setKindFilter] = useState<FileKind | "all">("all");
  // 「时间」筛选维（§7.7、#86）：服务端裁剪，改动后重新拉取，不在前端过滤。
  const [range, setRange] = useState<DateRange>(null);
  // #124：来源任务点编号在本页开任务抽屉，不跳全部任务。
  const [drawerTaskId, setDrawerTaskId] = useState<number | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, artifactsRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/artifacts", {
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
          // 「文件状态」两档按所属任务派生（裁决 G1），两类行同一口径。
          if (stateFilter !== "all" && task.fileState !== stateFilter) continue;
          for (const deliverable of task.deliverables) {
            // 「文件类型」三类（裁决 G1）：交付物不再分当前／候选。
            if (kindFilter === "process" || kindFilter === "external") continue;
            // #171（#17 反馈）：没有任何成果文件的交付物项不出现——不再有「—」空行；
            // 任务全部行为空时整任务自然不出现。
            if (!deliverable.current && !deliverable.candidate) continue;
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
            if (kindFilter === "deliverable") continue;
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
      {/* #124：来源任务只显编号，点击在本页开任务抽屉。 */}
      <td title={`${t.code} ${t.name}`}>
        <span className="file-link" onClick={() => setDrawerTaskId(t.taskId)}>
          {t.code}
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
            onClick={() => {
              const f = (d.current ?? d.candidate)!;
              openFile(f.id, f.fileType);
            }}
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
      {/* #171：「文件类型」列与筛选维同口径，交付物行固定「交付物」。 */}
      <td>交付物</td>
      {/* 裁决 G1（#140）：内容状态五档改「文件状态」两档（按所属任务派生）；来源关系列删除。 */}
      <td>
        <span className={`status-pill ${t.fileState === "published" ? "completed" : "archived"}`}>
          {t.fileStateLabel ?? "—"}
        </span>
      </td>
      {/* #171（#18 反馈）：接收方列只显示成员信息（服务端派生：名单／项目全体成员／未配置为空）。 */}
      <td title={t.receiverLabel || undefined}>{t.receiverLabel}</td>
      <td className="task-date">{fmtTime(d.contentStateAt) || "—"}</td>
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

  // #124：PDF／图片／纯文本在新标签内联预览（预签名带 inline），其余类型下载。
  const openFile = async (fileId: number, fileType?: string) => {
    const preview = !!fileType && PREVIEWABLE.has(fileType.toLowerCase());
    const res = await client.GET(
      "/projects/{projectId}/files/{fileId}/download-url",
      {
        params: {
          path: { projectId, fileId },
          query: preview ? { disposition: "inline" } : {},
        },
      },
    );
    if (!res.data) {
      message.error(res.error?.message ?? "获取下载地址失败");
      return;
    }
    if (preview) window.open(res.data.url, "_blank");
    else window.location.assign(res.data.url);
  };

  // 过程文件与外部材料的下载走它们自己的入口（与交付物文件各自一套 id）。
  const openTaskFile = async (fileId: number, fileType?: string) => {
    const preview = !!fileType && PREVIEWABLE.has(fileType.toLowerCase());
    const res = await client.GET("/projects/{projectId}/task-files/{fileId}/download-url", {
      params: {
        path: { projectId, fileId },
        query: preview ? { disposition: "inline" } : {},
      },
    });
    if (!res.data) {
      message.error(res.error?.message ?? "获取下载地址失败");
      return;
    }
    if (preview) window.open(res.data.url, "_blank");
    else window.location.assign(res.data.url);
  };

  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="成果归档"
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
              <h1>成果归档</h1>
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
                  { value: "deliverable" as const, label: "交付物" },
                  { value: "process" as const, label: "过程文件" },
                  { value: "external" as const, label: "重要外部材料" },
                ]}
              />
              <Select
                style={{ width: 150 }}
                value={stateFilter}
                onChange={setStateFilter}
                options={[
                  { value: "all" as const, label: "全部文件状态" },
                  { value: "published" as const, label: "已发布" },
                  { value: "unpublished" as const, label: "未发布" },
                ]}
              />
            </div>
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
                      {/* #124 去名称列；裁决 G1（#140）：无勾选列、无来源关系列，「文件状态」两档。
                          #171（#17 反馈）：列名「成果文件」＋「文件类型」列（与文件类型筛选维同口径）。 */}
                      <th style={{ width: 90 }}>来源任务</th>
                      <th style={{ width: 100 }}>任务负责人</th>
                      <th>成果文件</th>
                      <th style={{ width: 110 }}>文件类型</th>
                      <th style={{ width: 110 }}>文件状态</th>
                      <th style={{ width: 140 }}>接收方</th>
                      <th style={{ width: 140 }}>提交／生效时间</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((row) =>
                      row.kind === "taskFile" ? (
                        // 过程文件／重要外部材料行（§7.7）：没有交付物项、没有内容状态与关系边，
                        // 「内容状态」列改说边界事实——它们不进任何审批。
                        <tr key={`f${row.file.id}`}>
                          <td title={`${row.task.code} ${row.task.name}`}>
                            <span
                              className="file-link"
                              onClick={() => setDrawerTaskId(row.task.taskId)}
                            >
                              {row.task.code}
                            </span>
                          </td>
                          <td title={row.task.ownerName}>{row.task.ownerName}</td>
                          <td title={`${row.file.fileName} · ${fmtSize(row.file.fileSize)}`}>
                            <span
                              className="file-link"
                              onClick={() => openTaskFile(row.file.id, row.file.fileType)}
                            >
                              {row.file.fileName}
                              <span className="muted">
                                {" "}
                                · {fmtSize(row.file.fileSize)}
                              </span>
                            </span>
                          </td>
                          {/* #171：过程文件／重要外部材料行的「文件类型」取自身 kindLabel。 */}
                          <td>{row.file.kindLabel}</td>
                          <td>
                            <span
                              className={`status-pill ${row.task.fileState === "published" ? "completed" : "archived"}`}
                            >
                              {row.task.fileStateLabel ?? "—"}
                            </span>
                          </td>
                          <td title={row.task.receiverLabel || undefined}>
                            {row.task.receiverLabel}
                          </td>
                          <td className="task-date">
                            {fmtTime(row.file.uploadedAt) || "—"}
                          </td>
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
          {/* 裁决 G1（#140）：阶段成果包整体移除。 */}
          {/* #124：来源任务在本页打开抽屉；动作落库后刷新归档数据。 */}
          <TaskDrawerHost
            projectId={projectId}
            taskId={drawerTaskId}
            onClose={() => setDrawerTaskId(null)}
            onChanged={load}
          />
        </>
      )}
    </ProjectShell>
  );
}

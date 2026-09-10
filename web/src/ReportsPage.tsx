import { Fragment, useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Button, Spin, message } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";
import ProjectShell from "./ProjectShell";
import { STATUS_CLASS, fmtTime } from "./task-drawer/shared";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Report = components["schemas"]["Report"];
type ReportRange = components["schemas"]["ReportRange"];
type RiskLevel = components["schemas"]["RiskLevel"];
type ReportBlocker = components["schemas"]["ReportBlocker"];
type ReportResolvedBlocker = components["schemas"]["ReportResolvedBlocker"];

const RANGE_LABEL: Record<ReportRange, string> = {
  today: "今天",
  week: "近 7 天",
  month: "近 30 天",
  all: "项目整体",
};
const RISK_LABEL: Record<RiskLevel, string> = {
  normal: "正常",
  warning: "预警",
  high_risk: "高风险",
};

// 月-日短格式：报告纸内的日期都以 MM-DD 呈现，年份由眉题的完整时间给出。
const md = (s?: string) => (s ? s.slice(5, 10) : "");

const RiskPill = ({ level }: { level: RiskLevel }) => (
  <span className={`status-pill risk-${level}`}>{RISK_LABEL[level]}</span>
);

// 项目报告（AC-19；PRD §7.8）：正文为本期成果／风险与卡点／下一步三段，O／KR 进展作附录；
// 全部内容由后端从同一份项目事实实时派生，前端只负责排版。
export default function ReportsPage({
  user,
  onLogout,
}: {
  user: CurrentUser;
  onLogout: () => void;
}) {
  const { projectId: projectIdParam } = useParams();
  const projectId = Number(projectIdParam);

  const [project, setProject] = useState<Project | null>(null);
  const [report, setReport] = useState<Report | null>(null);
  const [range, setRange] = useState<ReportRange>("week");
  const [exporting, setExporting] = useState<"image" | "pdf" | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    const [projectRes, reportRes] = await Promise.all([
      client.GET("/projects/{projectId}", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/report", {
        params: { path: { projectId }, query: { range } },
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
    setReport(reportRes.data ?? null);
    setLoading(false);
  }, [projectId, range, onLogout]);

  useEffect(() => {
    load();
  }, [load]);

  // 导出先取回响应再决定去向：失败时后端回 502 JSON，直接 window.open 只会开出一个空白页。
  const exportReport = async (format: "image" | "pdf") => {
    setExporting(format);
    try {
      const res = await fetch(
        `/api/v1/projects/${projectId}/report/export?range=${range}&format=${format}`,
        { credentials: "same-origin" },
      );
      if (!res.ok) {
        let text = "导出失败，请稍后重试";
        try {
          const body = (await res.json()) as { message?: string };
          if (body.message) text = body.message;
        } catch {
          // 非 JSON 响应时保留通用文案
        }
        message.error(text);
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      window.open(url, "_blank");
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch {
      message.error("导出失败，请检查网络后重试");
    } finally {
      setExporting(null);
    }
  };

  const renderOpenBlocker = (b: ReportBlocker) => (
    <div key={`${b.taskId}-${b.kind}`} className={`rp-blocker ${b.phase === "carried" ? "carry" : ""}`}>
      {b.phase ? (
        <span className={`rp-tag ${b.phase === "new" ? "new" : "carry"}`}>{b.phaseLabel}</span>
      ) : (
        <RiskPill level={b.level} />
      )}
      <div>
        <div className="rp-b-title">
          <span className="rp-code">{b.code}</span> {b.taskName}{" "}
          <span className="rp-tag gray">{b.kindLabel}</span>
          {b.phase && <RiskPill level={b.level} />}
        </div>
        <div className="rp-b-reason">
          {b.reason} · 缺 {b.missing}
        </div>
      </div>
      <div className="rp-b-side">
        <b>{b.actionOwnerName ?? "—"}</b>
        待行动人 · 已停留 {b.stayDays} 天
      </div>
    </div>
  );

  const renderResolvedBlocker = (b: ReportResolvedBlocker) => (
    <div key={`${b.taskId}-${b.kind}-${b.resolvedAt}`} className="rp-blocker resolved">
      <span className="rp-tag resolved">本期解除</span>
      <div>
        <div className="rp-b-title">
          <span className="rp-code">{b.code}</span> {b.taskName}{" "}
          <span className="rp-tag gray">{b.kindLabel}</span>
        </div>
        <div className="rp-b-reason">缺 {b.missing}</div>
      </div>
      <div className="rp-b-side">
        <b>{md(b.resolvedAt)} 解除</b>
        出现 {md(b.openedAt)} · 持续 {b.durationDays} 天
      </div>
    </div>
  );

  return (
    <ProjectShell
      user={user}
      project={project}
      projectId={projectId}
      pageLabel="报告"
      onLogout={onLogout}
    >
      {notFound ? (
        <Alert type="error" message="项目不存在" description={<Link to="/">返回项目列表</Link>} />
      ) : loading || !report || !project ? (
        <Spin />
      ) : (
        <div className="rp-a">
          <div className="page-head">
            <div>
              <h1>项目报告</h1>
              <p>由当前项目事实生成；本期成果、卡点与下一步为主体，O／KR 进展作附录。</p>
            </div>
            <div className="report-actions">
              <Button loading={exporting === "image"} onClick={() => exportReport("image")}>
                导出长图
              </Button>
              <Button type="primary" loading={exporting === "pdf"} onClick={() => exportReport("pdf")}>
                导出 PDF
              </Button>
            </div>
          </div>
          <div className="toolbar report-toolbar">
            {/* 时间范围用基线 §6 的 segment（h36 灰底、激活项白底），不是实心蓝底按钮。 */}
            <div className="segment" role="group" aria-label="报告时间范围">
              {(Object.keys(RANGE_LABEL) as ReportRange[]).map((r) => (
                <button
                  key={r}
                  type="button"
                  aria-pressed={range === r}
                  onClick={() => setRange(r)}
                >
                  {RANGE_LABEL[r]}
                </button>
              ))}
            </div>
            <span className="rp-muted">
              {report.from ? `${md(report.from)} ~ ${md(report.generatedAt)}` : "项目整体"} · 生成于{" "}
              {fmtTime(report.generatedAt)}
            </span>
          </div>

          {/* report-sheet 版式（风格基线 §6 弹窗／报告规格）：眉题由 CSS 提供，正文分节。 */}
          <article className="report-sheet">
            <div className="report-title">
              <h2>{project.name} · 项目报告</h2>
              <p>
                {RANGE_LABEL[report.range]}
                {report.from && `（${md(report.from)} ~ ${md(report.generatedAt)}）`} · 生成时间{" "}
                {fmtTime(report.generatedAt)}
              </p>
            </div>

            <section className="report-section">
              <h3>
                一、本期成果
                <span className="rp-count">
                  {report.deliveries.completedTasks} 项任务完成 · {report.deliveries.effectiveFiles} 份交付物生效
                </span>
                <span className="rp-sub">按 O → KR → 任务归组</span>
              </h3>
              {report.deliveries.objectives.length === 0 && (
                <div className="empty compact-empty">该范围内没有完成的任务，也没有新生效的交付物</div>
              )}
              {report.deliveries.objectives.map((o) => (
                <div key={o.objectiveId} className="rp-o">
                  <div className="rp-o-head">
                    <span className="objective-code">{o.code}</span>
                    <b>{o.title}</b>
                  </div>
                  {o.keyResults.map((k) => (
                    <div key={k.keyResultId} className="rp-kr">
                      <div className="rp-kr-head">
                        <span className="kr-code">{k.code}</span>
                        <span>{k.description}</span>
                        {k.averageProgress != null && (
                          <span className="rp-muted">· {k.averageProgress}%</span>
                        )}
                      </div>
                      {k.tasks.map((t) => (
                        <div key={t.taskId} className="rp-task">
                          <div className="rp-task-name">
                            <span className="rp-code">{t.code}</span>
                            {t.name}
                          </div>
                          <div className="rp-task-meta">
                            {t.ownerName} ·
                            <span className={`status-pill ${STATUS_CLASS[t.status]}`}>{t.statusLabel}</span>
                            {t.completedAt ? md(t.completedAt) : t.progress != null ? `进度 ${t.progress}%` : ""}
                          </div>
                          {t.files.length > 0 && (
                            <div className="rp-files">
                              {t.files.map((f) => (
                                <span key={`${f.fileName}-${f.effectiveAt}`} className="rp-file" title={f.deliverableName}>
                                  <Icon name="archive" size={14} />
                                  {f.fileName}
                                  <small>生效 {md(f.effectiveAt)}</small>
                                </span>
                              ))}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  ))}
                </div>
              ))}
            </section>

            <section className="report-section">
              <h3>
                二、风险与卡点
                <span className="rp-count">
                  {report.blockers.open.length} 项开放
                  {report.from &&
                    ` · 本期新增 ${report.blockers.newInRange} · 本期解除 ${report.blockers.resolvedInRange}`}
                </span>
                {report.blockers.pendingCompletions > 0 && (
                  <span className="rp-sub">
                    完成审核 {report.blockers.pendingCompletions} 件仍停留在审批队列
                  </span>
                )}
              </h3>
              {report.blockers.open.length === 0 ? (
                <div className="empty compact-empty">当前没有开放的卡点</div>
              ) : (
                <div className="rp-blockers">{report.blockers.open.map(renderOpenBlocker)}</div>
              )}
              {report.blockers.resolved.length > 0 && (
                <>
                  <div className="rp-resolved-head">本期已解除</div>
                  <div className="rp-blockers">{report.blockers.resolved.map(renderResolvedBlocker)}</div>
                </>
              )}
            </section>

            <section className="report-section rp-next">
              <h3>
                三、下一步
                <span className="rp-count">
                  {report.nextSteps.due.length} 项到期／超期 · {report.nextSteps.upcoming.length} 项即将启动
                </span>
                <span className="rp-sub">未来 {report.nextSteps.horizonDays} 天</span>
              </h3>
              {report.nextSteps.due.length === 0 ? (
                <div className="empty compact-empty">
                  未来 {report.nextSteps.horizonDays} 天内没有到期的任务，也没有已超期的任务
                </div>
              ) : (
                <table>
                  <thead>
                    <tr>
                      <th>编号</th>
                      <th>任务</th>
                      <th>负责人</th>
                      <th>状态</th>
                      <th>截止</th>
                    </tr>
                  </thead>
                  <tbody>
                    {report.nextSteps.due.map((n) => (
                      <tr key={n.taskId} className={n.overdueDays != null ? "overdue" : ""}>
                        <td>
                          <span className="rp-code">{n.code}</span>
                        </td>
                        <td>
                          {n.taskName}
                          {n.keyResultCode && (
                            <div className="rp-muted">
                              {n.keyResultCode} · {n.keyResultDescription}
                            </div>
                          )}
                        </td>
                        <td>{n.ownerName}</td>
                        <td>
                          <span className={`status-pill ${STATUS_CLASS[n.status]}`}>{n.statusLabel}</span>
                          {n.unreadyNote && <div className="rp-muted">{n.unreadyNote}</div>}
                        </td>
                        <td className={`rp-due ${n.overdueDays != null ? "overdue" : ""}`}>
                          {md(n.endDate)}
                          {n.overdueDays != null
                            ? `（超期 ${n.overdueDays} 天）`
                            : n.dueInDays === 0
                              ? "（今天）"
                              : `（${n.dueInDays} 天后）`}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
              {report.nextSteps.upcoming.length > 0 && (
                <>
                  <div className="rp-next-sub">即将启动</div>
                  <table>
                    <tbody>
                      {report.nextSteps.upcoming.map((u) => (
                        <tr key={u.taskId}>
                          <td>
                            <span className="rp-code">{u.code}</span>
                          </td>
                          <td>
                            {u.taskName}
                            {u.keyResultCode && (
                              <div className="rp-muted">
                                {u.keyResultCode} · {u.keyResultDescription}
                              </div>
                            )}
                          </td>
                          <td>{u.ownerName}</td>
                          <td>
                            <span className={`status-pill ${STATUS_CLASS[u.status]}`}>{u.statusLabel}</span>
                            {u.unreadyNote && <div className="rp-muted">{u.unreadyNote}</div>}
                          </td>
                          <td className="rp-due">计划 {md(u.startDate)} 启动</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </>
              )}
            </section>

            <section className="report-section">
              <h3>附、O／KR 进展</h3>
              {report.okrProgress.length === 0 && (
                <div className="empty compact-empty">项目还没有 O／KR</div>
              )}
              {report.okrProgress.length > 0 && (
                <table className="rp-okr-compact">
                  <tbody>
                    {report.okrProgress.map((o) => (
                      <Fragment key={o.objectiveId}>
                        <tr className="o">
                          <td colSpan={4}>
                            {o.code} {o.title}
                          </td>
                        </tr>
                        {o.keyResults.map((k) => (
                          <tr key={k.keyResultId}>
                            <td>
                              <span className="kr-code">{k.code}</span> {k.description}{" "}
                              <RiskPill level={k.riskLevel} />
                              <span className="rp-muted">
                                · {k.filledTasks}／{k.totalTasks} 已填
                                {report.from ? ` · 本期完成 ${k.completedInRange} 项` : ` · 已完成 ${k.completedInRange} 项`}
                              </span>
                            </td>
                            <td className="rp-bar">
                              <div className="progress">
                                <i style={{ width: `${k.averageProgress ?? 0}%` }} />
                              </div>
                            </td>
                            <td className="rp-pct">
                              {k.averageProgress != null ? `${k.averageProgress}%` : "未填"}
                            </td>
                          </tr>
                        ))}
                      </Fragment>
                    ))}
                  </tbody>
                </table>
              )}
            </section>
          </article>
        </div>
      )}
    </ProjectShell>
  );
}

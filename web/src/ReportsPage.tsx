import { Fragment, useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Button, Spin, message } from "antd";
import dayjs from "dayjs";
import utc from "dayjs/plugin/utc";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";
import { STATUS_CLASS } from "./task-drawer/shared";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Report = components["schemas"]["Report"];
type ReportRange = components["schemas"]["ReportRange"];
type RiskLevel = components["schemas"]["RiskLevel"];
type ReportBlocker = components["schemas"]["ReportBlocker"];

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
// md 只用于纯日期（YYYY-MM-DD，无时区）；带时区的时间戳一律换算到项目时区（UTC+8）再格式化：
// 直接截 UTC 串会把晚间的时刻显示成前一天，按浏览器本地时区又会让异地查看者与服务端「当日」口径不一致。
dayjs.extend(utc);
const PROJECT_UTC_OFFSET = 8;
const md = (s?: string) => (s ? s.slice(5, 10) : "");
const mdTime = (s?: string) => (s ? dayjs(s).utcOffset(PROJECT_UTC_OFFSET).format("MM-DD") : "");
const fmtTime = (s?: string) => (s ? dayjs(s).utcOffset(PROJECT_UTC_OFFSET).format("YYYY-MM-DD HH:mm") : "");

const RiskPill = ({ level }: { level: RiskLevel }) => (
  <span className={`status-pill risk-${level}`}>{RISK_LABEL[level]}</span>
);

// 报告纸内统一的四列表格：任务／负责人／状态／日期，固定列宽让表头永不换行；
// 状态列放得下最长的显示状态（「待××等 N 人审批」），日期列只放日期本身，超期／今天另起一行。
const ReportTableCols = () => (
  <colgroup>
    <col />
    <col style={{ width: 84 }} />
    <col style={{ width: 112 }} />
    <col style={{ width: 96 }} />
  </colgroup>
);

// 到期／超期表的日期格：日期与「超期 N 天」／「今天」分两行，互不挤占，导出成长图时也不会溢出列宽。
const DueDate = ({
  endDate,
  overdueDays,
  dueInDays,
}: {
  endDate: string;
  overdueDays?: number;
  dueInDays?: number;
}) => (
  <>
    <div>{md(endDate)}</div>
    {overdueDays != null ? (
      <div className="rp-red">超期 {overdueDays} 天</div>
    ) : dueInDays === 0 ? (
      <div className="rp-today">今天</div>
    ) : null}
  </>
);

// 项目报告（AC-19；PRD §7.8）：正文为本期成果／风险与卡点／下一步三段，O／KR 进展作附录；
// 全部内容由后端从同一份项目事实实时派生，前端只负责排版。面向领导阅读，只保留结论级字段。
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

  // 卡点行：任务在首列（不加粗，只是定位），卡点本身加粗是重点；阶段标签（本期新出现／上期遗留）跟在卡点之后，
  // 项目整体范围不分阶段时改放风险等级。
  const renderOpenBlocker = (b: ReportBlocker) => (
    <tr key={b.key} className={b.level === "high_risk" ? "overdue" : ""}>
      <td className="c-task">
        <span className="rp-code">{b.code}</span> {b.taskName}
      </td>
      <td>
        <div className="rp-blk-kind">
          <b>{b.kindLabel}</b>
          {b.phase ? (
            <span className={`rp-tag ${b.phase === "new" ? "new" : "carry"}`}>{b.phaseLabel}</span>
          ) : (
            <RiskPill level={b.level} />
          )}
        </div>
      </td>
      <td>{b.actionOwnerName ?? "—"}</td>
      <td className="c-date">
        <b>{b.stayDays}</b> 天
      </td>
    </tr>
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
              {report.from ? `${mdTime(report.from)} ~ ${mdTime(report.generatedAt)}` : "项目整体"} · 生成于{" "}
              {fmtTime(report.generatedAt)}
            </span>
          </div>

          {/* report-sheet 版式（风格基线 §6 弹窗／报告规格）：眉题由 CSS 提供，正文分节。 */}
          <article className="report-sheet">
            <div className="report-title">
              <h2>{project.name} · 项目报告</h2>
              <p>
                {RANGE_LABEL[report.range]}
                {report.from && `（${mdTime(report.from)} ~ ${mdTime(report.generatedAt)}）`} · 生成时间{" "}
                {fmtTime(report.generatedAt)}
              </p>
            </div>

            {/* 摘要条：四个数字先给结论，读者不必往下翻就知道本期状况。 */}
            <div className="rp-summary">
              <div>
                <small>本期成果</small>
                <b>{report.deliveries.completedTasks} 项</b>
                <span>任务完成 · {report.deliveries.effectiveFiles} 份交付物生效</span>
              </div>
              <div>
                <small>开放卡点</small>
                <b className={report.blockers.open.length > 0 ? "warn" : ""}>{report.blockers.open.length} 项</b>
                <span>
                  {report.from
                    ? `本期新增 ${report.blockers.newInRange} · 解除 ${report.blockers.resolvedInRange}`
                    : "项目整体累计"}
                </span>
              </div>
              <div>
                <small>到期／超期</small>
                <b className={report.nextSteps.due.some((n) => n.overdueDays != null) ? "warn" : ""}>
                  {report.nextSteps.due.length} 项
                </b>
                <span>其中超期 {report.nextSteps.due.filter((n) => n.overdueDays != null).length} 项</span>
              </div>
              <div>
                <small>即将启动</small>
                <b>{report.nextSteps.upcoming.length} 项</b>
                <span>未来 {report.nextSteps.horizonDays} 天内</span>
              </div>
            </div>

            <section className="report-section">
              <h3>一、本期成果</h3>
              {report.deliveries.objectives.length === 0 ? (
                <div className="empty compact-empty">该范围内没有完成的任务，也没有新生效的交付物</div>
              ) : (
                /* 交付物是本节的核心：放首列、逐份成行加粗；任务只作简写的出处，负责人与状态／日期居后。 */
                <table className="rp-table rp-deliv">
                  <colgroup>
                    <col />
                    <col style={{ width: 200 }} />
                    <col style={{ width: 84 }} />
                    <col style={{ width: 112 }} />
                  </colgroup>
                  <thead>
                    <tr>
                      <th>交付物</th>
                      <th>任务</th>
                      <th>负责人</th>
                      <th>状态／日期</th>
                    </tr>
                  </thead>
                  <tbody>
                    {report.deliveries.objectives.map((o) => (
                      <Fragment key={o.objectiveId}>
                        <tr className="group">
                          <td colSpan={4}>
                            <span className="objective-code">{o.code}</span> {o.title}
                          </td>
                        </tr>
                        {o.keyResults.flatMap((k) =>
                          k.tasks.map((t) => (
                            <tr key={t.taskId}>
                              <td className="c-files">
                                {t.files.length > 0 ? (
                                  t.files.map((f) => <div key={f.fileName}>{f.fileName}</div>)
                                ) : (
                                  <span className="rp-muted">无交付物文件</span>
                                )}
                              </td>
                              <td className="c-task rp-brief">
                                <span className="rp-code">{t.code}</span> {t.name}
                              </td>
                              <td>{t.ownerName}</td>
                              <td className="c-date">
                                <span className={`status-pill ${STATUS_CLASS[t.status]}`}>{t.statusLabel}</span>
                                <div className="rp-muted">
                                  {t.completedAt ? mdTime(t.completedAt) : t.progress != null ? `进度 ${t.progress}%` : "—"}
                                </div>
                              </td>
                            </tr>
                          )),
                        )}
                      </Fragment>
                    ))}
                  </tbody>
                </table>
              )}
            </section>

            <section className="report-section">
              <h3>
                二、风险与卡点
                {report.blockers.pendingCompletions > 0 && (
                  <span className="rp-sub">另有 {report.blockers.pendingCompletions} 件完成审核仍在审批队列</span>
                )}
              </h3>
              {report.blockers.open.length === 0 ? (
                <div className="empty compact-empty">当前没有开放的卡点</div>
              ) : (
                <table className="rp-table">
                  <colgroup>
                    <col />
                    <col style={{ width: 188 }} />
                    <col style={{ width: 112 }} />
                    <col style={{ width: 72 }} />
                  </colgroup>
                  <thead>
                    <tr>
                      <th>任务</th>
                      <th>卡点</th>
                      <th>待行动</th>
                      <th>已停留</th>
                    </tr>
                  </thead>
                  <tbody>{report.blockers.open.map(renderOpenBlocker)}</tbody>
                </table>
              )}
              {report.blockers.resolved.length > 0 && (
                <div className="rp-blk-resolved">
                  <b>本期解除 {report.blockers.resolved.length} 项</b>{" "}
                  {report.blockers.resolved.map((b, i) => (
                    <Fragment key={`${b.taskId}-${b.key}-${b.resolvedAt}`}>
                      {i > 0 && "；"}
                      <span className="rp-code">{b.code}</span> {b.taskName}（{b.kindLabel}，{mdTime(b.resolvedAt)} 解除）
                    </Fragment>
                  ))}
                </div>
              )}
            </section>

            <section className="report-section">
              <h3>
                三、下一步<span className="rp-sub">未来 {report.nextSteps.horizonDays} 天</span>
              </h3>
              <div className="rp-sub-title">到期／超期</div>
              {report.nextSteps.due.length === 0 ? (
                <div className="empty compact-empty">
                  未来 {report.nextSteps.horizonDays} 天内没有到期的任务，也没有已超期的任务
                </div>
              ) : (
                <table className="rp-table">
                  <ReportTableCols />
                  <thead>
                    <tr>
                      <th>任务</th>
                      <th>负责人</th>
                      <th>状态</th>
                      <th>截止日期</th>
                    </tr>
                  </thead>
                  <tbody>
                    {report.nextSteps.due.map((n) => (
                      <tr key={n.taskId} className={n.overdueDays != null ? "overdue" : ""}>
                        <td className="c-task">
                          <span className="rp-code">{n.code}</span> {n.taskName}
                        </td>
                        <td>{n.ownerName}</td>
                        <td>
                          <span className={`status-pill ${STATUS_CLASS[n.status]}`}>{n.statusLabel}</span>
                        </td>
                        <td className="c-date">
                          <DueDate endDate={n.endDate} overdueDays={n.overdueDays} dueInDays={n.dueInDays} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
              {report.nextSteps.upcoming.length > 0 && (
                <>
                  <div className="rp-sub-title">即将启动</div>
                  <table className="rp-table">
                    <ReportTableCols />
                    <thead>
                      <tr>
                        <th>任务</th>
                        <th>负责人</th>
                        <th>状态</th>
                        <th>计划启动</th>
                      </tr>
                    </thead>
                    <tbody>
                      {report.nextSteps.upcoming.map((u) => (
                        <tr key={u.taskId}>
                          <td className="c-task">
                            <span className="rp-code">{u.code}</span> {u.taskName}
                          </td>
                          <td>{u.ownerName}</td>
                          <td>
                            <span className={`status-pill ${STATUS_CLASS[u.status]}`}>{u.statusLabel}</span>
                          </td>
                          <td className="c-date">{md(u.startDate)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </>
              )}
            </section>

            <section className="report-section">
              <h3>附、O／KR 进展</h3>
              {report.okrProgress.length === 0 ? (
                <div className="empty compact-empty">项目还没有 O／KR</div>
              ) : (
                <table className="rp-table rp-okr">
                  <colgroup>
                    <col />
                    <col style={{ width: 76 }} />
                    <col style={{ width: 160 }} />
                    <col style={{ width: 52 }} />
                  </colgroup>
                  <tbody>
                    {report.okrProgress.map((o) => (
                      <Fragment key={o.objectiveId}>
                        <tr className="group">
                          <td colSpan={4}>
                            <span className="objective-code">{o.code}</span> {o.title}
                          </td>
                        </tr>
                        {o.keyResults.map((k) => (
                          <tr key={k.keyResultId}>
                            <td>
                              <span className="kr-code">{k.code}</span> {k.description}
                            </td>
                            <td>
                              <RiskPill level={k.riskLevel} />
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

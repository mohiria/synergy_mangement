import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Button, Spin, message } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import ProjectShell from "./ProjectShell";

type CurrentUser = components["schemas"]["CurrentUser"];
type Project = components["schemas"]["Project"];
type Report = components["schemas"]["Report"];
type ReportRange = components["schemas"]["ReportRange"];
type RiskLevel = components["schemas"]["RiskLevel"];

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

const fmtTime = (s?: string) => (s ? s.slice(0, 16).replace("T", " ") : "");

// 项目报告（AC-19）：从同一份项目事实生成；时间范围直接点击切换。
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
        <>
          <div className="page-head">
            <div>
              <h1>项目报告</h1>
              <p>由项目事实自动生成，不要求成员重复填报</p>
            </div>
          </div>
          <div className="toolbar report-toolbar">
            <div className="toolbar-group">
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
              <Button size="small" loading={exporting === "image"} onClick={() => exportReport("image")}>
                导出长图
              </Button>
              <Button size="small" loading={exporting === "pdf"} onClick={() => exportReport("pdf")}>
                导出 PDF
              </Button>
            </div>
          </div>

          {/* report-sheet 版式（风格基线 §6 弹窗／报告规格）：眉题由 CSS 提供，正文分节。 */}
          <div className="report-sheet">
            <div className="report-title">
              <h2>
                {project.name} · {RANGE_LABEL[report.range]}
              </h2>
              <p>生成于 {fmtTime(report.generatedAt)}</p>
            </div>

          <section className="report-section">
            <h3>O／KR 进展</h3>
            <div className="data-table-wrap">
              <table className="data-table" style={{ minWidth: 0 }}>
                <thead>
                  <tr>
                    <th>KR</th>
                    <th style={{ width: 90 }}>风险</th>
                    <th style={{ width: 180 }}>进度覆盖度</th>
                    <th style={{ width: 140 }}>范围内终审通过</th>
                  </tr>
                </thead>
                <tbody>
                  {report.krProgress.map((k) => (
                    <tr key={k.keyResultId}>
                      <td title={k.description}>{k.description}</td>
                      <td>
                        <span className={`status-pill risk-${k.riskLevel}`}>
                          {RISK_LABEL[k.riskLevel]}
                        </span>
                      </td>
                      <td>
                        {k.filledTasks}／{k.totalTasks} 由负责人填写
                        {k.averageProgress != null && `，平均 ${k.averageProgress}%`}
                      </td>
                      <td>{k.completedInRange} 项</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>

          <section className="report-section">
            <h3>完成成果（范围内生效）</h3>
            {report.completedDeliverables.length === 0 && (
              <div className="empty compact-empty">该范围内没有新生效的当前成果</div>
            )}
            {report.completedDeliverables.map((d, i) => (
              <div key={i} className="fact-card fact-card-aux">
                <div>
                  <b>
                    {d.taskName} / {d.deliverableName}
                  </b>
                  <small>
                    {d.fileName}
                    {d.effectiveAt ? ` · 生效于 ${fmtTime(d.effectiveAt)}` : ""}
                  </small>
                </div>
              </div>
            ))}
          </section>

          <section className="report-section">
            <h3>风险与卡点</h3>
            {report.blockers.length === 0 && (
              <div className="empty compact-empty">没有需要关注的卡点</div>
            )}
            {report.blockers.map((b, i) => (
              <div key={i} className="fact-card fact-card-aux">
                <div>
                  <b>
                    {b.taskName} · {b.kindLabel}：缺 {b.missing}
                    <span className={`status-pill risk-${b.level}`} style={{ marginLeft: 8 }}>
                      {RISK_LABEL[b.level]}
                    </span>
                  </b>
                  <small>
                    {b.reason}
                    {b.actionOwnerName ? ` · 待行动人 ${b.actionOwnerName}` : ""}
                  </small>
                </div>
              </div>
            ))}
          </section>

          <section className="report-section">
            <h3>待决策</h3>
            <div className="notice">
              关键字段修改 {report.pendingApprovals.fieldChanges} 件 · 完成审核{" "}
              {report.pendingApprovals.completions} 件仍停留在审批队列。
            </div>
          </section>

          <section className="report-section">
            <h3>下一步（临近截止／已超期）</h3>
            {report.nextSteps.length === 0 && (
              <div className="empty compact-empty">未来 7 天内没有临近截止的任务</div>
            )}
            {report.nextSteps.map((n, i) => (
              <div key={i} className="fact-card fact-card-aux">
                <div>
                  <b>
                    {n.taskName}
                    {n.overdue && (
                      <span className="status-pill risk-high_risk" style={{ marginLeft: 8 }}>
                        已超期
                      </span>
                    )}
                  </b>
                  <small>
                    {n.ownerName} · {n.statusLabel}
                    {n.endDate ? ` · 截止 ${n.endDate}` : ""}
                    {n.unreadyNote ? ` · ${n.unreadyNote}` : ""}
                  </small>
                </div>
              </div>
            ))}
          </section>
          </div>
        </>
      )}
    </ProjectShell>
  );
}

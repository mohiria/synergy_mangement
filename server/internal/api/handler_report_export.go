package api

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 报告导出（AC-20）：服务端渲染 HTML，经 Gotenberg 转正式 PDF 或移动端长图（ADR 0001）。

func gotenbergURL() string {
	if v := os.Getenv("GOTENBERG_URL"); v != "" {
		return v
	}
	return "http://localhost:3000"
}

func (s *Server) ExportReport(w http.ResponseWriter, r *http.Request, projectId int64, params ExportReportParams) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	rangeName := "all"
	if params.Range != nil {
		rangeName = string(*params.Range)
	}
	report, ok := s.buildReport(w, r, projectId, rangeName)
	if !ok {
		return
	}
	html, err := renderReportHTML(proj, report)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	// Gotenberg：PDF 走 convert/html，长图走 screenshot/html（宽 480、整页）。
	var endpoint string
	fields := map[string]string{}
	switch params.Format {
	case "pdf":
		endpoint = "/forms/chromium/convert/html"
		fields["paperWidth"] = "8.27"
		fields["paperHeight"] = "11.7"
		fields["marginTop"] = "0.4"
		fields["marginBottom"] = "0.4"
		fields["marginLeft"] = "0.4"
		fields["marginRight"] = "0.4"
	case "image":
		endpoint = "/forms/chromium/screenshot/html"
		fields["width"] = "480"
		fields["format"] = "png"
	default:
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_format", Message: "导出格式只支持 pdf 或 image"})
		return
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("files", "index.html")
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if _, err := part.Write([]byte(html)); err != nil {
		writeInternalError(w, r, err)
		return
	}
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.Close()

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, gotenbergURL()+endpoint, &body)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// 原因只进日志：错误串里带的是内部服务地址，不能回给用户。
		log.Printf("[export] request_id=%s 渲染服务不可达: %v", requestIDFrom(r.Context()), err)
		writeJSON(w, http.StatusBadGateway, Error{Code: "render_unavailable", Message: "渲染服务暂时不可用，请稍后重试"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		log.Printf("[export] request_id=%s 渲染失败 status=%d body=%s", requestIDFrom(r.Context()), resp.StatusCode, string(msg))
		writeJSON(w, http.StatusBadGateway, Error{Code: "render_failed", Message: "报告渲染失败，请稍后重试"})
		return
	}
	filename := fmt.Sprintf("%s-报告", proj.Name)
	if params.Format == "pdf" {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename+".pdf"))
	} else {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename+".png"))
	}
	_, _ = io.Copy(w, resp.Body)
}

// 模板结构与前端报告页一致（PRD §7.8）：正文三段 本期成果／风险与卡点／下一步，O／KR 进展作附录。
const reportTemplateText = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<style>
body { font-family: "PingFang SC", "Microsoft YaHei", sans-serif; color: #1f2937; margin: 24px; font-size: 14px; line-height: 1.5; }
h1 { font-size: 20px; margin: 0 0 4px; }
.meta { color: #6b778c; font-size: 12px; margin-bottom: 18px; }
h2 { font-size: 15px; margin: 22px 0 8px; border-left: 3px solid #5267df; padding-left: 8px; display: flex; align-items: center; gap: 10px; }
.count { font-size: 12px; font-weight: 600; color: #6b7588; background: #eef1f5; border-radius: 4px; padding: 2px 8px; }
.sub { margin-left: auto; font-size: 12px; font-weight: 500; color: #6b7588; }
.note { color: #6b7588; font-size: 12px; margin: 0 0 8px 12px; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { border: 1px solid #dfe5ec; padding: 6px 8px; text-align: left; vertical-align: top; }
th { background: #f7f9fb; color: #657184; font-size: 12px; }
.code { font-family: Menlo, Consolas, monospace; font-size: 12px; color: #42526b; background: #eef1f5; border-radius: 4px; padding: 1px 6px; white-space: nowrap; }
.tag { display: inline-block; border-radius: 4px; padding: 1px 7px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.tag.new { background: #fdecef; color: #c83f50; }
.tag.carried { background: #fff3da; color: #ad6e13; }
.tag.resolved { background: #e9f5ef; color: #247a5a; }
.tag.gray { background: #eef1f5; color: #677287; font-weight: 500; }
.pill { display: inline-block; border-radius: 4px; padding: 1px 6px; font-size: 12px; }
.warning { background: #fff6df; color: #a66a12; }
.high_risk { background: #fff0f1; color: #c44752; }
.normal { background: #ebf7f0; color: #377d5b; }
.empty { color: #6b778c; font-size: 12px; margin-left: 12px; }
.muted { color: #6b7588; font-size: 12px; }
.o { margin: 8px 0 14px; }
.o-head { display: flex; align-items: center; gap: 10px; margin-bottom: 6px; }
.o-head b { font-size: 14px; color: #2c3950; }
.kr { margin: 0 0 10px 24px; border-left: 2px solid #e2e6ed; padding-left: 12px; }
.kr-head { font-size: 13px; color: #42526b; margin-bottom: 6px; }
.task { border: 1px solid #e5e8ee; border-radius: 4px; background: #fbfcfd; padding: 6px 10px; margin-bottom: 6px; }
.task-name { font-weight: 600; }
.task-meta { color: #6b7588; font-size: 12px; margin-top: 2px; }
.files { margin-top: 4px; font-size: 13px; color: #42526b; }
.blocker { border: 1px solid #e5e8ee; border-left: 3px solid #c83f50; border-radius: 4px; padding: 8px 12px; margin-bottom: 6px; }
.blocker.carried { border-left-color: #ad6e13; }
.blocker.resolved { border-left-color: #247a5a; background: #fafcfb; }
.b-title { font-weight: 600; }
.b-reason { color: #3f4b5f; margin-top: 2px; }
.b-side { color: #6b7588; font-size: 12px; margin-top: 2px; }
.section-sub { margin: 14px 0 6px 12px; font-weight: 650; font-size: 13px; }
.section-sub.resolved { color: #247a5a; }
.section-sub.upcoming { color: #7259b7; }
tr.overdue td:first-child { box-shadow: inset 3px 0 #c83f50; }
.due.overdue { color: #c83f50; font-weight: 600; }
.bar { display: inline-block; width: 80px; height: 6px; background: #eef1f5; border-radius: 3px; vertical-align: middle; margin-right: 6px; }
.bar i { display: block; height: 100%; background: #5267df; border-radius: 3px; }
</style></head><body>
<h1>{{.ProjectName}} · 项目报告</h1>
<div class="meta">{{.RangeLabel}}{{if .From}}（{{.From}} ~ {{.GeneratedAt}}）{{end}} · 生成时间 {{.GeneratedAt}}</div>

<h2>一、本期成果 <span class="count">{{.Report.Deliveries.CompletedTasks}} 项任务完成 · {{.Report.Deliveries.EffectiveFiles}} 份交付物生效</span><span class="sub">按 O → KR → 任务归组</span></h2>
{{if not .Report.Deliveries.Objectives}}<div class="empty">该范围内没有终审通过的任务，也没有新生效的交付内容</div>{{end}}
{{range .Report.Deliveries.Objectives}}<div class="o"><div class="o-head"><span class="code">{{.Code}}</span><b>{{.Title}}</b></div>
{{range .KeyResults}}<div class="kr"><div class="kr-head"><span class="code">{{.Code}}</span> {{.Description}}{{if .AverageProgress}} <span class="muted">· {{.AverageProgress}}%</span>{{end}}</div>
{{range .Tasks}}<div class="task"><div class="task-name"><span class="code">{{.Code}}</span> {{.Name}}</div>
<div class="task-meta">{{.OwnerName}} · {{.StatusLabel}}{{if .CompletedAt}} · 完成于 {{datetime .CompletedAt}}{{else if .Progress}} · 进度 {{.Progress}}%{{end}}</div>
{{if .Files}}<div class="files">{{range .Files}}<div>{{.FileName}} <span class="muted">{{.DeliverableName}} · 生效 {{datetime .EffectiveAt}}</span></div>{{end}}</div>{{end}}
</div>{{end}}</div>{{end}}</div>{{end}}

<h2>二、风险与卡点 <span class="count">{{len .Report.Blockers.Open}} 项开放{{if .From}} · 本期新增 {{.Report.Blockers.NewInRange}} · 本期解除 {{.Report.Blockers.ResolvedInRange}}{{end}}</span>{{if .Report.Blockers.PendingCompletions}}<span class="sub">完成审核 {{.Report.Blockers.PendingCompletions}} 件仍停留在审批队列</span>{{end}}</h2>
{{if not .Report.Blockers.Open}}<div class="empty">当前没有开放的卡点</div>{{end}}
{{range .Report.Blockers.Open}}<div class="blocker{{if .Phase}} {{.Phase}}{{end}}">
<div class="b-title">{{if .PhaseLabel}}<span class="tag {{.Phase}}">{{.PhaseLabel}}</span> {{end}}<span class="code">{{.Code}}</span> {{.TaskName}} <span class="tag gray">{{.KindLabel}}</span> <span class="pill {{.Level}}">{{riskLabel .Level}}</span></div>
<div class="b-reason">缺 {{.Missing}}{{if .Reason}} · {{.Reason}}{{end}}</div>
<div class="b-side">{{if .ActionOwnerName}}待行动人 {{.ActionOwnerName}} · {{end}}已停留 {{.StayDays}} 天</div>
</div>{{end}}
{{if .Report.Blockers.Resolved}}<div class="section-sub resolved">本期已解除</div>
{{range .Report.Blockers.Resolved}}<div class="blocker resolved">
<div class="b-title"><span class="tag resolved">本期解除</span> <span class="code">{{.Code}}</span> {{.TaskName}} <span class="tag gray">{{.KindLabel}}</span></div>
{{if .Missing}}<div class="b-reason">缺 {{.Missing}}</div>{{end}}
<div class="b-side">{{datetime .OpenedAt}} 出现 · {{datetime .ResolvedAt}} 解除 · 持续 {{.DurationDays}} 天</div>
</div>{{end}}{{end}}

<h2>三、下一步 <span class="count">{{len .Report.NextSteps.Due}} 项到期／超期 · {{len .Report.NextSteps.Upcoming}} 项即将启动</span><span class="sub">未来 {{.Report.NextSteps.HorizonDays}} 天</span></h2>
{{if not .Report.NextSteps.Due}}<div class="empty">窗口内没有到期或超期的任务</div>{{else}}
<table><tr><th>编号</th><th>任务</th><th>负责人</th><th>状态</th><th>截止</th></tr>
{{range .Report.NextSteps.Due}}<tr{{if .OverdueDays}} class="overdue"{{end}}><td><span class="code">{{.Code}}</span></td>
<td>{{.TaskName}}{{if .KeyResultCode}}<div class="muted">{{.KeyResultCode}} · {{.KeyResultDescription}}</div>{{end}}</td>
<td>{{.OwnerName}}</td><td>{{.StatusLabel}}{{if .UnreadyNote}}<div class="muted">{{.UnreadyNote}}</div>{{end}}</td>
<td class="due{{if .OverdueDays}} overdue{{end}}">{{date .EndDate}}{{if .OverdueDays}}（超期 {{.OverdueDays}} 天）{{else if .DueInDays}}（{{.DueInDays}} 天后）{{else}}（今天）{{end}}</td></tr>{{end}}
</table>{{end}}
{{if .Report.NextSteps.Upcoming}}<div class="section-sub upcoming">即将启动</div>
<table><tr><th>编号</th><th>任务</th><th>负责人</th><th>状态</th><th>计划启动</th></tr>
{{range .Report.NextSteps.Upcoming}}<tr><td><span class="code">{{.Code}}</span></td>
<td>{{.TaskName}}{{if .KeyResultCode}}<div class="muted">{{.KeyResultCode}} · {{.KeyResultDescription}}</div>{{end}}</td>
<td>{{.OwnerName}}</td><td>{{.StatusLabel}}{{if .UnreadyNote}}<div class="muted">{{.UnreadyNote}}</div>{{end}}</td>
<td>{{date .StartDate}}</td></tr>{{end}}
</table>{{end}}

<h2>附、O／KR 进展</h2>
{{if not .Report.OkrProgress}}<div class="empty">尚无 O／KR</div>{{else}}
<table><tr><th>编号</th><th>KR</th><th>风险</th><th>进度</th><th>本期完成</th></tr>
{{range .Report.OkrProgress}}<tr><td><span class="code">{{.Code}}</span></td><td colspan="4"><b>{{.Title}}</b></td></tr>
{{range .KeyResults}}<tr><td><span class="code">{{.Code}}</span></td><td>{{.Description}}</td><td><span class="pill {{.RiskLevel}}">{{riskLabel .RiskLevel}}</span></td>
<td>{{if .AverageProgress}}<span class="bar"><i style="width:{{.AverageProgress}}%"></i></span>{{.AverageProgress}}%{{else}}<span class="muted">未填</span>{{end}} <span class="muted">{{.FilledTasks}}／{{.TotalTasks}} 已填</span></td>
<td>{{.CompletedInRange}} 项</td></tr>{{end}}{{end}}
</table>{{end}}
</body></html>`

func renderReportHTML(proj store.GetProjectRow, report Report) (string, error) {
	rangeLabels := map[string]string{"today": "今天", "week": "近 7 天", "month": "近 30 天", "all": "项目整体"}
	riskLabels := map[string]string{"normal": "正常", "warning": "预警", "high_risk": "高风险"}
	fmtTime := func(t time.Time) string { return t.In(domain.ProjectLocation).Format("2006-01-02 15:04") }
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"riskLabel": func(r RiskLevel) string { return riskLabels[string(r)] },
		"date":      func(d openapi_types.Date) string { return d.Time.Format("2006-01-02") },
		"datetime": func(t any) string {
			switch v := t.(type) {
			case time.Time:
				return fmtTime(v)
			case *time.Time:
				if v != nil {
					return fmtTime(*v)
				}
			}
			return ""
		},
	}).Parse(reportTemplateText)
	if err != nil {
		return "", err
	}
	from := ""
	if report.From != nil {
		from = fmtTime(*report.From)
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]any{
		"ProjectName": proj.Name,
		"RangeLabel":  rangeLabels[string(report.Range)],
		"From":        from,
		"GeneratedAt": fmtTime(report.GeneratedAt),
		"Report":      report,
	})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

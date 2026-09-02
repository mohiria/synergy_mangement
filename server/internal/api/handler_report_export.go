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

const reportTemplateText = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<style>
body { font-family: "PingFang SC", "Microsoft YaHei", sans-serif; color: #1f2937; margin: 24px; font-size: 14px; }
h1 { font-size: 20px; margin: 0 0 4px; }
.meta { color: #6b778c; font-size: 12px; margin-bottom: 18px; }
h2 { font-size: 15px; margin: 18px 0 8px; border-left: 3px solid #5267df; padding-left: 8px; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { border: 1px solid #dfe5ec; padding: 6px 8px; text-align: left; }
th { background: #f7f9fb; color: #657184; font-size: 12px; }
.item { border: 1px solid #dfe5ec; border-radius: 6px; padding: 8px 10px; margin-bottom: 6px; }
.item small { color: #6b778c; display: block; font-size: 12px; }
.pill { display: inline-block; border-radius: 4px; padding: 1px 6px; font-size: 12px; }
.warning { background: #fff6df; color: #a66a12; }
.high_risk { background: #fff0f1; color: #c44752; }
.normal { background: #ebf7f0; color: #377d5b; }
.empty { color: #6b778c; font-size: 12px; }
</style></head><body>
<h1>{{.ProjectName}} · 项目报告</h1>
<div class="meta">范围：{{.RangeLabel}} · 生成于 {{.GeneratedAt}}</div>
<h2>O／KR 进展</h2>
<table><tr><th>KR</th><th>风险</th><th>进度覆盖度</th><th>范围内终审通过</th></tr>
{{range .Report.KrProgress}}<tr><td>{{.Description}}</td><td><span class="pill {{.RiskLevel}}">{{riskLabel .RiskLevel}}</span></td>
<td>{{.FilledTasks}}／{{.TotalTasks}} 已填进度{{if .AverageProgress}}，平均 {{.AverageProgress}}%{{end}}</td>
<td>{{.CompletedInRange}} 项</td></tr>{{end}}</table>
<h2>完成成果</h2>
{{if not .Report.CompletedDeliverables}}<div class="empty">该范围内没有新生效的当前成果</div>{{end}}
{{range .Report.CompletedDeliverables}}<div class="item"><b>{{.TaskName}} / {{.DeliverableName}}</b><small>{{.FileName}}</small></div>{{end}}
<h2>风险与卡点</h2>
{{if not .Report.Blockers}}<div class="empty">没有需要关注的卡点</div>{{end}}
{{range .Report.Blockers}}<div class="item"><b>{{.TaskName}}：缺 {{.Missing}}</b> <span class="pill {{.Level}}">{{riskLabel .Level}}</span><small>{{.Reason}}{{if .ActionOwnerName}} · 待行动人 {{.ActionOwnerName}}{{end}}</small></div>{{end}}
<h2>待决策</h2>
<div class="item">关闭申请 {{.Report.PendingApprovals.CancelRequests}} 件 · 完成审核 {{.Report.PendingApprovals.Completions}} 件仍停留在审批队列。</div>
<h2>下一步（临近截止／已超期）</h2>
{{if not .Report.NextSteps}}<div class="empty">未来 7 天内没有临近截止的任务</div>{{end}}
{{range .Report.NextSteps}}<div class="item"><b>{{.TaskName}}</b>{{if .Overdue}}{{if deref .Overdue}} <span class="pill high_risk">已超期</span>{{end}}{{end}}<small>{{.OwnerName}}{{if .EndDate}} · 截止 {{.EndDate}}{{end}}</small></div>{{end}}
</body></html>`

func renderReportHTML(proj store.GetProjectRow, report Report) (string, error) {
	rangeLabels := map[string]string{"today": "今天", "week": "近 7 天", "month": "近 30 天", "all": "项目整体"}
	riskLabels := map[string]string{"normal": "正常", "warning": "预警", "high_risk": "高风险"}
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"riskLabel": func(r RiskLevel) string { return riskLabels[string(r)] },
		"deref":     func(b *bool) bool { return b != nil && *b },
	}).Parse(reportTemplateText)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]any{
		"ProjectName": proj.Name,
		"RangeLabel":  rangeLabels[string(report.Range)],
		"GeneratedAt": report.GeneratedAt.Format("2006-01-02 15:04"),
		"Report":      report,
	})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

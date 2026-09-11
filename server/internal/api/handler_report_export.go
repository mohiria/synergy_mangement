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

// 模板结构与前端报告页一致（PRD §7.8）：摘要条 + 正文三段 本期成果／风险与卡点／下一步，O／KR 进展作附录；
// 面向领导阅读，只保留结论级字段（不列文件生效日期、卡点原因与缺失项、KR 行与未就绪注记）。
const reportTemplateText = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<style>
body { font-family: "PingFang SC", "Microsoft YaHei", sans-serif; color: #1f2937; margin: 24px; font-size: 14px; line-height: 1.5; }
h1 { font-size: 20px; margin: 0 0 4px; }
.meta { color: #6b778c; font-size: 12px; margin-bottom: 14px; }
h2 { font-size: 15px; margin: 22px 0 8px; border-left: 3px solid #5267df; padding-left: 8px; display: flex; align-items: center; gap: 10px; }
.sub { margin-left: auto; font-size: 12px; font-weight: 500; color: #6b7588; }
.summary { display: grid; grid-template-columns: repeat(4, 1fr); border: 1px solid #e2e6ed; border-radius: 4px; overflow: hidden; margin: 12px 0 4px; }
.summary > div { padding: 10px 12px; border-right: 1px solid #e2e6ed; background: #fafbfc; }
.summary > div:last-child { border-right: 0; }
.summary small { display: block; font-size: 12px; color: #6b7588; font-weight: 600; }
.summary b { display: block; margin-top: 2px; font-size: 20px; font-weight: 650; color: #1f2a44; }
.summary b.warn { color: #c83f50; }
.summary span { display: block; font-size: 12px; color: #6b7588; }
table { width: 100%; border-collapse: collapse; table-layout: fixed; font-size: 13px; }
th, td { border: 1px solid #dfe5ec; padding: 6px 8px; text-align: left; vertical-align: top; overflow-wrap: anywhere; }
th { background: #f7f9fb; color: #657184; font-size: 12px; white-space: nowrap; }
tr.group td { background: #f3f5f8; font-weight: 650; color: #3e4b61; }
td.date { white-space: nowrap; }
.code { font-family: Menlo, Consolas, monospace; font-size: 12px; color: #42526b; background: #eef1f5; border-radius: 4px; padding: 1px 6px; white-space: nowrap; }
.tag { display: inline-block; border-radius: 4px; padding: 1px 7px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.tag.new { background: #fdecef; color: #c83f50; }
.tag.carried { background: #fff3da; color: #ad6e13; }
.tag.gray { background: #eef1f5; color: #677287; font-weight: 500; }
.pill { display: inline-block; border-radius: 4px; padding: 1px 6px; font-size: 12px; white-space: nowrap; }
.warning { background: #fff6df; color: #a66a12; }
.high_risk { background: #fff0f1; color: #c44752; }
.normal { background: #ebf7f0; color: #377d5b; }
.empty { color: #6b778c; font-size: 12px; margin-left: 12px; }
.muted { color: #6b7588; font-size: 12px; }
.files { margin-top: 2px; font-size: 12px; color: #6b7588; }
.blk-list { border-top: 1px solid #e2e6ed; }
.blk { display: grid; grid-template-columns: auto minmax(0, 1fr) auto auto; gap: 10px; align-items: center; padding: 7px 10px; border-bottom: 1px solid #eceff4; }
.blk .actor { white-space: nowrap; }
.blk .actor small { color: #6b7588; margin-right: 4px; }
.blk .stay { min-width: 60px; text-align: right; font-size: 12px; color: #6b7588; white-space: nowrap; }
.blk .stay b { font-size: 14px; color: #1f2937; }
.resolved-line { margin-top: 8px; padding: 6px 10px; border-radius: 4px; background: #f4faf7; font-size: 13px; line-height: 1.7; }
.resolved-line > b { color: #247a5a; }
.section-sub { margin: 12px 0 4px; font-weight: 650; font-size: 12px; color: #6b7588; }
tr.overdue td:first-child { box-shadow: inset 3px 0 #c83f50; }
.red { color: #c83f50; font-weight: 600; }
.bar { display: inline-block; width: 100px; height: 6px; background: #eef1f5; border-radius: 3px; vertical-align: middle; }
.bar i { display: block; height: 100%; background: #5267df; border-radius: 3px; }
</style></head><body>
<h1>{{.ProjectName}} · 项目报告</h1>
<div class="meta">{{.RangeLabel}}{{if .From}}（{{.From}} ~ {{.GeneratedAt}}）{{end}} · 生成时间 {{.GeneratedAt}}</div>

<div class="summary">
<div><small>本期成果</small><b>{{.Report.Deliveries.CompletedTasks}} 项</b><span>任务完成 · {{.Report.Deliveries.EffectiveFiles}} 份交付物生效</span></div>
<div><small>开放卡点</small><b{{if .Report.Blockers.Open}} class="warn"{{end}}>{{len .Report.Blockers.Open}} 项</b><span>{{if .From}}本期新增 {{.Report.Blockers.NewInRange}} · 解除 {{.Report.Blockers.ResolvedInRange}}{{else}}项目整体累计{{end}}</span></div>
<div><small>到期／超期</small><b{{if .OverdueCount}} class="warn"{{end}}>{{len .Report.NextSteps.Due}} 项</b><span>其中超期 {{.OverdueCount}} 项</span></div>
<div><small>即将启动</small><b>{{len .Report.NextSteps.Upcoming}} 项</b><span>未来 {{.Report.NextSteps.HorizonDays}} 天内</span></div>
</div>

<h2>一、本期成果</h2>
{{if not .Report.Deliveries.Objectives}}<div class="empty">该范围内没有终审通过的任务，也没有新生效的交付内容</div>{{else}}
<table><colgroup><col><col style="width:80px"><col style="width:76px"><col style="width:96px"></colgroup>
<tr><th>任务</th><th>负责人</th><th>状态</th><th>完成日期</th></tr>
{{range .Report.Deliveries.Objectives}}<tr class="group"><td colspan="4"><span class="code">{{.Code}}</span> {{.Title}}</td></tr>
{{range .KeyResults}}{{range .Tasks}}<tr><td><span class="code">{{.Code}}</span> {{.Name}}{{if .Files}}<div class="files">交付物：{{range $i, $f := .Files}}{{if $i}}、{{end}}{{$f.FileName}}{{end}}</div>{{end}}</td>
<td>{{.OwnerName}}</td><td>{{.StatusLabel}}</td>
<td class="date">{{if .CompletedAt}}{{date .CompletedAt}}{{else if .Progress}}进度 {{.Progress}}%{{else}}—{{end}}</td></tr>{{end}}{{end}}{{end}}
</table>{{end}}

<h2>二、风险与卡点{{if .Report.Blockers.PendingCompletions}}<span class="sub">另有 {{.Report.Blockers.PendingCompletions}} 件完成审核仍在审批队列</span>{{end}}</h2>
{{if not .Report.Blockers.Open}}<div class="empty">当前没有开放的卡点</div>{{else}}<div class="blk-list">
{{range .Report.Blockers.Open}}<div class="blk">{{if .PhaseLabel}}<span class="tag {{.Phase}}">{{.PhaseLabel}}</span>{{else}}<span class="pill {{.Level}}">{{riskLabel .Level}}</span>{{end}}
<div><span class="code">{{.Code}}</span> <b>{{.TaskName}}</b> <span class="tag gray">{{.KindLabel}}</span></div>
<div class="actor">{{if .ActionOwnerName}}<small>待行动</small>{{.ActionOwnerName}}{{else}}—{{end}}</div>
<div class="stay"><b>{{.StayDays}}</b> 天</div></div>{{end}}
</div>{{end}}
{{if .Report.Blockers.Resolved}}<div class="resolved-line"><b>本期解除 {{len .Report.Blockers.Resolved}} 项</b> {{range $i, $b := .Report.Blockers.Resolved}}{{if $i}}；{{end}}<span class="code">{{$b.Code}}</span> {{$b.TaskName}}（{{$b.KindLabel}}，{{date $b.ResolvedAt}} 解除）{{end}}</div>{{end}}

<h2>三、下一步<span class="sub">未来 {{.Report.NextSteps.HorizonDays}} 天</span></h2>
<div class="section-sub">到期／超期</div>
{{if not .Report.NextSteps.Due}}<div class="empty">窗口内没有到期或超期的任务</div>{{else}}
<table><colgroup><col><col style="width:80px"><col style="width:76px"><col style="width:120px"></colgroup>
<tr><th>任务</th><th>负责人</th><th>状态</th><th>截止日期</th></tr>
{{range .Report.NextSteps.Due}}<tr{{if .OverdueDays}} class="overdue"{{end}}><td><span class="code">{{.Code}}</span> {{.TaskName}}</td>
<td>{{.OwnerName}}</td><td>{{.StatusLabel}}</td>
<td class="date">{{if .OverdueDays}}<span class="red">{{date .EndDate}} 超期 {{.OverdueDays}} 天</span>{{else}}{{date .EndDate}}{{if eq (intv .DueInDays) 0}} 今天{{end}}{{end}}</td></tr>{{end}}
</table>{{end}}
{{if .Report.NextSteps.Upcoming}}<div class="section-sub">即将启动</div>
<table><colgroup><col><col style="width:80px"><col style="width:76px"><col style="width:120px"></colgroup>
<tr><th>任务</th><th>负责人</th><th>状态</th><th>计划启动</th></tr>
{{range .Report.NextSteps.Upcoming}}<tr><td><span class="code">{{.Code}}</span> {{.TaskName}}</td>
<td>{{.OwnerName}}</td><td>{{.StatusLabel}}</td><td class="date">{{date .StartDate}}</td></tr>{{end}}
</table>{{end}}

<h2>附、O／KR 进展</h2>
{{if not .Report.OkrProgress}}<div class="empty">尚无 O／KR</div>{{else}}
<table><colgroup><col><col style="width:64px"><col style="width:120px"><col style="width:52px"></colgroup>
{{range .Report.OkrProgress}}<tr class="group"><td colspan="4"><span class="code">{{.Code}}</span> {{.Title}}</td></tr>
{{range .KeyResults}}<tr><td><span class="code">{{.Code}}</span> {{.Description}}</td><td><span class="pill {{.RiskLevel}}">{{riskLabel .RiskLevel}}</span></td>
<td><span class="bar"><i style="width:{{intv .AverageProgress}}%"></i></span></td>
<td class="date">{{if .AverageProgress}}{{.AverageProgress}}%{{else}}<span class="muted">未填</span>{{end}}</td></tr>{{end}}{{end}}
</table>{{end}}
</body></html>`

func renderReportHTML(proj store.GetProjectRow, report Report) (string, error) {
	rangeLabels := map[string]string{"today": "今天", "week": "近 7 天", "month": "近 30 天", "all": "项目整体"}
	riskLabels := map[string]string{"normal": "正常", "warning": "预警", "high_risk": "高风险"}
	fmtTime := func(t time.Time) string { return t.In(domain.ProjectLocation).Format("2006-01-02 15:04") }
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"riskLabel": func(r RiskLevel) string { return riskLabels[string(r)] },
		// date 统一输出 YYYY-MM-DD：日期型字段直接取日，时间戳按项目时区取日。
		"date": func(t any) string {
			switch v := t.(type) {
			case openapi_types.Date:
				return v.Time.Format("2006-01-02")
			case time.Time:
				return v.In(domain.ProjectLocation).Format("2006-01-02")
			case *time.Time:
				if v != nil {
					return v.In(domain.ProjectLocation).Format("2006-01-02")
				}
			}
			return ""
		},
		"intv": func(p *int) int {
			if p == nil {
				return 0
			}
			return *p
		},
	}).Parse(reportTemplateText)
	if err != nil {
		return "", err
	}
	from := ""
	if report.From != nil {
		from = fmtTime(*report.From)
	}
	overdue := 0
	for _, n := range report.NextSteps.Due {
		if n.OverdueDays != nil {
			overdue++
		}
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]any{
		"ProjectName":  proj.Name,
		"RangeLabel":   rangeLabels[string(report.Range)],
		"From":         from,
		"GeneratedAt":  fmtTime(report.GeneratedAt),
		"OverdueCount": overdue,
		"Report":       report,
	})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

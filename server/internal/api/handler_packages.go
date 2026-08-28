package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 成果与归档、轻量成果包（AC-17、AC-18）。业务规则在 domain，handler 仅编排。

// GetArtifacts 统一归档视角：按 O／KR／任务组织当前成果、候选状态与审批记录数。
func (s *Server) GetArtifacts(w http.ResponseWriter, r *http.Request, projectId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	ctx := r.Context()
	objectives, err := s.q.ListObjectives(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	krs, err := s.q.ListKeyResultsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	deliverables, err := s.q.ListDeliverablesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	files, err := s.q.ListDeliverableFilesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	reviewCounts, err := s.q.CompletionReviewCountsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	countByTask := map[int64]int{}
	for _, c := range reviewCounts {
		countByTask[c.TaskID] = int(c.N)
	}
	// 任务状态显示文案（AC-04）：入池与终审取所属 KR 负责人，或签取审核组姓名。
	taskRows, err := s.q.ListProjectTasks(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	krOwnerNameByTask := map[int64]string{}
	for _, t := range taskRows {
		krOwnerNameByTask[t.ID] = t.KrOwnerName.String
	}
	reviewerRows, err := s.q.IntermediateReviewerNamesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	reviewerNamesByTask := map[int64][]string{}
	for _, rv := range reviewerRows {
		reviewerNamesByTask[rv.TaskID] = append(reviewerNamesByTask[rv.TaskID], rv.DisplayName)
	}
	filesByDeliverable := map[int64][]store.ListDeliverableFilesByProjectRow{}
	for _, f := range files {
		filesByDeliverable[f.DeliverableID] = append(filesByDeliverable[f.DeliverableID], f)
	}
	type taskAgg struct {
		task  ArtifactTask
		krID  int64
		objID int64
	}
	taskByID := map[int64]*taskAgg{}
	order := []int64{}
	for _, d := range deliverables {
		agg, ok := taskByID[d.TaskID]
		if !ok {
			agg = &taskAgg{
				task: ArtifactTask{
					TaskId:       d.TaskID,
					Name:         d.TaskName,
					Status:       TaskStatus(d.TaskStatus),
					StatusLabel:  domain.StatusLabel(d.TaskStatus, krOwnerNameByTask[d.TaskID], reviewerNamesByTask[d.TaskID]),
					ReviewCount:  countByTask[d.TaskID],
					Deliverables: []Deliverable{},
				},
				krID:  d.KeyResultID,
				objID: d.ObjectiveID,
			}
			taskByID[d.TaskID] = agg
			order = append(order, d.TaskID)
		}
		item := Deliverable{Id: d.ID, TaskId: d.TaskID, Name: d.Name}
		for _, f := range filesByDeliverable[d.ID] {
			view := toDeliverableFile(store.DeliverableFile{
				ID: f.ID, DeliverableID: f.DeliverableID, State: f.State, FileName: f.FileName,
				FileType: f.FileType, FileSize: f.FileSize, ObjectKey: f.ObjectKey,
				UploadedBy: f.UploadedBy, UploadedAt: f.UploadedAt, EffectiveAt: f.EffectiveAt,
			}, f.UploadedByName)
			switch f.State {
			case domain.DeliverableCurrent:
				item.Current = &view
			case domain.DeliverableCandidate:
				item.Candidate = &view
			}
		}
		agg.task.Deliverables = append(agg.task.Deliverables, item)
	}
	// 组装 O → KR → 任务。
	resp := []ArtifactObjective{}
	for _, o := range objectives {
		out := ArtifactObjective{ObjectiveId: o.ID, Title: o.Title, Krs: []ArtifactKr{}}
		for _, k := range krs {
			if k.ObjectiveID != o.ID {
				continue
			}
			kr := ArtifactKr{KeyResultId: k.ID, Description: k.Description, Tasks: []ArtifactTask{}}
			for _, taskID := range order {
				agg := taskByID[taskID]
				if agg.krID == k.ID {
					kr.Tasks = append(kr.Tasks, agg.task)
				}
			}
			if len(kr.Tasks) > 0 {
				out.Krs = append(out.Krs, kr)
			}
		}
		if len(out.Krs) > 0 {
			resp = append(resp, out)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) ListPackages(w http.ResponseWriter, r *http.Request, projectId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	resp, err := s.packageList(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) CreatePackage(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req CreatePackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	if !domain.CanCreatePackage(projectActor(uid, proj.OwnerID, proj.MyRole)) {
		writeForbidden(w)
		return
	}
	// 勾选项必须属于本项目且有已生效当前内容。
	deliverables, err := s.q.ListDeliverablesByProject(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	inProject := map[int64]bool{}
	for _, d := range deliverables {
		inProject[d.ID] = true
	}
	files, err := s.q.ListDeliverableFilesByProject(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	hasCurrent := map[int64]bool{}
	for _, f := range files {
		if f.State == domain.DeliverableCurrent {
			hasCurrent[f.DeliverableID] = true
		}
	}
	name := strings.TrimSpace(req.Name)
	if err := domain.ValidatePackage(name, req.DeliverableIds, func(id int64) bool {
		return inProject[id] && hasCurrent[id]
	}); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_package", Message: err.Error()})
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	pkg, err := qtx.CreatePackage(r.Context(), store.CreatePackageParams{ProjectID: projectId, Name: name, CreatedBy: uid})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	for _, id := range req.DeliverableIds {
		if err := qtx.CreatePackageItem(r.Context(), store.CreatePackageItemParams{PackageID: pkg.ID, DeliverableID: id}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	list, err := s.packageList(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	for _, p := range list {
		if p.Id == pkg.ID {
			writeJSON(w, http.StatusCreated, p)
			return
		}
	}
	writeInternalError(w, r, err)
}

// DownloadPackage 整包下载：目录解析为当前内容并流式打包（不复制旧文件，AC-18）。
func (s *Server) DownloadPackage(w http.ResponseWriter, r *http.Request, projectId int64, packageId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	pkg, err := s.q.GetPackageInProject(r.Context(), store.GetPackageInProjectParams{ID: packageId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "package_not_found", Message: "成果包不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	items, err := s.q.ListPackageItems(r.Context(), packageId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 先把全部对象取出来：响应头一旦写出就无法再改状态码，缺文件时不能伪装成功（E1）。
	objects := make(map[int64]io.ReadCloser, len(items))
	defer func() {
		for _, obj := range objects {
			_ = obj.Close()
		}
	}()
	var missing []string
	for _, item := range items {
		if !item.FileID.Valid || item.ObjectKey.String == "" {
			continue
		}
		obj, err := s.files.Get(r.Context(), item.ObjectKey.String)
		if err != nil {
			log.Printf("[package] request_id=%s 取对象失败 key=%s: %v", requestIDFrom(r.Context()), item.ObjectKey.String, err)
			missing = append(missing, fmt.Sprintf("%s / %s", item.TaskName, item.DeliverableName))
			continue
		}
		objects[item.FileID.Int64] = obj
	}
	if len(missing) > 0 {
		writeJSON(w, http.StatusBadGateway, Error{
			Code:    "package_incomplete",
			Message: "以下当前内容暂不可读取，成果包未生成：" + strings.Join(missing, "、"),
		})
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s.zip", url.PathEscape(pkg.Name)))
	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()
	// 来源清单随包附带。
	manifest, _ := zw.Create("成果包目录.txt")
	for _, item := range items {
		line := fmt.Sprintf("%s / %s", item.TaskName, item.DeliverableName)
		if item.FileID.Valid {
			line += " → " + item.FileName.String
		} else {
			line += " →（暂无已生效当前内容）"
		}
		_, _ = io.WriteString(manifest, line+"\n")
	}
	for _, item := range items {
		obj, ok := objects[item.FileID.Int64]
		if !ok {
			continue
		}
		entry, err := zw.Create(fmt.Sprintf("%s/%s", sanitizeObjectName(item.TaskName), sanitizeObjectName(item.FileName.String)))
		if err != nil {
			log.Printf("[package] request_id=%s 写入包内条目失败: %v", requestIDFrom(r.Context()), err)
			continue
		}
		if _, err := io.Copy(entry, obj); err != nil {
			log.Printf("[package] request_id=%s 拷贝对象失败: %v", requestIDFrom(r.Context()), err)
		}
	}
}

func (s *Server) packageList(ctx context.Context, projectID int64) ([]ArtifactPackage, error) {
	rows, err := s.q.ListPackagesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ArtifactPackage, 0, len(rows))
	for _, p := range rows {
		items, err := s.q.ListPackageItems(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		views := make([]PackageItem, 0, len(items))
		for _, item := range items {
			v := PackageItem{
				DeliverableId:   item.DeliverableID,
				DeliverableName: item.DeliverableName,
				TaskName:        item.TaskName,
			}
			if item.FileID.Valid {
				v.FileId = &item.FileID.Int64
				v.FileName = fromPgText(item.FileName)
				if item.EffectiveAt.Valid {
					v.EffectiveAt = &item.EffectiveAt.Time
				}
			}
			views = append(views, v)
		}
		out = append(out, ArtifactPackage{
			Id:            p.ID,
			Name:          p.Name,
			CreatedByName: optString(p.CreatedByName),
			CreatedAt:     p.CreatedAt.Time,
			Items:         views,
		})
	}
	return out, nil
}

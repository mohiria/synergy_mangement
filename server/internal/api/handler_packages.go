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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 成果与归档、轻量成果包（AC-17、AC-18）。业务规则在 domain，handler 仅编排。

// GetArtifacts 统一归档视角：按 O／KR／任务组织当前成果、候选状态与审批记录数。
// 「时间」维在服务端裁剪（§7.7、#86；沿用 #65 的服务端裁剪口径，不在前端过滤）。
func (s *Server) GetArtifacts(w http.ResponseWriter, r *http.Request, projectId int64, params GetArtifactsParams) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	ctx := r.Context()
	var from, to *time.Time
	if params.From != nil {
		v := params.From.Time
		from = &v
	}
	if params.To != nil {
		v := params.To.Time
		to = &v
	}
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
	// AC-67：内容状态里的「在审」以存在未决完成申请为准，不以候选文件在不在为准。
	pendingReviewTasks, err := s.q.PendingCompletionReviewTasksByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	pendingReviewByTask := make(map[int64]bool, len(pendingReviewTasks))
	for _, id := range pendingReviewTasks {
		pendingReviewByTask[id] = true
	}
	// 任务状态显示文案（AC-04）：入池与终审取所属 KR 负责人，或签取审核组姓名。
	taskRows, err := s.q.ListProjectTasks(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	krOwnerNameByTask := map[int64]string{}
	taskFactsByID := map[int64]store.ListProjectTasksRow{}
	for _, t := range taskRows {
		krOwnerNameByTask[t.ID] = t.KrOwnerName.String
		taskFactsByID[t.ID] = t
	}
	// 接收方名单（词汇表「接收方」）：归档列表按项展示成果交给谁。
	receiverRows, err := s.q.ListReceiversByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	receiverNamesByTask := map[int64][]string{}
	for _, rv := range receiverRows {
		receiverNamesByTask[rv.TaskID] = append(receiverNamesByTask[rv.TaskID], rv.DisplayName)
	}
	// 交付物承接的关系边（AC-17「来源关系边」列）。
	edgeRefRows, err := s.q.ListEdgeRefsByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	refs := make([]edgeRefRow, 0, len(edgeRefRows))
	for _, e := range edgeRefRows {
		refs = append(refs, edgeRefRow{
			ID: e.ID, DeliverableID: e.DeliverableID, Name: e.Name,
			EdgeType: e.EdgeType, TargetTaskID: e.TargetTaskID, TargetTaskName: e.TargetTaskName,
		})
	}
	edgesByDeliverable := edgeRefsByDeliverable(refs)
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
	// 过程文件与重要外部材料（§7.7）：归档按「文件类型」维筛选需要它们在列表层可见。
	taskFileRows, err := s.q.ListTaskFilesByProject(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	taskFilesByTask := map[int64][]TaskFile{}
	for _, f := range taskFileRows {
		// 「时间」维：过程文件与外部材料比对上传时间。
		var uploadedAt *time.Time
		if f.UploadedAt.Valid {
			v := f.UploadedAt.Time
			uploadedAt = &v
		}
		if !domain.InArchiveWindow(uploadedAt, from, to) {
			continue
		}
		taskFilesByTask[f.TaskID] = append(taskFilesByTask[f.TaskID], toTaskFile(store.TaskFile{
			ID: f.ID, TaskID: f.TaskID, Kind: f.Kind, State: f.State, FileName: f.FileName,
			FileType: f.FileType, FileSize: f.FileSize, ObjectKey: f.ObjectKey, Note: f.Note,
			UploadedBy: f.UploadedBy, UploadedAt: f.UploadedAt,
		}, f.UploadedByName))
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
			facts := taskFactsByID[d.TaskID]
			agg = &taskAgg{
				task: ArtifactTask{
					TaskId:        d.TaskID,
					Code:          domain.TaskCode(int(facts.ObjectiveCodeSeq), int(facts.KrCodeSeq), int(facts.CodeSeq)),
					Name:          d.TaskName,
					OwnerName:     facts.OwnerName,
					ReceiverLabel: receiverScopeSummary(facts.ReceiverScope, receiverNamesByTask[d.TaskID]),
					Status:        TaskStatus(d.TaskStatus),
					StatusLabel:   domain.StatusLabel(d.TaskStatus, krOwnerNameByTask[d.TaskID], reviewerNamesByTask[d.TaskID]),
					ReviewCount:   countByTask[d.TaskID],
					Deliverables:  []Deliverable{},
					Files:         taskFilesOf(taskFilesByTask, d.TaskID),
				},
				krID:  d.KeyResultID,
				objID: d.ObjectiveID,
			}
			taskByID[d.TaskID] = agg
			order = append(order, d.TaskID)
		}
		item := Deliverable{Id: d.ID, TaskId: d.TaskID, Name: d.Name, Edges: edgesByDeliverable[d.ID]}
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
		fillContentState(&item, pendingReviewByTask[d.TaskID])
		// 「时间」维：交付物比对内容状态时间（有当前内容取生效时刻，否则取候选提交时刻）。
		if !domain.InArchiveWindow(item.ContentStateAt, from, to) {
			continue
		}
		agg.task.Deliverables = append(agg.task.Deliverables, item)
	}
	// 只有过程文件／外部材料、还没有交付物项的任务同样进归档（§7.7 四类文件都在这里看，
	// 「文件类型」筛选维否则会漏掉它们）。
	objectiveByKr := make(map[int64]int64, len(krs))
	for _, k := range krs {
		objectiveByKr[k.ID] = k.ObjectiveID
	}
	for _, f := range taskFileRows {
		if _, ok := taskByID[f.TaskID]; ok {
			continue
		}
		if len(taskFilesByTask[f.TaskID]) == 0 {
			continue
		}
		facts, ok := taskFactsByID[f.TaskID]
		if !ok {
			continue
		}
		taskByID[f.TaskID] = &taskAgg{
			task: ArtifactTask{
				TaskId:        f.TaskID,
				Code:          domain.TaskCode(int(facts.ObjectiveCodeSeq), int(facts.KrCodeSeq), int(facts.CodeSeq)),
				Name:          facts.Name,
				OwnerName:     facts.OwnerName,
				ReceiverLabel: receiverScopeSummary(facts.ReceiverScope, receiverNamesByTask[f.TaskID]),
				Status:        TaskStatus(facts.Status),
				StatusLabel:   domain.StatusLabel(facts.Status, krOwnerNameByTask[f.TaskID], reviewerNamesByTask[f.TaskID]),
				ReviewCount:   countByTask[f.TaskID],
				Deliverables:  []Deliverable{},
				Files:         taskFilesOf(taskFilesByTask, f.TaskID),
			},
			krID:  facts.KeyResultID,
			objID: objectiveByKr[facts.KeyResultID],
		}
		order = append(order, f.TaskID)
	}
	// 时间维裁掉全部内容后，任务节点是空壳（既无交付物行也无文件行）——不再返回它，
	// 否则「按时间筛」会筛出一堆没有任何内容的任务行。
	if from != nil || to != nil {
		kept := order[:0]
		for _, id := range order {
			agg := taskByID[id]
			if len(agg.task.Deliverables) == 0 && (agg.task.Files == nil || len(*agg.task.Files) == 0) {
				delete(taskByID, id)
				continue
			}
			kept = append(kept, id)
		}
		order = kept
	}
	// 组装 O → KR → 任务。
	resp := []ArtifactObjective{}
	for _, o := range objectives {
		out := ArtifactObjective{
			ObjectiveId: o.ID,
			Code:        domain.ObjectiveCode(int(o.CodeSeq)),
			Title:       o.Title,
			Krs:         []ArtifactKr{},
		}
		for _, k := range krs {
			if k.ObjectiveID != o.ID {
				continue
			}
			kr := ArtifactKr{
				KeyResultId: k.ID,
				Code:        domain.KeyResultCode(int(k.ObjectiveCodeSeq), int(k.CodeSeq)),
				Description: k.Description,
				OwnerName:   k.OwnerName.String,
				Tasks:       []ArtifactTask{},
			}
			for _, taskID := range order {
				agg := taskByID[taskID]
				if agg.krID == k.ID {
					kr.Tasks = append(kr.Tasks, agg.task)
					kr.DeliverableCount += len(agg.task.Deliverables)
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
	// 过程文件与重要外部材料也可按需进包（§7.7 边界表第三列）。
	taskFileIDs := []int64{}
	if req.TaskFileIds != nil {
		taskFileIDs = *req.TaskFileIds
	}
	projectFiles, err := s.q.ListTaskFilesByProject(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	fileInProject := map[int64]bool{}
	for _, f := range projectFiles {
		fileInProject[f.ID] = true
	}
	name := strings.TrimSpace(req.Name)
	if err := domain.ValidatePackage(name, req.DeliverableIds, taskFileIDs, func(id int64) bool {
		return inProject[id] && hasCurrent[id]
	}, func(id int64) bool {
		return fileInProject[id]
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
		if err := qtx.CreatePackageItem(r.Context(), store.CreatePackageItemParams{
			PackageID: pkg.ID, DeliverableID: pgtype.Int8{Int64: id, Valid: true},
		}); err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	for _, id := range taskFileIDs {
		if err := qtx.CreatePackageTaskFileItem(r.Context(), store.CreatePackageTaskFileItemParams{
			PackageID: pkg.ID, TaskFileID: id,
		}); err != nil {
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
	resolved := make([]domain.PackageItem, 0, len(items))
	for _, row := range items {
		resolved = append(resolved, domain.ResolvePackageItem(packageItemFacts(row)))
	}
	// 先把全部对象取出来：响应头一旦写出就无法再改状态码，缺文件时不能伪装成功（E1）。
	objects := make(map[int64]io.ReadCloser, len(resolved))
	defer func() {
		for _, obj := range objects {
			_ = obj.Close()
		}
	}()
	var missing []string
	for _, item := range resolved {
		if item.FileID == nil || item.ObjectKey == "" {
			continue
		}
		obj, err := s.files.Get(r.Context(), item.ObjectKey)
		if err != nil {
			log.Printf("[package] request_id=%s 取对象失败 key=%s: %v", requestIDFrom(r.Context()), item.ObjectKey, err)
			missing = append(missing, fmt.Sprintf("%s / %s", item.TaskName, item.Name))
			continue
		}
		objects[*item.FileID] = obj
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
	for _, item := range resolved {
		_, _ = io.WriteString(manifest, domain.PackageManifestLine(item)+"\n")
	}
	for _, item := range resolved {
		if item.FileID == nil {
			continue
		}
		obj, ok := objects[*item.FileID]
		if !ok {
			continue
		}
		entry, err := zw.Create(fmt.Sprintf("%s/%s", sanitizeObjectName(item.TaskName), sanitizeObjectName(item.FileName)))
		if err != nil {
			log.Printf("[package] request_id=%s 写入包内条目失败: %v", requestIDFrom(r.Context()), err)
			continue
		}
		if _, err := io.Copy(entry, obj); err != nil {
			log.Printf("[package] request_id=%s 拷贝对象失败: %v", requestIDFrom(r.Context()), err)
		}
	}
}

// packageItemFacts 把库行搬成 domain 的入参：归一口径（含来源已删除的判定）只写在 domain 里。
func packageItemFacts(row store.ListPackageItemsRow) domain.PackageItemFacts {
	f := domain.PackageItemFacts{
		DeliverableName:     row.DeliverableName.String,
		DeliverableTaskName: row.DeliverableTaskName.String,
		CurrentFileName:     row.CurrentFileName.String,
		CurrentObjectKey:    row.CurrentObjectKey.String,
		TaskFileName:        row.TaskFileName.String,
		TaskFileKind:        row.FileKind.String,
		TaskFileObjectKey:   row.TaskFileObjectKey.String,
		TaskFileTaskName:    row.TaskFileTaskName.String,
		SourceTaskName:      row.SourceTaskName,
		SourceFileName:      row.SourceFileName,
		SourceFileKind:      row.SourceFileKind,
	}
	if row.DeliverableID.Valid {
		id := row.DeliverableID.Int64
		f.DeliverableID = &id
	}
	if row.CurrentFileID.Valid {
		id := row.CurrentFileID.Int64
		f.CurrentFileID = &id
	}
	if row.EffectiveAt.Valid {
		t := row.EffectiveAt.Time
		f.EffectiveAt = &t
	}
	if row.TaskFileID.Valid {
		id := row.TaskFileID.Int64
		f.TaskFileID = &id
	}
	return f
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
		for _, row := range items {
			item := domain.ResolvePackageItem(packageItemFacts(row))
			v := PackageItem{
				DeliverableId:   item.DeliverableID,
				TaskFileId:      item.TaskFileID,
				DeliverableName: item.Name,
				TaskName:        item.TaskName,
				SourceDeleted:   item.SourceDeleted,
			}
			if item.FileKind != "" {
				kind := TaskFileKind(item.FileKind)
				v.FileKind = &kind
			}
			if item.FileID != nil {
				v.FileId = item.FileID
				v.FileName = optString(item.FileName)
				v.EffectiveAt = item.EffectiveAt
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

// taskFilesOf 取任务下的过程文件与外部材料；没有时给空数组，前端不必区分 null。
func taskFilesOf(byTask map[int64][]TaskFile, taskID int64) *[]TaskFile {
	files := byTask[taskID]
	if files == nil {
		files = []TaskFile{}
	}
	return &files
}

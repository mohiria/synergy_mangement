package api

import (
	"net/http"
	"time"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 成果归档（AC-17）。业务规则在 domain，handler 仅编排；
// 轻量成果包已随裁决 G1（#140）整体移除。

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
	// 任务状态显示文案（AC-04；裁决 11：终审取项目管理员集合，或签取审核组姓名）。
	taskRows, err := s.q.ListProjectTasks(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	_, finalNames, err := s.projectFinalReviewers(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// 审核中任务的当前环节从完成申请单读取（裁决 13，#182）。
	reviewStageByTask, err := s.pendingReviewStageByTask(ctx, projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	taskFactsByID := map[int64]store.ListProjectTasksRow{}
	for _, t := range taskRows {
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
			ID: e.ID, SourceTaskID: e.SourceTaskID,
			Necessity: e.Necessity, TargetTaskID: e.TargetTaskID, TargetTaskName: e.TargetTaskName,
		})
	}
	edgesByTask := edgeRefsBySourceTask(refs)
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
		if !domain.InArchiveWindow(uploadedAt, from, to, time.Local) {
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
			// 裁决 G1（#140）：文件状态两档按所属任务派生。
			fsState, fsLabel := domain.ArchiveFileState(d.TaskStatus)
			fs := ArtifactTaskFileState(fsState)
			agg = &taskAgg{
				task: ArtifactTask{
					TaskId:         d.TaskID,
					Code:           domain.TaskCode(int(facts.ObjectiveCodeSeq), int(facts.KrCodeSeq), int(facts.CodeSeq)),
					Name:           d.TaskName,
					OwnerName:      facts.OwnerName,
					// #171：归档接收方列只显示成员信息——指定成员列名单、全员「项目全体成员」、未配置空。
					ReceiverLabel: domain.ReceiverDisplay(facts.ReceiverScope, receiverNamesByTask[d.TaskID]),
					Status:         TaskStatus(d.TaskStatus),
					StatusLabel:    domain.StatusLabel(d.TaskStatus, reviewStageByTask[d.TaskID], finalNames, reviewerNamesByTask[d.TaskID]),
					FileState:      &fs,
					FileStateLabel: &fsLabel,
					ReviewCount:    countByTask[d.TaskID],
					Deliverables:   []Deliverable{},
					Files:          taskFilesOf(taskFilesByTask, d.TaskID),
				},
				krID:  d.KeyResultID,
				objID: d.ObjectiveID,
			}
			taskByID[d.TaskID] = agg
			order = append(order, d.TaskID)
		}
		item := Deliverable{Id: d.ID, TaskId: d.TaskID, Name: d.Name, Edges: edgesByTask[d.TaskID]}
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
		if !domain.InArchiveWindow(item.ContentStateAt, from, to, time.Local) {
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
		fsState, fsLabel := domain.ArchiveFileState(facts.Status)
		fs := ArtifactTaskFileState(fsState)
		taskByID[f.TaskID] = &taskAgg{
			task: ArtifactTask{
				TaskId:         f.TaskID,
				Code:           domain.TaskCode(int(facts.ObjectiveCodeSeq), int(facts.KrCodeSeq), int(facts.CodeSeq)),
				Name:           facts.Name,
				OwnerName:      facts.OwnerName,
				ReceiverLabel:  domain.ReceiverDisplay(facts.ReceiverScope, receiverNamesByTask[f.TaskID]),
				Status:         TaskStatus(facts.Status),
				StatusLabel:    domain.StatusLabel(facts.Status, reviewStageByTask[f.TaskID], finalNames, reviewerNamesByTask[f.TaskID]),
				FileState:      &fs,
				FileStateLabel: &fsLabel,
				ReviewCount:    countByTask[f.TaskID],
				Deliverables:   []Deliverable{},
				Files:          taskFilesOf(taskFilesByTask, f.TaskID),
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

// taskFilesOf 取任务下的过程文件与外部材料；没有时给空数组，前端不必区分 null。
func taskFilesOf(byTask map[int64][]TaskFile, taskID int64) *[]TaskFile {
	files := byTask[taskID]
	if files == nil {
		files = []TaskFile{}
	}
	return &files
}

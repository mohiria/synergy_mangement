package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 交付物项与文件存取（AC-32、AC-33）。业务规则在 domain，文件走 MinIO 预签名 URL（ADR 0001）。

const presignExpiry = 15 * time.Minute

func (s *Server) CreateDeliverable(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req CreateDeliverableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	_, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	// 项名取上传文件的文件名（裁决 G1）：用户不再手填，重名不静默新建第二项。
	name := domain.DeliverableName("", req.FileName)
	existing, err := s.q.ListDeliverablesByTask(r.Context(), taskId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	existingNames := make([]string, 0, len(existing))
	for _, d := range existing {
		existingNames = append(existingNames, d.Name)
	}
	if err := domain.ValidateNewDeliverableName(name, existingNames); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_deliverable", Message: err.Error()})
		return
	}
	// 裁决 H1（#141）：提交完成申请前新增即时生效，不走关键字段审批；在审期间冻结。
	if err := domain.DeliverableStructureRule(actor, uid, facts); err != nil {
		writeDeliverableRuleError(w, err)
		return
	}
	if _, err := s.q.CreateDeliverable(r.Context(), store.CreateDeliverableParams{
		TaskID: taskId, Name: name, CreatedBy: uid,
	}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// DeleteDeliverable 删除交付物项（裁决 H1，#141）：未发布的项（空／仅候选）可自由删，
// 有当前内容的项不可删（走成果更新）；行由 FK 级联清理，候选对象文件同步从对象存储移除。
func (s *Server) DeleteDeliverable(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64, deliverableId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	_, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	if _, err := s.q.GetDeliverableInProject(r.Context(), store.GetDeliverableInProjectParams{
		ID: deliverableId, ID_2: taskId, ProjectID: projectId,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "deliverable_not_found", Message: "交付物不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	hasCurrent, err := s.deliverableHasCurrent(r.Context(), deliverableId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := domain.DeleteDeliverableRule(actor, uid, facts, hasCurrent); err != nil {
		writeDeliverableRuleError(w, err)
		return
	}
	keys, err := s.q.DeleteDeliverable(r.Context(), deliverableId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	for _, key := range keys {
		s.removeObject(r.Context(), key)
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

// deliverableHasCurrent 报告交付物项是否已有当前内容（已终审发布）。
func (s *Server) deliverableHasCurrent(ctx context.Context, deliverableID int64) (bool, error) {
	files, err := s.q.ListFilesByDeliverable(ctx, deliverableID)
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if f.State == domain.DeliverableCurrent {
			return true, nil
		}
	}
	return false, nil
}

// writeDeliverableRuleError 把交付物增删规则错误映射为一致的 HTTP 回报。
func writeDeliverableRuleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrDeliverableChangeForbidden):
		writeForbidden(w)
	case errors.Is(err, domain.ErrDeliverableHasCurrent):
		writeJSON(w, http.StatusConflict, Error{Code: "deliverable_has_current", Message: err.Error()})
	default:
		writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: err.Error()})
	}
}

func (s *Server) UploadCandidate(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64, deliverableId int64) {
	var req UploadCandidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	d, err := s.q.GetDeliverableInProject(r.Context(), store.GetDeliverableInProjectParams{ID: deliverableId, ID_2: taskId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "deliverable_not_found", Message: "交付物不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	facts := domain.TaskFacts{Status: d.TaskStatus, CreatorID: d.TaskCreatedBy, OwnerID: d.TaskOwnerID, ResultUpdate: d.TaskResultUpdate}
	if !domain.CanUploadCandidate(actor, uid, facts) {
		switch d.TaskStatus {
		case domain.TaskNotStarted, domain.TaskWaitingInput, domain.TaskInProgress:
			writeForbidden(w)
		default:
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: "任务当前状态不能登记候选内容"})
		}
		return
	}
	fileName := strings.TrimSpace(req.FileName)
	if err := domain.ValidateCandidateFileName(fileName); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_file", Message: err.Error()})
		return
	}
	key := fmt.Sprintf("deliverables/%d/%d-%s", deliverableId, s.now().UnixNano(), sanitizeObjectName(fileName))
	fileType := ""
	if req.FileType != nil {
		fileType = strings.TrimSpace(*req.FileType)
	}
	var size int64
	if req.FileSize != nil {
		size = *req.FileSize
	}
	// 两阶段提交第一步：只落 uploading 记录（R4）。旧候选此刻不动——文件还没上传，
	// 提前删掉就会让「传了一半」的操作把已生效的候选弄丢（D1）。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	// 同一交付物项同时只允许一条待上传记录：重新发起时顶掉上一条。
	staleKey := ""
	stale, err := qtx.GetUploadingFile(r.Context(), deliverableId)
	switch {
	case err == nil:
		if _, err := qtx.DeleteDeliverableFile(r.Context(), stale.ID); err != nil {
			writeInternalError(w, r, err)
			return
		}
		staleKey = stale.ObjectKey
	case !errors.Is(err, pgx.ErrNoRows):
		writeInternalError(w, r, err)
		return
	}
	f, err := qtx.CreateDeliverableFile(r.Context(), store.CreateDeliverableFileParams{
		DeliverableID: deliverableId,
		State:         domain.DeliverableUploading,
		FileName:      fileName,
		FileType:      fileType,
		FileSize:      size,
		ObjectKey:     key,
		UploadedBy:    uid,
	})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if staleKey != "" {
		s.removeObject(r.Context(), staleKey)
	}
	uploadURL, err := s.files.PresignPut(r.Context(), key, presignExpiry)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	user := currentUser(r)
	writeJSON(w, http.StatusCreated, UploadCandidateResponse{
		File:      toDeliverableFile(f, user.DisplayName),
		UploadUrl: uploadURL,
	})
}

// CommitCandidate 两阶段提交第二步：校验对象确已写入后，uploading → candidate 并覆盖旧候选（R4）。
func (s *Server) CommitCandidate(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64, deliverableId int64) {
	var req CommitUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	d, err := s.q.GetDeliverableInProject(r.Context(), store.GetDeliverableInProjectParams{ID: deliverableId, ID_2: taskId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "deliverable_not_found", Message: "交付物不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	facts := domain.TaskFacts{Status: d.TaskStatus, CreatorID: d.TaskCreatedBy, OwnerID: d.TaskOwnerID, ResultUpdate: d.TaskResultUpdate}
	if !domain.CanUploadCandidate(actor, uid, facts) {
		writeForbidden(w)
		return
	}
	pending, err := s.q.GetUploadingFile(r.Context(), deliverableId)
	if err != nil || pending.ID != req.FileId {
		writeJSON(w, http.StatusConflict, Error{Code: "upload_state_conflict", Message: "没有待确认的上传记录"})
		return
	}
	// 对象必须真的写进了存储：预签名直传绕过服务端，不校验就等于「没传也算已提交」。
	size, err := s.files.Stat(r.Context(), pending.ObjectKey)
	if err != nil {
		writeJSON(w, http.StatusConflict, Error{Code: "upload_not_found", Message: "文件尚未上传完成，请重试"})
		return
	}
	// 大小按对象存储的真实值兜底：客户端自报的 fileSize 与前端限制都可绕过。
	if err := domain.ValidateUploadSize(size); err != nil {
		if _, delErr := s.q.DeleteDeliverableFile(r.Context(), pending.ID); delErr != nil {
			writeInternalError(w, r, delErr)
			return
		}
		s.removeObject(r.Context(), pending.ObjectKey)
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_file", Message: err.Error()})
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	// 确认成功才覆盖旧候选（不留历史版本，§5.3）。
	oldKey := ""
	old, err := qtx.GetCandidateFile(r.Context(), deliverableId)
	switch {
	case err == nil:
		if _, err := qtx.DeleteDeliverableFile(r.Context(), old.ID); err != nil {
			writeInternalError(w, r, err)
			return
		}
		oldKey = old.ObjectKey
	case !errors.Is(err, pgx.ErrNoRows):
		writeInternalError(w, r, err)
		return
	}
	f, err := qtx.CommitUploadingFile(r.Context(), store.CommitUploadingFileParams{ID: pending.ID, FileSize: size})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	if oldKey != "" {
		s.removeObject(r.Context(), oldKey)
	}
	writeJSON(w, http.StatusOK, toDeliverableFile(f, currentUser(r).DisplayName))
}

func (s *Server) GetFileDownloadUrl(w http.ResponseWriter, r *http.Request, projectId int64, fileId int64, params GetFileDownloadUrlParams) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	f, err := s.q.GetDeliverableFileInProject(r.Context(), store.GetDeliverableFileInProjectParams{ID: fileId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "file_not_found", Message: "文件不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	// #124：预览（inline）或下载（attachment，默认）由调用方声明；预签名带对应 disposition。
	inline := params.Disposition != nil && string(*params.Disposition) == "inline"
	url, err := s.files.PresignGet(r.Context(), f.ObjectKey, f.FileName, inline, presignExpiry)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, DownloadUrlResponse{Url: url})
}

// deliverableList 组装任务的交付物项（含当前内容与候选，AC-32/33）；
// canDelete 按裁决 H1 派生（有编辑权限＋草稿或执行类状态＋无当前内容），前端不复刻规则。
func (s *Server) deliverableList(ctx context.Context, taskID int64, actor domain.Actor, uid int64, facts domain.TaskFacts) ([]Deliverable, error) {
	items, err := s.q.ListDeliverablesByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	// AC-67：候选是不是「在审」看有没有未决完成申请，不看文件在不在。
	hasPendingReview, err := s.q.HasPendingCompletionReview(ctx, taskID)
	if err != nil {
		return nil, err
	}
	files, err := s.q.ListDeliverableFilesByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	byDeliverable := make(map[int64][]store.ListDeliverableFilesByTaskRow)
	for _, f := range files {
		byDeliverable[f.DeliverableID] = append(byDeliverable[f.DeliverableID], f)
	}
	edgeRows, err := s.q.ListEdgeRefsByDeliverableTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	refs := make([]edgeRefRow, 0, len(edgeRows))
	for _, e := range edgeRows {
		refs = append(refs, edgeRefRow{
			ID: e.ID, DeliverableID: e.DeliverableID,
			EdgeType: e.EdgeType, TargetTaskID: e.TargetTaskID, TargetTaskName: e.TargetTaskName,
		})
	}
	edgesByDeliverable := edgeRefsByDeliverable(refs)
	out := make([]Deliverable, 0, len(items))
	for _, d := range items {
		item := Deliverable{Id: d.ID, TaskId: d.TaskID, Name: d.Name, Edges: edgesByDeliverable[d.ID]}
		for _, f := range byDeliverable[d.ID] {
			view := toDeliverableFile(store.DeliverableFile{
				ID: f.ID, DeliverableID: f.DeliverableID, State: f.State,
				FileName: f.FileName, FileType: f.FileType, FileSize: f.FileSize,
				ObjectKey: f.ObjectKey, UploadedBy: f.UploadedBy, UploadedAt: f.UploadedAt,
				EffectiveAt: f.EffectiveAt,
			}, f.UploadedByName)
			switch f.State {
			case domain.DeliverableCurrent:
				item.Current = &view
			case domain.DeliverableCandidate:
				item.Candidate = &view
			}
		}
		fillContentState(&item, hasPendingReview)
		item.CanDelete = domain.DeleteDeliverableRule(actor, uid, facts, item.Current != nil) == nil
		out = append(out, item)
	}
	return out, nil
}

// fillContentState 补内容状态与提交／生效时间（AC-17、AC-67）：状态在 domain 派生，
// 时间取当前内容的生效时刻，没有当前内容时退到候选的提交时刻。
func fillContentState(item *Deliverable, hasPendingReview bool) {
	state := domain.DeriveContentState(item.Current != nil, item.Candidate != nil, hasPendingReview)
	item.ContentState = DeliverableContentState(state)
	item.ContentStateLabel = domain.ContentStateLabel(state)
	switch {
	case item.Current != nil:
		item.ContentStateAt = item.Current.EffectiveAt
	case item.Candidate != nil:
		item.ContentStateAt = item.Candidate.UploadedAt
	}
	if item.Edges == nil {
		item.Edges = []DeliverableEdgeRef{}
	}
}

// edgeRefsByDeliverable 把关系边行按来源交付物归拢；两处查询行结构一致，此处只取公共字段。
type edgeRefRow struct {
	ID             int64
	DeliverableID  pgtype.Int8
	EdgeType       string
	TargetTaskID   int64
	TargetTaskName string
}

func edgeRefsByDeliverable(rows []edgeRefRow) map[int64][]DeliverableEdgeRef {
	out := map[int64][]DeliverableEdgeRef{}
	for _, row := range rows {
		if !row.DeliverableID.Valid {
			continue
		}
		out[row.DeliverableID.Int64] = append(out[row.DeliverableID.Int64], DeliverableEdgeRef{
			EdgeId:         row.ID,
			EdgeType:       EdgeType(row.EdgeType),
			EdgeTypeLabel:  domain.EdgeTypeLabel(row.EdgeType),
			TargetTaskId:   row.TargetTaskID,
			TargetTaskName: row.TargetTaskName,
		})
	}
	return out
}

func toDeliverableFile(f store.DeliverableFile, uploadedByName string) DeliverableFile {
	out := DeliverableFile{
		Id:             f.ID,
		State:          DeliverableFileState(f.State),
		FileName:       f.FileName,
		FileType:       optString(f.FileType),
		UploadedByName: optString(uploadedByName),
	}
	if f.FileSize > 0 {
		out.FileSize = &f.FileSize
	}
	if f.UploadedAt.Valid {
		out.UploadedAt = &f.UploadedAt.Time
	}
	if f.EffectiveAt.Valid {
		out.EffectiveAt = &f.EffectiveAt.Time
	}
	return out
}

// sanitizeObjectName 去除路径分隔符等，防止对象 key 越界。
func sanitizeObjectName(name string) string {
	repl := strings.NewReplacer("/", "_", "\\", "_", "..", "_", "#", "_", "?", "_")
	return repl.Replace(name)
}

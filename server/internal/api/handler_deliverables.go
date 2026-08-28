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
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	_, facts, ok := s.fetchTask(w, r, projectId, taskId)
	if !ok {
		return
	}
	if !domain.CanManageDeliverables(actor, uid, facts) {
		if facts.Status == domain.TaskCompleted || facts.Status == domain.TaskCancelled {
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: "任务已终止，不能再配置交付物"})
			return
		}
		writeForbidden(w)
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := domain.ValidateDeliverableName(name); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_deliverable", Message: err.Error()})
		return
	}
	d, err := s.q.CreateDeliverable(r.Context(), store.CreateDeliverableParams{TaskID: taskId, Name: name, CreatedBy: uid})
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, Deliverable{Id: d.ID, TaskId: d.TaskID, Name: d.Name})
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
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	d, err := s.q.GetDeliverableInProject(r.Context(), store.GetDeliverableInProjectParams{ID: deliverableId, ID_2: taskId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "deliverable_not_found", Message: "交付物不存在"})
		} else {
			writeInternalError(w)
		}
		return
	}
	facts := domain.TaskFacts{Status: d.TaskStatus, CreatorID: d.TaskCreatedBy, OwnerID: d.TaskOwnerID}
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
	// 删旧候选与建新候选必须同事务：中途失败会让旧候选永久消失且无新记录顶替（D1）。
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	// 重复登记覆盖旧候选（不留历史版本，§5.3）；对象删除延后到提交之后。
	oldKey := ""
	old, err := qtx.GetCandidateFile(r.Context(), deliverableId)
	switch {
	case err == nil:
		if _, err := qtx.DeleteDeliverableFile(r.Context(), old.ID); err != nil {
			writeInternalError(w)
			return
		}
		oldKey = old.ObjectKey
	case !errors.Is(err, pgx.ErrNoRows):
		writeInternalError(w)
		return
	}
	f, err := qtx.CreateDeliverableFile(r.Context(), store.CreateDeliverableFileParams{
		DeliverableID: deliverableId,
		State:         domain.DeliverableCandidate,
		FileName:      fileName,
		FileType:      fileType,
		FileSize:      size,
		ObjectKey:     key,
		UploadedBy:    uid,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w)
		return
	}
	if oldKey != "" {
		_ = s.files.Remove(r.Context(), oldKey)
	}
	uploadURL, err := s.files.PresignPut(r.Context(), key, presignExpiry)
	if err != nil {
		writeInternalError(w)
		return
	}
	user := currentUser(r)
	writeJSON(w, http.StatusCreated, UploadCandidateResponse{
		File:      toDeliverableFile(f, user.DisplayName),
		UploadUrl: uploadURL,
	})
}

func (s *Server) GetFileDownloadUrl(w http.ResponseWriter, r *http.Request, projectId int64, fileId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	f, err := s.q.GetDeliverableFileInProject(r.Context(), store.GetDeliverableFileInProjectParams{ID: fileId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "file_not_found", Message: "文件不存在"})
		} else {
			writeInternalError(w)
		}
		return
	}
	url, err := s.files.PresignGet(r.Context(), f.ObjectKey, f.FileName, presignExpiry)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, DownloadUrlResponse{Url: url})
}

// deliverableList 组装任务的交付物项（含当前内容与候选，AC-32/33）。
func (s *Server) deliverableList(ctx context.Context, taskID int64) ([]Deliverable, error) {
	items, err := s.q.ListDeliverablesByTask(ctx, taskID)
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
	out := make([]Deliverable, 0, len(items))
	for _, d := range items {
		item := Deliverable{Id: d.ID, TaskId: d.TaskID, Name: d.Name}
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
		out = append(out, item)
	}
	return out, nil
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

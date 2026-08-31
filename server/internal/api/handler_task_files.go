package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 过程文件与重要外部材料（§7.7 文件对象边界表；AC-17／AC-18）。
// 边界规则在 domain（FileBoundary）：不进完成审批、不作下游正式输入，可按需选进成果包。
// 文件与候选交付物同走两阶段提交，handler 只编排。

// UploadTaskFile 登记任务文件并返回预签名上传地址（两阶段提交第一步）。
func (s *Server) UploadTaskFile(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req UploadTaskFileRequest
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
	if !domain.CanManageTaskFiles(actor, uid, facts) {
		if facts.Status == domain.TaskCancelled {
			writeJSON(w, http.StatusConflict, Error{Code: "task_state_conflict", Message: "已关闭任务不再接受文件"})
			return
		}
		writeForbidden(w)
		return
	}
	kind := string(req.Kind)
	if err := domain.ValidateTaskFileKind(kind); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_file_kind", Message: err.Error()})
		return
	}
	fileName := strings.TrimSpace(req.FileName)
	if err := domain.ValidateCandidateFileName(fileName); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_file", Message: err.Error()})
		return
	}
	note := ""
	if req.Note != nil {
		note = strings.TrimSpace(*req.Note)
	}
	if err := domain.ValidateTaskFileNote(note); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_file", Message: err.Error()})
		return
	}
	fileType := ""
	if req.FileType != nil {
		fileType = strings.TrimSpace(*req.FileType)
	}
	var size int64
	if req.FileSize != nil {
		size = *req.FileSize
	}
	key := fmt.Sprintf("task-files/%d/%d-%s", taskId, s.now().UnixNano(), sanitizeObjectName(fileName))
	f, err := s.q.CreateTaskFile(r.Context(), store.CreateTaskFileParams{
		TaskID: taskId, Kind: kind, FileName: fileName, FileType: fileType,
		FileSize: size, ObjectKey: key, Note: note, UploadedBy: uid,
	})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	uploadURL, err := s.files.PresignPut(r.Context(), key, presignExpiry)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, UploadTaskFileResponse{
		File:      toTaskFile(f, currentUser(r).DisplayName),
		UploadUrl: uploadURL,
	})
}

// CommitTaskFile 两阶段提交第二步：校验对象确已写入后才转 ready（R4）。
func (s *Server) CommitTaskFile(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64, fileId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	row, ok := s.fetchTaskFile(w, r, projectId, taskId, fileId)
	if !ok {
		return
	}
	facts := domain.TaskFacts{Status: row.TaskStatus, CreatorID: row.TaskCreatedBy, OwnerID: row.TaskOwnerID}
	if !domain.CanManageTaskFiles(actor, uid, facts) {
		writeForbidden(w)
		return
	}
	if row.State != domain.TaskFileUploading {
		writeJSON(w, http.StatusConflict, Error{Code: "upload_state_conflict", Message: "没有待确认的上传记录"})
		return
	}
	// 对象必须真的写进了存储：预签名直传绕过服务端，不校验就等于「没传也算已上传」。
	size, err := s.files.Stat(r.Context(), row.ObjectKey)
	if err != nil {
		writeJSON(w, http.StatusConflict, Error{Code: "upload_not_found", Message: "文件尚未上传完成，请重试"})
		return
	}
	if err := domain.ValidateUploadSize(size); err != nil {
		if _, delErr := s.q.DeleteTaskFile(r.Context(), row.ID); delErr != nil {
			writeInternalError(w, r, delErr)
			return
		}
		s.removeObject(r.Context(), row.ObjectKey)
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_file", Message: err.Error()})
		return
	}
	f, err := s.q.CommitTaskFile(r.Context(), store.CommitTaskFileParams{ID: row.ID, FileSize: size})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTaskFile(f, row.UploadedByName))
}

// DeleteTaskFile 删除过程文件或外部材料：这两类文件不进审批，删除直接生效（对象随后清理）。
func (s *Server) DeleteTaskFile(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64, fileId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole, proj.Visibility)
	row, ok := s.fetchTaskFile(w, r, projectId, taskId, fileId)
	if !ok {
		return
	}
	facts := domain.TaskFacts{Status: row.TaskStatus, CreatorID: row.TaskCreatedBy, OwnerID: row.TaskOwnerID}
	if !domain.CanManageTaskFiles(actor, uid, facts) {
		writeForbidden(w)
		return
	}
	if _, err := s.q.DeleteTaskFile(r.Context(), row.ID); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.removeObject(r.Context(), row.ObjectKey)
	w.WriteHeader(http.StatusNoContent)
}

// GetTaskFileDownloadUrl 预签名下载：全体项目成员可查看、下载（§3.3）。
func (s *Server) GetTaskFileDownloadUrl(w http.ResponseWriter, r *http.Request, projectId int64, fileId int64, params GetTaskFileDownloadUrlParams) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	f, err := s.q.GetTaskFileInProject(r.Context(), store.GetTaskFileInProjectParams{ID: fileId, ProjectID: projectId})
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

// fetchTaskFile 读取任务文件并校验它确实挂在本项目的这个任务上。
func (s *Server) fetchTaskFile(w http.ResponseWriter, r *http.Request, projectID, taskID, fileID int64) (store.GetTaskFileInProjectRow, bool) {
	row, err := s.q.GetTaskFileInProject(r.Context(), store.GetTaskFileInProjectParams{ID: fileID, ProjectID: projectID})
	if err != nil || row.TaskID != taskID {
		if err == nil || errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "file_not_found", Message: "文件不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return store.GetTaskFileInProjectRow{}, false
	}
	return row, true
}

// taskFileList 任务下已确认写入的过程文件与外部材料。
func (s *Server) taskFileList(ctx context.Context, taskID int64) ([]TaskFile, error) {
	rows, err := s.q.ListTaskFilesByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]TaskFile, 0, len(rows))
	for _, f := range rows {
		out = append(out, toTaskFile(store.TaskFile{
			ID: f.ID, TaskID: f.TaskID, Kind: f.Kind, State: f.State, FileName: f.FileName,
			FileType: f.FileType, FileSize: f.FileSize, ObjectKey: f.ObjectKey, Note: f.Note,
			UploadedBy: f.UploadedBy, UploadedAt: f.UploadedAt,
		}, f.UploadedByName))
	}
	return out, nil
}

func toTaskFile(f store.TaskFile, uploadedByName string) TaskFile {
	out := TaskFile{
		Id:             f.ID,
		TaskId:         f.TaskID,
		Kind:           TaskFileKind(f.Kind),
		KindLabel:      domain.TaskFileKindLabel(f.Kind),
		FileName:       f.FileName,
		FileType:       optString(f.FileType),
		Note:           optString(f.Note),
		UploadedByName: optString(uploadedByName),
	}
	if f.FileSize > 0 {
		out.FileSize = &f.FileSize
	}
	if f.UploadedAt.Valid {
		out.UploadedAt = f.UploadedAt.Time
	}
	return out
}

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 指定项目成员输入请求（AC-29、AC-30）。业务规则在 domain，handler 仅编排。

// CreateMemberInput 指定项目成员提供输入：为每名对接人分别建边 + 建输入请求（AC-29；AC-53 可多选对接人）；
// 任务已入池立即逐人通知，否则入池通过后补发。
func (s *Server) CreateMemberInput(w http.ResponseWriter, r *http.Request, projectId int64, taskId int64) {
	var req CreateMemberInputRequest
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
	members, err := s.q.ListProjectMembers(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	roleByID := make(map[int64]string, len(members))
	for _, m := range members {
		roleByID[m.UserID] = m.Role
	}
	input := domain.MemberInputs{
		Necessity:       string(req.Necessity),
		ProviderIDs:     req.ProviderIds,
		ContentNote:     strings.TrimSpace(req.ContentNote),
		HasExpectedDate: !req.ExpectedDate.Time.IsZero(),
	}
	if err := domain.ValidateMemberInputs(input, func(id int64) string { return roleByID[id] }); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_member_input", Message: err.Error()})
		return
	}
	// 输入源是关键字段（§5.2.B、§5.5）：已入池任务指定对接人要经所属 KR 负责人审批，
	// 审批通过后才建边、生成输入请求并发通知。
	outcome, ok := s.routeStructureChange(w, r, taskId, actor, uid, facts)
	if !ok {
		return
	}
	providerNames := make([]string, 0, len(input.ProviderIDs))
	for _, m := range members {
		for _, id := range input.ProviderIDs {
			if m.UserID == id {
				providerNames = append(providerNames, m.DisplayName)
			}
		}
	}
	raw, err := json.Marshal(req)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	payload := structurePayload{
		Op:       domain.StructureAddMemberInput,
		Label:    domain.StructureFieldLabel(domain.StructureAddMemberInput),
		OldValue: "—",
		NewValue: fmt.Sprintf("新增输入源「%s」，对接人：%s",
			domain.EdgeDisplayName("", "", input.ContentNote), strings.Join(providerNames, "、")),
		Request:  raw,
	}
	if !s.commitStructureChange(w, r, projectId, taskId, uid, outcome, payload, payload.NewValue) {
		return
	}
	s.writeTask(w, r, projectId, taskId, uid, actor)
}

func (s *Server) AcceptInputRequest(w http.ResponseWriter, r *http.Request, projectId int64, requestId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	uid := currentUser(r).ID
	ir, err := s.q.GetInputRequestInProject(r.Context(), store.GetInputRequestInProjectParams{ID: requestId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "request_not_found", Message: "输入请求不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	if err := domain.AcceptInputRule(ir.State, ir.ProviderID, uid); err != nil {
		if errors.Is(err, domain.ErrNotProvider) {
			writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: err.Error()})
		} else {
			writeJSON(w, http.StatusConflict, Error{Code: "request_state_conflict", Message: err.Error()})
		}
		return
	}
	updated, err := s.q.AcceptInputRequest(r.Context(), requestId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.inputRequestView(updated, ir.ProviderName, uid))
}

func (s *Server) ProvideInput(w http.ResponseWriter, r *http.Request, projectId int64, requestId int64) {
	var req ProvideInputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	uid := currentUser(r).ID
	ir, err := s.q.GetInputRequestInProject(r.Context(), store.GetInputRequestInProjectParams{ID: requestId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "request_not_found", Message: "输入请求不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	text := ""
	if req.Text != nil {
		text = strings.TrimSpace(*req.Text)
	}
	fileName := ""
	if req.FileName != nil {
		fileName = strings.TrimSpace(*req.FileName)
	}
	if err := domain.ProvideInputRule(ir.State, ir.ProviderID, uid, text != "" || fileName != ""); err != nil {
		switch {
		case errors.Is(err, domain.ErrNotProvider):
			writeJSON(w, http.StatusForbidden, Error{Code: "forbidden", Message: err.Error()})
		case errors.Is(err, domain.ErrInputContentRequired):
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "content_required", Message: err.Error()})
		default:
			writeJSON(w, http.StatusConflict, Error{Code: "request_state_conflict", Message: err.Error()})
		}
		return
	}
	objectKey := ""
	uploadURL := ""
	if fileName != "" {
		if err := domain.ValidateCandidateFileName(fileName); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_file", Message: err.Error()})
			return
		}
		objectKey = fmt.Sprintf("input-requests/%d/%d-%s", requestId, s.now().UnixNano(), sanitizeObjectName(fileName))
		uploadURL, err = s.files.PresignPut(r.Context(), objectKey, presignExpiry)
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
	}
	// 带附件时先停在 uploading：预签名直传绕过服务端，不等确认就置 provided 等于
	// 「没传也算已提供」，下游等待输入会被错误解除（R4）。
	newState := domain.InputRequestProvided
	if fileName != "" {
		newState = domain.InputRequestUploading
	}
	updated, err := s.q.ProvideInputRequest(r.Context(), store.ProvideInputRequestParams{
		ID: requestId, ProvidedText: text, FileName: fileName, ObjectKey: objectKey, State: newState,
	})
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	resp := ProvideInputResponse{Request: s.inputRequestView(updated, ir.ProviderName, uid)}
	if uploadURL != "" {
		resp.UploadUrl = &uploadURL
	}
	writeJSON(w, http.StatusOK, resp)
}

// CommitInputRequestFile 两阶段提交第二步：校验附件确已写入对象存储后，uploading → provided（R4）。
func (s *Server) CommitInputRequestFile(w http.ResponseWriter, r *http.Request, projectId int64, requestId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	uid := currentUser(r).ID
	ir, err := s.q.GetInputRequestInProject(r.Context(), store.GetInputRequestInProjectParams{ID: requestId, ProjectID: projectId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "request_not_found", Message: "输入请求不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return
	}
	if ir.ProviderID != uid {
		writeForbidden(w)
		return
	}
	if ir.State != domain.InputRequestUploading || ir.ObjectKey == "" {
		writeJSON(w, http.StatusConflict, Error{Code: "upload_state_conflict", Message: "没有待确认的上传记录"})
		return
	}
	size, err := s.files.Stat(r.Context(), ir.ObjectKey)
	if err != nil {
		writeJSON(w, http.StatusConflict, Error{Code: "upload_not_found", Message: "文件尚未上传完成，请重试"})
		return
	}
	if err := domain.ValidateUploadSize(size); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_file", Message: err.Error()})
		return
	}
	updated, err := s.q.CommitInputRequestUpload(r.Context(), requestId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.inputRequestView(updated, ir.ProviderName, uid))
}

func (s *Server) GetInputRequestFileUrl(w http.ResponseWriter, r *http.Request, projectId int64, requestId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	ir, err := s.q.GetInputRequestInProject(r.Context(), store.GetInputRequestInProjectParams{ID: requestId, ProjectID: projectId})
	if err != nil || ir.ObjectKey == "" {
		writeJSON(w, http.StatusNotFound, Error{Code: "file_not_found", Message: "文件不存在"})
		return
	}
	url, err := s.files.PresignGet(r.Context(), ir.ObjectKey, ir.FileName, presignExpiry)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, DownloadUrlResponse{Url: url})
}

// notifyPendingInputRequests 入池审批通过后补发对接人通知（AC-29；§7.3 首次入池通过后发送）。
func (s *Server) notifyPendingInputRequests(r *http.Request, projectID, taskID int64, taskName string) {
	rows, err := s.q.ListUnnotifiedInputRequestsByTask(r.Context(), taskID)
	if err != nil {
		return
	}
	for _, ir := range rows {
		_, err := s.q.CreateNotification(r.Context(), store.CreateNotificationParams{
			UserID:    ir.ProviderID,
			Kind:      domain.NotifyInputRequest,
			Content:   fmt.Sprintf("请你为任务「%s」提供输入「%s」：%s", taskName, ir.EdgeName, ir.ContentNote),
			ProjectID: pgtype.Int8{Int64: projectID, Valid: true},
			TaskID:    pgtype.Int8{Int64: taskID, Valid: true},
		})
		if err == nil {
			_ = s.q.MarkInputRequestNotified(r.Context(), ir.ID)
		}
	}
}

// inputRequestView 组装契约 InputRequest（派生动作标志）。
func (s *Server) inputRequestView(ir store.InputRequest, providerName string, userID int64) InputRequest {
	canAccept := domain.AcceptInputRule(ir.State, ir.ProviderID, userID) == nil
	canProvide := domain.ProvideInputRule(ir.State, ir.ProviderID, userID, true) == nil
	out := InputRequest{
		Id:           ir.ID,
		EdgeId:       ir.EdgeID,
		ProviderId:   ir.ProviderID,
		ProviderName: providerName,
		ContentNote:  optString(ir.ContentNote),
		State:        InputRequestState(ir.State),
		ProvidedText: optString(ir.ProvidedText),
		CanAccept:    &canAccept,
		CanProvide:   &canProvide,
	}
	if ir.FileName != "" {
		out.ProvidedFileName = &ir.FileName
	}
	if ir.NotifiedAt.Valid {
		out.NotifiedAt = &ir.NotifiedAt.Time
	}
	if ir.AcceptedAt.Valid {
		out.AcceptedAt = &ir.AcceptedAt.Time
	}
	if ir.ProvidedAt.Valid {
		out.ProvidedAt = &ir.ProvidedAt.Time
	}
	return out
}

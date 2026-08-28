package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// O／KR 的编辑、删除与负责人交接（AC-61、AC-65；PRD §7.2）。业务规则在 domain，handler 仅编排。

// UpdateObjective 编辑 O：仅项目管理员。
func (s *Server) UpdateObjective(w http.ResponseWriter, r *http.Request, projectId int64, objectiveId int64) {
	var req UpdateObjectiveRequest
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
	if !domain.CanEditObjective(actor) {
		writeForbidden(w)
		return
	}
	obj, ok := s.fetchObjective(w, r, projectId, objectiveId)
	if !ok {
		return
	}
	update := domain.ObjectiveUpdate{Title: trimmedPtr(req.Title), Description: trimmedPtr(req.Description)}
	if err := domain.ValidateObjectiveUpdate(update); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_okr", Message: err.Error()})
		return
	}
	if update.Empty() {
		s.writeObjective(w, r, projectId, obj.ID, actor, uid)
		return
	}
	if _, err := s.q.UpdateObjective(r.Context(), store.UpdateObjectiveParams{
		ID: objectiveId, ProjectID: projectId,
		Title:       toPgTextPtr(update.Title),
		Description: toPgTextPtr(update.Description),
	}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.writeObjective(w, r, projectId, objectiveId, actor, uid)
}

// DeleteObjective 删除 O：仅项目管理员，且 O 下没有 KR。
func (s *Server) DeleteObjective(w http.ResponseWriter, r *http.Request, projectId int64, objectiveId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	if _, ok := s.fetchObjective(w, r, projectId, objectiveId); !ok {
		return
	}
	n, err := s.q.CountKeyResultsByObjective(r.Context(), objectiveId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := domain.DeleteObjectiveRule(actor, int(n)); err != nil {
		if errors.Is(err, domain.ErrOkrDeleteForbidden) {
			writeForbidden(w)
		} else {
			writeJSON(w, http.StatusConflict, Error{Code: "okr_has_children", Message: err.Error()})
		}
		return
	}
	if _, err := s.q.DeleteObjective(r.Context(), store.DeleteObjectiveParams{ID: objectiveId, ProjectID: projectId}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetKrHandoverPreview 更换 KR 负责人前的确认信息：该 KR 下未决审批单条数（AC-61）。
func (s *Server) GetKrHandoverPreview(w http.ResponseWriter, r *http.Request, projectId int64, keyResultId int64) {
	if _, ok := s.fetchProject(w, r, projectId); !ok {
		return
	}
	if _, ok := s.fetchKeyResult(w, r, projectId, keyResultId); !ok {
		return
	}
	n, err := s.q.CountPendingApprovalsByKeyResult(r.Context(), keyResultId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, KrHandoverPreview{PendingApprovals: int(n)})
}

// UpdateKeyResult 编辑 KR：项目管理员或本 KR 负责人；负责人不可置空，
// 更换负责人时按 transferPendingApprovals 决定未决审批单是否转交继任者（AC-61）。
func (s *Server) UpdateKeyResult(w http.ResponseWriter, r *http.Request, projectId int64, keyResultId int64) {
	var req UpdateKeyResultRequest
	raw, err := readAllBody(r)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_request", Message: "请求内容无法解析"})
		return
	}
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	kr, ok := s.fetchKeyResult(w, r, projectId, keyResultId)
	if !ok {
		return
	}
	if !domain.CanEditKeyResult(actor, uid, fromPgInt8(kr.OwnerID)) {
		writeForbidden(w)
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
	update := domain.KeyResultUpdate{
		Description: trimmedPtr(req.Description),
		Metric:      trimmedPtr(req.Metric),
		OwnerID:     req.OwnerId,
		// 契约里 ownerId 缺省＝不改；显式传 null 才是「置空」，须被拒（AC-61）。
		ClearOwner: jsonFieldIsNull(raw, "ownerId"),
		Start:      toTimePtr(req.StartDate),
		End:        toTimePtr(req.EndDate),
	}
	// 周期只改一端时，另一端取库里现值参与倒挂校验。
	start, end := update.Start, update.End
	if start == nil {
		start = pgDateAsTime(kr.StartDate)
	}
	if end == nil {
		end = pgDateAsTime(kr.EndDate)
	}
	check := update
	check.Start, check.End = start, end
	if err := domain.ValidateKeyResultUpdate(check, func(id int64) string { return roleByID[id] }); err != nil {
		code := "invalid_okr"
		if errors.Is(err, domain.ErrKrOwnerRequired) {
			code = "kr_owner_required"
		}
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: code, Message: err.Error()})
		return
	}
	handover := update.OwnerID != nil && (!kr.OwnerID.Valid || kr.OwnerID.Int64 != *update.OwnerID)
	transfer := handover && (req.TransferPendingApprovals == nil || *req.TransferPendingApprovals)

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.UpdateKeyResult(r.Context(), store.UpdateKeyResultParams{
		ID:          keyResultId,
		Description: toPgTextPtr(update.Description),
		Metric:      toPgTextPtr(update.Metric),
		OwnerID:     toPgInt8(update.OwnerID),
		StartDate:   toPgDateFromTime(update.Start),
		EndDate:     toPgDateFromTime(update.End),
	}); err != nil {
		writeInternalError(w, r, err)
		return
	}
	pending := int32(0)
	if handover {
		// 未决审批单跟着 KR 负责人走：审批人本就是「所属 KR 负责人」这一职责，
		// 不转交就得让离任者继续处理，转交与否由发起人在确认框里定（AC-61）。
		if pending, err = qtx.CountPendingApprovalsByKeyResult(r.Context(), keyResultId); err != nil {
			writeInternalError(w, r, err)
			return
		}
		if transfer && pending > 0 {
			if _, err := qtx.CreateNotification(r.Context(), store.CreateNotificationParams{
				UserID:    *update.OwnerID,
				Kind:      domain.NotifyKrHandover,
				Content:   fmt.Sprintf("你已接任 KR「%s」负责人，%d 件未决审批已转交你处理", krDescriptionAfter(kr, update), pending),
				ProjectID: pgtype.Int8{Int64: projectId, Valid: true},
			}); err != nil {
				writeInternalError(w, r, err)
				return
			}
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInternalError(w, r, err)
		return
	}
	s.writeKeyResult(w, r, projectId, keyResultId, actor, uid)
}

// DeleteKeyResult 删除 KR：仅项目管理员，且 KR 下没有任务（含已完成、已取消）。
func (s *Server) DeleteKeyResult(w http.ResponseWriter, r *http.Request, projectId int64, keyResultId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	if _, ok := s.fetchKeyResult(w, r, projectId, keyResultId); !ok {
		return
	}
	counts, err := s.taskCountByKeyResult(r.Context(), projectId)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := domain.DeleteKeyResultRule(actor, counts[keyResultId]); err != nil {
		if errors.Is(err, domain.ErrOkrDeleteForbidden) {
			writeForbidden(w)
		} else {
			writeJSON(w, http.StatusConflict, Error{Code: "okr_has_children", Message: err.Error()})
		}
		return
	}
	if _, err := s.q.DeleteKeyResult(r.Context(), keyResultId); err != nil {
		writeInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// —— 编排辅助 ——

func (s *Server) fetchObjective(w http.ResponseWriter, r *http.Request, projectID, objectiveID int64) (store.Objective, bool) {
	obj, err := s.q.GetObjective(r.Context(), store.GetObjectiveParams{ID: objectiveID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "objective_not_found", Message: "O 不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return store.Objective{}, false
	}
	return obj, true
}

func (s *Server) fetchKeyResult(w http.ResponseWriter, r *http.Request, projectID, keyResultID int64) (store.GetKeyResultInProjectRow, bool) {
	kr, err := s.q.GetKeyResultInProject(r.Context(), store.GetKeyResultInProjectParams{ID: keyResultID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "key_result_not_found", Message: "KR 不存在"})
		} else {
			writeInternalError(w, r, err)
		}
		return store.GetKeyResultInProjectRow{}, false
	}
	return kr, true
}

// writeObjective 回写单个 O（复用 okrList 的派生字段口径）。
func (s *Server) writeObjective(w http.ResponseWriter, r *http.Request, projectID, objectiveID int64, actor domain.Actor, userID int64) {
	list, err := s.okrList(r.Context(), projectID, actor, userID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	for _, o := range list {
		if o.Id == objectiveID {
			writeJSON(w, http.StatusOK, o)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, Error{Code: "objective_not_found", Message: "O 不存在"})
}

// writeKeyResult 回写单个 KR（复用 okrList 的派生字段口径）。
func (s *Server) writeKeyResult(w http.ResponseWriter, r *http.Request, projectID, keyResultID int64, actor domain.Actor, userID int64) {
	list, err := s.okrList(r.Context(), projectID, actor, userID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	for _, o := range list {
		for _, k := range o.KeyResults {
			if k.Id == keyResultID {
				writeJSON(w, http.StatusOK, k)
				return
			}
		}
	}
	writeJSON(w, http.StatusNotFound, Error{Code: "key_result_not_found", Message: "KR 不存在"})
}

func krDescriptionAfter(kr store.GetKeyResultInProjectRow, u domain.KeyResultUpdate) string {
	if u.Description != nil {
		return *u.Description
	}
	return kr.Description
}

func trimmedPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	return &v
}

// readAllBody 读出整个请求体：UpdateKeyResult 需要在解码之外再判断 ownerId 是否被显式置空。
func readAllBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

// jsonFieldIsNull 报告 JSON 对象里某个顶层字段是否被显式写成 null。
func jsonFieldIsNull(raw []byte, field string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	v, ok := m[field]
	return ok && string(v) == "null"
}

// taskCountByKeyResult 每个 KR 下的任务数（含已完成与已取消，AC-65）。
func (s *Server) taskCountByKeyResult(ctx context.Context, projectID int64) (map[int64]int, error) {
	rows, err := s.q.CountTasksByKeyResultInProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, row := range rows {
		out[row.KeyResultID] = int(row.N)
	}
	return out, nil
}

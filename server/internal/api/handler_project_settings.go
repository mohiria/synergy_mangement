package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"synergy/server/internal/domain"
	"synergy/server/internal/store"
)

// 项目规则设置（主 PRD §7.9、我的工作 PRD §8.8；AC-60）。
// 三项阈值按项目生效、仅项目管理员可改；卡点派生、我的工作与一键提醒冷却读同一份值。

// GetProjectSettings 读取本项目的规则设置；项目成员均可读，canEdit 派生给前端控显隐。
func (s *Server) GetProjectSettings(w http.ResponseWriter, r *http.Request, projectId int64) {
	proj, ok := s.fetchProject(w, r, projectId)
	if !ok {
		return
	}
	uid := currentUser(r).ID
	actor := projectActor(uid, proj.OwnerID, proj.MyRole)
	writeJSON(w, http.StatusOK, toProjectSettings(projectSettingsOf(proj), actor))
}

// UpdateProjectSettings 修改规则设置（全量三项）。
func (s *Server) UpdateProjectSettings(w http.ResponseWriter, r *http.Request, projectId int64) {
	var req UpdateProjectSettingsRequest
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
	if !domain.CanEditProjectSettings(actor) {
		writeForbidden(w)
		return
	}
	settings := domain.ProjectSettings{
		ApprovalTimeoutDays: req.ApprovalTimeoutDays,
		DueSoonDays:         req.DueSoonDays,
		RemindDailyLimit:    req.RemindDailyLimit,
	}
	if err := domain.ValidateProjectSettings(settings); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, Error{Code: "invalid_project_settings", Message: err.Error()})
		return
	}
	row, err := s.q.UpdateProjectSettings(r.Context(), store.UpdateProjectSettingsParams{
		ID:                  projectId,
		ApprovalTimeoutDays: int32(settings.ApprovalTimeoutDays),
		DueSoonDays:         int32(settings.DueSoonDays),
		RemindDailyLimit:    int32(settings.RemindDailyLimit),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, Error{Code: "not_found", Message: "项目不存在"})
			return
		}
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProjectSettings(domain.NormalizeProjectSettings(domain.ProjectSettings{
		ApprovalTimeoutDays: int(row.ApprovalTimeoutDays),
		DueSoonDays:         int(row.DueSoonDays),
		RemindDailyLimit:    int(row.RemindDailyLimit),
	}), actor))
}

// projectSettingsOf 从已读到的项目行取规则设置，缺值回落默认。
func projectSettingsOf(p store.GetProjectRow) domain.ProjectSettings {
	return domain.NormalizeProjectSettings(domain.ProjectSettings{
		ApprovalTimeoutDays: int(p.ApprovalTimeoutDays),
		DueSoonDays:         int(p.DueSoonDays),
		RemindDailyLimit:    int(p.RemindDailyLimit),
	})
}

// projectSettings 供不持有项目行的派生路径（卡点、提醒目标、ticker）读同一份阈值。
func (s *Server) projectSettings(ctx context.Context, projectID int64) (domain.ProjectSettings, error) {
	row, err := s.q.GetProjectSettings(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.DefaultProjectSettings(), nil
		}
		return domain.ProjectSettings{}, err
	}
	return domain.NormalizeProjectSettings(domain.ProjectSettings{
		ApprovalTimeoutDays: int(row.ApprovalTimeoutDays),
		DueSoonDays:         int(row.DueSoonDays),
		RemindDailyLimit:    int(row.RemindDailyLimit),
	}), nil
}

func toProjectSettings(s domain.ProjectSettings, actor domain.Actor) ProjectSettings {
	return ProjectSettings{
		ApprovalTimeoutDays: s.ApprovalTimeoutDays,
		DueSoonDays:         s.DueSoonDays,
		RemindDailyLimit:    s.RemindDailyLimit,
		CanEdit:             domain.CanEditProjectSettings(actor),
	}
}

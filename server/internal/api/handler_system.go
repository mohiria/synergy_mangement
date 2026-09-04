package api

import (
	"net/http"

	"synergy/server/internal/domain"
)

// requireSystemAdmin 系统设置准入（#201）：非系统管理员一律 403，规则在 domain。
func requireSystemAdmin(w http.ResponseWriter, r *http.Request) bool {
	if err := domain.CanAccessSystemSettings(currentUser(r).IsSystemAdmin); err != nil {
		writeJSON(w, http.StatusForbidden, Error{Code: "system_admin_required", Message: err.Error()})
		return false
	}
	return true
}

// ListSystemUsers 系统设置 → 用户管理的只读列表（#201）。
func (s *Server) ListSystemUsers(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	rows, err := s.q.ListSystemUsers(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	resp := make([]SystemUser, 0, len(rows))
	for _, u := range rows {
		resp = append(resp, SystemUser{
			Id: u.ID, Username: u.Username, DisplayName: u.DisplayName,
			IsSystemAdmin: u.IsSystemAdmin, CreatedAt: u.CreatedAt.Time,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

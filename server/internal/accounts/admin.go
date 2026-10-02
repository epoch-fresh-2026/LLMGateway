package accounts

import (
	"net/http"
	"strconv"

	"LLMGateway/server/internal/httpcommon"
)

// RegisterAdminRoutes registers account management routes on mux.
func (a *Server) RegisterAdminRoutes(mux *http.ServeMux) {
	httpcommon.HandleAdmin(mux, "/admin/profile", a.profile)
	httpcommon.HandleAdmin(mux, "/admin/keys", a.keys)
	httpcommon.HandleAdmin(mux, "/admin/keys/{id}", a.key)
	a.registerAuthRoutes(mux)
}

func (a *Server) profile(r *http.Request) httpcommon.AdminResult {
	userID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	switch r.Method {
	case http.MethodGet:
		return httpcommon.Result(a.Profile(r.Context(), userID))
	case http.MethodPut:
		var req ProfileUpdateInput
		if err := httpcommon.ReadJSON(r, &req); err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
		}
		account, err := a.UpdateProfile(r.Context(), userID, req)
		if err != nil {
			return authError(err)
		}
		return httpcommon.Handled(account)
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func parseKeyID(r *http.Request) (int, httpcommon.AdminResult) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		return 0, httpcommon.HTTPError(http.StatusBadRequest, "invalid key id")
	}
	return id, httpcommon.AdminResult{}
}

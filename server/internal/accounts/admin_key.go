package accounts

import (
	"net/http"

	"LLMGateway/server/internal/httpcommon"
)

func (a *Server) keys(r *http.Request) httpcommon.AdminResult {
	userID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	switch r.Method {
	case http.MethodGet:
		page, pageSize := httpcommon.ParsePagination(r)
		return httpcommon.Result(a.store.ListKeys(r.Context(), userID, page, pageSize))
	case http.MethodPost:
		var req KeyInput
		if err := httpcommon.ReadJSON(r, &req); err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
		}
		return httpcommon.Result(a.CreateKey(r.Context(), userID, req))
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *Server) key(r *http.Request) httpcommon.AdminResult {
	userID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	keyID, result := parseKeyID(r)
	if result.Status != 0 {
		return result
	}
	switch r.Method {
	case http.MethodPut:
		var req KeyUpdateInput
		if err := httpcommon.ReadJSON(r, &req); err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
		}
		return httpcommon.Result(a.UpdateKey(r.Context(), userID, keyID, req))
	case http.MethodDelete:
		return httpcommon.NoBody(a.DeleteKey(r.Context(), userID, keyID))
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

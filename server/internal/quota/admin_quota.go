package quota

import (
	"net/http"
	"strconv"

	"LLMGateway/server/internal/httpcommon"
)

func (a *Server) RegisterAdminRoutes(mux *http.ServeMux) {
	httpcommon.HandleAdmin(mux, "/admin/quota-usage", a.quotaUsage)
	httpcommon.HandleAdmin(mux, "/admin/quota-policies", a.quotaPolicies)
	httpcommon.HandleAdmin(mux, "/admin/quota-policies/{id}", a.quotaPolicy)
}

func (a *Server) quotaUsage(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodGet {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	return a.listUsage(r)
}

func (a *Server) quotaPolicies(r *http.Request) httpcommon.AdminResult {
	switch r.Method {
	case http.MethodGet:
		return a.listPolicies(r)
	case http.MethodPost:
		return a.createPolicy(r)
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *Server) quotaPolicy(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid policy id")
	}
	switch r.Method {
	case http.MethodDelete:
		return httpcommon.NoBody(a.DeleteQuotaPolicy(r.Context(), ownerUserID, id))
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *Server) listPolicies(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	page, pageSize := httpcommon.ParsePagination(r)
	filter := QuotaPolicyFilter{OwnerUserID: ownerUserID, ScopeType: r.URL.Query().Get("scope_type"), Page: page, PageSize: pageSize}
	filter.ScopeID, _ = strconv.Atoi(r.URL.Query().Get("scope_id"))
	if value := r.URL.Query().Get("enabled"); value == "true" || value == "false" {
		enabled := value == "true"
		filter.Enabled = &enabled
	}
	return httpcommon.Result(a.ListQuotaPolicies(r.Context(), filter))
}

func (a *Server) listUsage(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	page, pageSize := httpcommon.ParsePagination(r)
	filter := QuotaPolicyFilter{OwnerUserID: ownerUserID, ScopeType: r.URL.Query().Get("scope_type"), Page: page, PageSize: pageSize}
	filter.ScopeID, _ = strconv.Atoi(r.URL.Query().Get("scope_id"))
	return httpcommon.Result(a.ListQuotaUsage(r.Context(), filter))
}

func (a *Server) createPolicy(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	var input QuotaPolicyInput
	if err := httpcommon.ReadJSON(r, &input); err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
	}
	return httpcommon.Result(a.CreateQuotaPolicy(r.Context(), ownerUserID, input))
}

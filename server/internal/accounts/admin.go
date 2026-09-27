package accounts

import (
	"net/http"
	"strconv"

	"LLMGateway/server/internal/httpcommon"
)

// RegisterAdminRoutes registers account management routes on mux.
func (a *Server) RegisterAdminRoutes(mux *http.ServeMux) {
	httpcommon.HandleAdmin(mux, "/admin/users", a.users)
	httpcommon.HandleAdmin(mux, "/admin/users/{id}", a.user)
	httpcommon.HandleAdmin(mux, "/admin/users/{id}/status", a.userStatus)
	httpcommon.HandleAdmin(mux, "/admin/users/{id}/recharge", a.userRecharge)
	httpcommon.HandleAdmin(mux, "/admin/users/{id}/balance", a.userBalance)
	httpcommon.HandleAdmin(mux, "/admin/users/{id}/balance-transactions", a.userBalanceTransactions)
	httpcommon.HandleAdmin(mux, "/admin/users/{id}/keys", a.userKeys)
	httpcommon.HandleAdmin(mux, "/admin/users/{id}/keys/{key_id}", a.userKey)
	httpcommon.HandleAdmin(mux, "/admin/users/{id}/keys/{key_id}/reset", a.userKeyReset)
	httpcommon.HandleAdmin(mux, "/admin/keys", a.keys)
	a.registerAuthRoutes(mux)
}

func (a *Server) users(r *http.Request) httpcommon.AdminResult {
	switch r.Method {
	case http.MethodGet:
		return a.listUsers(r)
	case http.MethodPost:
		return a.createUser(r)
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *Server) user(r *http.Request) httpcommon.AdminResult {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid user id")
	}
	switch r.Method {
	case http.MethodPut:
		return a.updateUser(r, id)
	case http.MethodDelete:
		return httpcommon.NoBody(a.DeleteUser(r.Context(), id))
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *Server) userStatus(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodPut {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid user id")
	}
	return a.updateUserStatus(r, id)
}

func (a *Server) userRecharge(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodPost {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid user id")
	}
	return a.rechargeUser(r, id)
}

func (a *Server) userBalance(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodGet {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid user id")
	}
	return httpcommon.Result(a.store.GetUserBalance(r.Context(), id))
}

func (a *Server) userBalanceTransactions(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodGet {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid user id")
	}
	return a.listBalanceTransactions(r, id)
}

package catalog

import (
	"net/http"

	"LLMGateway/server/internal/httpcommon"
)

func (a *Server) channels(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	switch r.Method {
	case http.MethodGet:
		return httpcommon.Result(a.ListChannels(r.Context(), ownerUserID))
	case http.MethodPost:
		return a.createChannel(r, ownerUserID)
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *Server) channel(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	id, result := parseID(r.PathValue("id"), "channel")
	if result.Status != 0 {
		return result
	}
	switch r.Method {
	case http.MethodPut:
		return a.updateChannel(r, ownerUserID, id)
	case http.MethodDelete:
		return httpcommon.NoBody(a.DeleteChannel(r.Context(), ownerUserID, id))
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *Server) channelStatus(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodPut {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	id, result := parseID(r.PathValue("id"), "channel")
	if result.Status != 0 {
		return result
	}
	return a.updateChannelStatus(r, ownerUserID, id)
}

func (a *Server) channelBalance(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodPut {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	id, result := parseID(r.PathValue("id"), "channel")
	if result.Status != 0 {
		return result
	}
	return a.updateChannelBalance(r, ownerUserID, id)
}

func (a *Server) createChannel(r *http.Request, ownerUserID int) httpcommon.AdminResult {
	var req ChannelInput
	if err := httpcommon.ReadJSON(r, &req); err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
	}
	return httpcommon.Result(a.CreateChannel(r.Context(), ownerUserID, req))
}

func (a *Server) updateChannel(r *http.Request, ownerUserID, id int) httpcommon.AdminResult {
	var req ChannelInput
	if err := httpcommon.ReadJSON(r, &req); err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
	}
	return httpcommon.Result(a.UpdateChannel(r.Context(), ownerUserID, id, req))
}

func (a *Server) updateChannelStatus(r *http.Request, ownerUserID, id int) httpcommon.AdminResult {
	var req struct {
		Status int `json:"status"`
	}
	if err := httpcommon.ReadJSON(r, &req); err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
	}
	return httpcommon.Result(a.UpdateChannelStatus(r.Context(), ownerUserID, id, req.Status))
}

func (a *Server) updateChannelBalance(r *http.Request, ownerUserID, id int) httpcommon.AdminResult {
	var req struct {
		Balance string `json:"balance"`
		Delta   string `json:"delta"`
	}
	if err := httpcommon.ReadJSON(r, &req); err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
	}
	return httpcommon.Result(a.UpdateChannelBalance(r.Context(), ownerUserID, id, req.Balance, req.Delta))
}

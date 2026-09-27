package catalog

import (
	"net/http"

	"LLMGateway/server/internal/httpcommon"
)

func (a *Server) models(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodGet {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	return httpcommon.Result(a.ListCatalogModels(r.Context(), ownerUserID, r.URL.Query().Get("status") == "1"))
}

func (a *Server) channelTest(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodPost {
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
	return a.testChannel(r, ownerUserID, id)
}

func (a *Server) channelRemoteModels(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodPost {
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
	return a.remoteModels(r.Context(), ownerUserID, id)
}

func (a *Server) channelModels(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	id, result := parseID(r.PathValue("id"), "channel")
	if result.Status != 0 {
		return result
	}
	if r.Method == http.MethodGet {
		return httpcommon.Result(a.ListChannelModels(r.Context(), ownerUserID, id))
	}
	if r.Method == http.MethodPost {
		var req ChannelModel
		if err := httpcommon.ReadJSON(r, &req); err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
		}
		return httpcommon.Result(a.CreateChannelModel(r.Context(), ownerUserID, id, req))
	}
	return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
}

func (a *Server) channelModel(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	id, result := parseID(r.PathValue("id"), "channel")
	if result.Status != 0 {
		return result
	}
	modelID, result := parseID(r.PathValue("model_id"), "model")
	if result.Status != 0 {
		return result
	}
	switch r.Method {
	case http.MethodPut:
		var req struct {
			UpstreamModel string `json:"upstream_model"`
			Enabled       bool   `json:"enabled"`
		}
		if err := httpcommon.ReadJSON(r, &req); err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
		}
		return httpcommon.Result(a.UpdateChannelModel(r.Context(), ownerUserID, id, modelID, req.UpstreamModel, req.Enabled))
	case http.MethodDelete:
		return httpcommon.NoBody(a.DeleteChannelModel(r.Context(), ownerUserID, id, modelID))
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

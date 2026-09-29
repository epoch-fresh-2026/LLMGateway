package catalog

import (
	"net/http"

	"LLMGateway/server/internal/httpcommon"
)

func (a *Server) channelHealth(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodGet {
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
	if _, err := a.GetChannelSecret(r.Context(), ownerUserID, id); err != nil {
		return httpcommon.Result(nil, err)
	}
	health, err := a.GetChannelHealth(r.Context(), id)
	if err != nil {
		return httpcommon.Result(nil, err)
	}
	return httpcommon.Result(ChannelHealthDTO{ChannelID: health.ChannelID, State: string(health.State), ConsecutiveFailures: health.ConsecutiveFailures, SuccessCount: health.SuccessCount, FailureCount: health.FailureCount, OpenedAt: health.OpenedAt, UpdatedAt: health.UpdatedAt}, nil)
}

func (a *Server) channelHealthReset(r *http.Request) httpcommon.AdminResult {
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
	if _, err := a.GetChannelSecret(r.Context(), ownerUserID, id); err != nil {
		return httpcommon.NoBody(err)
	}
	return httpcommon.NoBody(a.ResetChannelHealth(r.Context(), id))
}

func (a *Server) channelHealthList(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodGet {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	return httpcommon.Result(a.ListChannelHealth(r.Context(), ownerUserID))
}

// userBreakerConfig serves the owner-level breaker default that channels
// inherit when they have no per-channel override.
func (a *Server) userBreakerConfig(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	switch r.Method {
	case http.MethodGet:
		return httpcommon.Result(a.GetUserBreakerConfig(r.Context(), ownerUserID))
	case http.MethodPut:
		var req ChannelBreakerConfigInput
		if err := httpcommon.ReadJSON(r, &req); err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
		}
		return httpcommon.Result(a.UpdateUserBreakerConfig(r.Context(), ownerUserID, req))
	case http.MethodDelete:
		return httpcommon.NoBody(a.DeleteUserBreakerConfig(r.Context(), ownerUserID))
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *Server) channelBreakerConfig(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	id, result := parseID(r.PathValue("id"), "channel")
	if result.Status != 0 {
		return result
	}
	switch r.Method {
	case http.MethodGet:
		return httpcommon.Result(a.GetChannelBreakerConfig(r.Context(), ownerUserID, id))
	case http.MethodPut:
		var req ChannelBreakerConfigInput
		if err := httpcommon.ReadJSON(r, &req); err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
		}
		return httpcommon.Result(a.UpdateChannelBreakerConfig(r.Context(), ownerUserID, id, req))
	case http.MethodDelete:
		return httpcommon.NoBody(a.DeleteChannelBreakerConfig(r.Context(), ownerUserID, id))
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

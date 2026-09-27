package catalog

import (
	"net/http"
	"strconv"

	"LLMGateway/server/internal/httpcommon"
)

// RegisterAdminRoutes registers catalog management routes on mux.
func (a *Server) RegisterAdminRoutes(mux *http.ServeMux) {
	httpcommon.HandleAdmin(mux, "/admin/models", a.models)
	httpcommon.HandleAdmin(mux, "/admin/channels", a.channels)
	httpcommon.HandleAdmin(mux, "/admin/channels/{id}", a.channel)
	httpcommon.HandleAdmin(mux, "/admin/channels/{id}/status", a.channelStatus)
	httpcommon.HandleAdmin(mux, "/admin/channels/{id}/balance", a.channelBalance)
	httpcommon.HandleAdmin(mux, "/admin/channels/{id}/health", a.channelHealth)
	httpcommon.HandleAdmin(mux, "/admin/channels/{id}/health/reset", a.channelHealthReset)
	httpcommon.HandleAdmin(mux, "/admin/channels/{id}/breaker", a.channelBreakerConfig)
	httpcommon.HandleAdmin(mux, "/admin/channels/{id}/test", a.channelTest)
	httpcommon.HandleAdmin(mux, "/admin/channels/{id}/remote-models", a.channelRemoteModels)
	httpcommon.HandleAdmin(mux, "/admin/channels/{id}/models", a.channelModels)
	httpcommon.HandleAdmin(mux, "/admin/channels/{id}/models/{model_id}", a.channelModel)
	httpcommon.HandleAdmin(mux, "/admin/channels/health", a.channelHealthList)
	httpcommon.HandleAdmin(mux, "/admin/pricing", a.pricingData)
}

func parseID(value, name string) (int, httpcommon.AdminResult) {
	id, err := strconv.Atoi(value)
	if err != nil {
		return 0, httpcommon.HTTPError(http.StatusBadRequest, "invalid "+name+" id")
	}
	return id, httpcommon.AdminResult{}
}

// ownerFromRequest resolves the authenticated user for owner-scoped catalog
// operations. A missing identity is reported as 401.
func ownerFromRequest(r *http.Request) (int, httpcommon.AdminResult) {
	return httpcommon.RequireUserID(r)
}

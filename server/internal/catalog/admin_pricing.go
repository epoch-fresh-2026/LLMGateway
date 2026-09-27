package catalog

import (
	"io"
	"net/http"

	"LLMGateway/server/internal/httpcommon"
)

func (a *Server) pricingData(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := ownerFromRequest(r)
	if result.Status != 0 {
		return result
	}
	switch r.Method {
	case http.MethodGet:
		return httpcommon.Result(a.ListPricing(r.Context(), ownerUserID))
	case http.MethodPost:
		var req PricingInput
		if err := httpcommon.ReadJSON(r, &req); err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
		}
		return httpcommon.Result(a.UpsertPricing(r.Context(), ownerUserID, req))
	case http.MethodDelete:
		var req DeletePricingInput
		if err := httpcommon.ReadJSON(r, &req); err != nil && err != io.EOF {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
		}
		return httpcommon.NoBody(a.DeletePricing(r.Context(), ownerUserID, req))
	default:
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

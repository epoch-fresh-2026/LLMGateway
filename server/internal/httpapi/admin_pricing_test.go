package httpapi

import (
	"net/http"
	"testing"
)

func TestPricingRejectsInvalidChannelOrModel(t *testing.T) {
	handler := newTestServer()

	res := adminRaw(t, handler, http.MethodPost, "/admin/pricing", map[string]any{"channel_id": 404, "upstream_model": "missing", "input_price_per_1m": "0.10000000", "output_price_per_1m": "0.20000000", "currency": "USD"})
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing channel status = %d, want %d; body=%s", res.Code, http.StatusNotFound, res.Body.String())
	}

	adminDo(t, handler, http.MethodPost, "/admin/channels", map[string]any{"name": "OpenAI", "base_url": "https://api.openai.test", "api_key": "sk-secret", "auth_type": "bearer", "status": 1})
	res = adminRaw(t, handler, http.MethodPost, "/admin/pricing", map[string]any{"channel_id": 1, "upstream_model": "missing", "input_price_per_1m": "0.10000000", "output_price_per_1m": "0.20000000", "currency": "USD"})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing upstream model status = %d, want %d; body=%s", res.Code, http.StatusBadRequest, res.Body.String())
	}
}

package httpapi

import (
	domain "LLMGateway/server/internal/testutil/testtypes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChannelCRUDDoesNotLeakAPIKey(t *testing.T) {
	handler := newTestServer()

	created := adminDo(t, handler, http.MethodPost, "/admin/channels", map[string]any{
		"name": "OpenAI", "base_url": "https://api.openai.test", "api_key": "sk-secret", "auth_type": "bearer", "status": 1, "weight": 100, "priority": 10, "balance": "100.000000",
	})
	assertNoSecret(t, created, "sk-secret")
	channel := created["data"].(map[string]any)
	if channel["id"].(float64) != 1 || channel["model_count"].(float64) != 0 {
		t.Fatalf("unexpected channel: %+v", channel)
	}

	updated := adminDo(t, handler, http.MethodPut, "/admin/channels/1", map[string]any{
		"name": "OpenAI Updated", "base_url": "https://api2.openai.test", "auth_type": "bearer", "status": 1, "weight": 50, "priority": 20, "balance": "",
	})
	assertNoSecret(t, updated, "sk-secret")
	updatedChannel := updated["data"].(map[string]any)
	if updatedChannel["balance"] != nil {
		t.Fatalf("balance = %v, want null", updatedChannel["balance"])
	}

	status := adminDo(t, handler, http.MethodPut, "/admin/channels/1/status", map[string]any{"status": 0})
	if status["data"].(map[string]any)["status"].(float64) != 0 {
		t.Fatalf("status update failed: %+v", status)
	}

	balance := adminDo(t, handler, http.MethodPut, "/admin/channels/1/balance", map[string]any{"balance": "10.000000", "delta": "2.500000"})
	if balance["data"].(map[string]any)["balance"] != "12.500000" {
		t.Fatalf("balance update failed: %+v", balance)
	}

	listed := adminDo(t, handler, http.MethodGet, "/admin/channels", nil)
	assertNoSecret(t, listed, "sk-secret")
	if listed["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("channel list total mismatch: %+v", listed)
	}
}

func TestChannelCreateRequiresAPIKey(t *testing.T) {
	res := adminRaw(t, newTestServer(), http.MethodPost, "/admin/channels", map[string]any{"name": "missing key", "base_url": "https://api.test"})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusBadRequest, res.Body.String())
	}
}

func TestModelMappingsCatalogPricingAndCascadeDelete(t *testing.T) {
	handler := newTestServer()
	adminDo(t, handler, http.MethodPost, "/admin/channels", map[string]any{"name": "OpenAI", "base_url": "https://api.openai.test", "api_key": "sk-secret", "auth_type": "bearer", "status": 1})

	mapping := adminDo(t, handler, http.MethodPost, "/admin/channels/1/models", map[string]any{"model_name": "gpt-4o-mini", "upstream_model": "gpt-4o-mini-up", "enabled": true})
	modelID := int(mapping["data"].(map[string]any)["id"].(float64))

	adminDo(t, handler, http.MethodPut, "/admin/channels/1/models/"+itoa(modelID), map[string]any{"upstream_model": "gpt-4o-mini", "enabled": true})
	catalog := adminDo(t, handler, http.MethodGet, "/admin/models?status=1", nil)
	models := catalog["data"].(map[string]any)["list"].([]any)
	if len(models) != 1 || models[0].(map[string]any)["model_name"] != "gpt-4o-mini" {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
	channels := models[0].(map[string]any)["channels"].([]any)
	if len(channels) != 1 || channels[0].(map[string]any)["channel_name"] != "OpenAI" {
		t.Fatalf("unexpected catalog channels: %+v", channels)
	}

	price := adminDo(t, handler, http.MethodPost, "/admin/pricing", map[string]any{"channel_id": 1, "model_name": "gpt-4o-mini", "input_price_per_1m": "0.15000000", "output_price_per_1m": "0.60000000", "currency": "USD"})
	if price["data"].(map[string]any)["upstream_model"] != "gpt-4o-mini" {
		t.Fatalf("pricing upstream model mismatch: %+v", price)
	}

	adminDo(t, handler, http.MethodDelete, "/admin/pricing", map[string]any{"channel_id": 1, "model_name": "gpt-4o-mini"})
	pricingList := adminDo(t, handler, http.MethodGet, "/admin/pricing", nil)
	if pricingList["data"].(map[string]any)["total"].(float64) != 0 {
		t.Fatalf("pricing delete failed: %+v", pricingList)
	}

	adminDo(t, handler, http.MethodPost, "/admin/pricing", map[string]any{"channel_id": 1, "model_name": "gpt-4o-mini", "input_price_per_1m": "0.15000000", "output_price_per_1m": "0.60000000", "currency": "USD"})
	adminDo(t, handler, http.MethodDelete, "/admin/channels/1", nil)
	if res := adminRaw(t, handler, http.MethodGet, "/admin/channels/1/models", nil); res.Code != http.StatusNotFound {
		t.Fatalf("model mappings should be gone with the channel, status = %d", res.Code)
	}
	if adminDo(t, handler, http.MethodGet, "/admin/pricing", nil)["data"].(map[string]any)["total"].(float64) != 0 {
		t.Fatal("pricing was not cascade deleted")
	}
}

func TestChannelOwnershipIsolation(t *testing.T) {
	handler := newTestServer()

	created := adminRawAs(t, handler, 1, http.MethodPost, "/admin/channels", map[string]any{"name": "mine", "base_url": "https://mine.test", "api_key": "sk", "status": 1})
	if created.Code != http.StatusOK {
		t.Fatalf("owner create status = %d; body=%s", created.Code, created.Body.String())
	}

	// Another user sees none of it and cannot read, update or delete it.
	if res := adminRawAs(t, handler, 2, http.MethodGet, "/admin/channels", nil); res.Code != http.StatusOK {
		t.Fatalf("other list status = %d", res.Code)
	} else if decoded := decodeAdminData(t, res); decoded["total"].(float64) != 0 {
		t.Fatalf("other user sees %v channels, want 0", decoded["total"])
	}
	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/admin/channels/1/health", nil},
		{http.MethodPut, "/admin/channels/1", map[string]any{"name": "x", "base_url": "https://x.test", "status": 1}},
		{http.MethodDelete, "/admin/channels/1", nil},
	} {
		if res := adminRawAs(t, handler, 2, tc.method, tc.path, tc.body); res.Code != http.StatusNotFound {
			t.Fatalf("other user %s %s status = %d, want 404", tc.method, tc.path, res.Code)
		}
	}

	if res := adminRawAs(t, handler, 1, http.MethodGet, "/admin/channels", nil); res.Code != http.StatusOK {
		t.Fatalf("owner list status = %d", res.Code)
	} else if decoded := decodeAdminData(t, res); decoded["total"].(float64) != 1 {
		t.Fatalf("owner sees %v channels, want 1", decoded["total"])
	}
}

func decodeAdminData(t *testing.T, res *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	decodeJSON(t, res, &envelope)
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T", envelope["data"])
	}
	return data
}

func TestRemoteModelsUsesFakeUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer sk-secret" {
			t.Fatalf("unexpected upstream request path=%s auth=%s", r.URL.Path, r.Header.Get("Authorization"))
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]string{{"id": "gpt-4o-mini"}}})
	}))
	defer upstream.Close()

	handler := newTestServer()
	adminDo(t, handler, http.MethodPost, "/admin/channels", map[string]any{"name": "Fake", "base_url": upstream.URL, "api_key": "sk-secret", "auth_type": "bearer", "status": 1})
	remote := adminDo(t, handler, http.MethodPost, "/admin/channels/1/remote-models", map[string]any{})
	data := remote["data"].(map[string]any)
	if data["ok"] != true || len(data["models"].([]any)) != 1 {
		t.Fatalf("unexpected remote models: %+v", remote)
	}
}

func TestChannelTestCallsChatCompletionsForEveryEnabledModel(t *testing.T) {
	var requests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-secret" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model == "" || body.MaxTokens != 1 || len(body.Messages) != 1 || body.Messages[0].Role != "user" || body.Messages[0].Content != "hi" {
			t.Fatalf("unexpected test request: %+v", body)
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": "chatcmpl-test"})
	}))
	defer upstream.Close()

	handler := newTestServer()
	adminDo(t, handler, http.MethodPost, "/admin/channels", map[string]any{"name": "Fake", "base_url": upstream.URL, "api_key": "sk-secret", "auth_type": "bearer", "status": 1})
	adminDo(t, handler, http.MethodPost, "/admin/channels/1/models", map[string]any{"model_name": "public-a", "upstream_model": "upstream-a", "enabled": true})
	adminDo(t, handler, http.MethodPost, "/admin/channels/1/models", map[string]any{"model_name": "public-b", "upstream_model": "upstream-b", "enabled": true})
	adminDo(t, handler, http.MethodPost, "/admin/channels/1/models", map[string]any{"model_name": "disabled", "upstream_model": "disabled", "enabled": false})

	result := adminDo(t, handler, http.MethodPost, "/admin/channels/1/test", map[string]any{"check_all": true})
	items := result["data"].(map[string]any)["list"].([]any)
	if requests != 2 || len(items) != 2 {
		t.Fatalf("requests/items = %d/%d, want 2/2; result=%+v", requests, len(items), result)
	}
	for _, raw := range items {
		item := raw.(map[string]any)
		if item["http_status"].(float64) != http.StatusOK || item["ok"] != true || item["error"] != "" {
			t.Fatalf("unexpected success item: %+v", item)
		}
	}
}

func TestChannelTestReportsUpstreamFailuresWithoutLeakingKey(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("upstream rejected sk-secret"))
			}))
			defer upstream.Close()

			handler := newTestServer()
			adminDo(t, handler, http.MethodPost, "/admin/channels", map[string]any{"name": "Fake", "base_url": upstream.URL, "api_key": "sk-secret", "auth_type": "bearer", "status": 1})
			adminDo(t, handler, http.MethodPost, "/admin/channels/1/models", map[string]any{"model_name": "public", "upstream_model": "upstream", "enabled": true})
			result := adminDo(t, handler, http.MethodPost, "/admin/channels/1/test", map[string]any{"check_all": true})
			item := result["data"].(map[string]any)["list"].([]any)[0].(map[string]any)
			if item["http_status"].(float64) != float64(status) || item["ok"] != false || !strings.Contains(item["error"].(string), "upstream status") {
				t.Fatalf("unexpected failure item: %+v", item)
			}
			assertNoSecret(t, result, "sk-secret")
		})
	}
}

func TestInvalidBalanceReturnsBadRequest(t *testing.T) {
	handler := newTestServer()
	adminDo(t, handler, http.MethodPost, "/admin/channels", map[string]any{"name": "OpenAI", "base_url": "https://api.openai.test", "api_key": "sk-secret", "auth_type": "bearer", "status": 1})

	res := adminRaw(t, handler, http.MethodPut, "/admin/channels/1/balance", map[string]any{"delta": "abc"})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusBadRequest, res.Body.String())
	}
}

func TestDeleteMissingChannelReturnsNotFound(t *testing.T) {
	res := adminRaw(t, newTestServer(), http.MethodDelete, "/admin/channels/404", nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

func TestDeleteMissingModelMappingReturnsNotFound(t *testing.T) {
	handler := newTestServer()
	adminDo(t, handler, http.MethodPost, "/admin/channels", map[string]any{"name": "OpenAI", "base_url": "https://api.openai.test", "api_key": "sk-secret", "auth_type": "bearer", "status": 1})

	res := adminRaw(t, handler, http.MethodDelete, "/admin/channels/1/models/404", nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

func TestChannelHealthSubresourcesAndReset(t *testing.T) {
	handler := newTestServer()
	adminDo(t, handler, http.MethodPost, "/admin/channels", map[string]any{"name": "Healthy", "base_url": "https://healthy.test", "api_key": "secret", "auth_type": "bearer", "status": 1})
	adminDo(t, handler, http.MethodPost, "/admin/channels", map[string]any{"name": "NoHealth", "base_url": "https://no-health.test", "api_key": "secret", "auth_type": "bearer", "status": 0})
	for i := 0; i < 5; i++ {
		if _, err := handler.catalog.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx); err != nil {
			t.Fatal(err)
		}
	}
	single := adminDo(t, handler, http.MethodGet, "/admin/channels/1/health", nil)
	if single["data"].(map[string]any)["state"] != "open" {
		t.Fatalf("single health = %+v", single)
	}
	all := adminDo(t, handler, http.MethodGet, "/admin/channels/health", nil)
	items := all["data"].(map[string]any)["list"].([]any)
	if len(items) != 2 {
		t.Fatalf("health list = %+v", all)
	}
	for _, raw := range items {
		if raw.(map[string]any)["channel_id"].(float64) == 2 && raw.(map[string]any)["state"] != "closed" {
			t.Fatalf("missing health row = %+v", raw)
		}
	}
	adminDo(t, handler, http.MethodPost, "/admin/channels/1/health/reset", nil)
	reset := adminDo(t, handler, http.MethodGet, "/admin/channels/1/health", nil)
	if reset["data"].(map[string]any)["state"] != "closed" {
		t.Fatalf("reset health = %+v", reset)
	}
	if res := adminRaw(t, handler, http.MethodGet, "/admin/channels/404/health", nil); res.Code != http.StatusNotFound {
		t.Fatalf("missing channel health status = %d", res.Code)
	}
}

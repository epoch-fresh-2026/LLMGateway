package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminSuccessEnvelopeContract(t *testing.T) {
	tests := []struct {
		name string
		path string
		list bool
	}{
		{name: "overview", path: "/admin/stats/overview?start_time=2026-09-16T10:00:00Z&end_time=2026-09-16T11:00:00Z"},
		{name: "ttft", path: "/admin/stats/ttft?start_time=2026-09-16T10:00:00Z&end_time=2026-09-16T11:00:00Z"},
		{name: "daily", path: "/admin/stats/daily?date_from=2026-09-10&date_to=2026-09-16&page=1&page_size=100", list: true},
		{name: "channels", path: "/admin/channels?page=1&page_size=100", list: true},
		{name: "channel health", path: "/admin/channels/health", list: true},
		{name: "channel stats", path: "/admin/stats/channels?start_time=2026-09-16T10:00:00Z&end_time=2026-09-16T11:00:00Z", list: true},
		{name: "usage aggregates", path: "/admin/stats/usage?group_by=model&page=1&page_size=100", list: true},
		{name: "usage logs", path: "/admin/usage-logs?page=1&page_size=20", list: true},
		{name: "profile", path: "/admin/profile"},
		{name: "keys", path: "/admin/keys?page=1&page_size=100", list: true},
		{name: "rate limits", path: "/admin/rate-limits?page=1&page_size=100", list: true},
		{name: "models", path: "/admin/models?status=1&page=1&page_size=100", list: true},
		{name: "pricing", path: "/admin/pricing?page=1&page_size=100", list: true},
		{name: "quota policies", path: "/admin/quota-policies?page=1&page_size=100", list: true},
		{name: "quota usage", path: "/admin/quota-usage?page=1&page_size=100", list: true},
	}

	server := newTestServer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := adminRaw(t, server, http.MethodGet, tt.path, nil)
			data := assertAdminSuccess(t, res)
			if tt.list {
				assertAdminList(t, data)
			}
		})
	}
}

func TestAdminMutationResponseContract(t *testing.T) {
	server := newTestServer()

	channel := assertAdminSuccess(t, adminRaw(t, server, http.MethodPost, "/admin/channels", map[string]any{
		"name": "OpenAI", "base_url": "https://api.openai.test", "api_key": "sk-contract-secret", "auth_type": "bearer", "status": 1,
	}))
	assertFields(t, channel, "id", "name", "base_url", "auth_type", "status", "weight", "priority", "balance", "model_count")
	assertAbsentFields(t, channel, "api_key", "api_key_ciphertext")

	mapping := assertAdminSuccess(t, adminRaw(t, server, http.MethodPost, "/admin/channels/1/models", map[string]any{
		"model_name": "gpt-contract", "upstream_model": "gpt-contract-upstream", "enabled": true,
	}))
	assertFields(t, mapping, "id", "model_name", "upstream_model", "enabled")

	pricing := assertAdminSuccess(t, adminRaw(t, server, http.MethodPost, "/admin/pricing", map[string]any{
		"channel_id": 1, "upstream_model": "gpt-contract-upstream", "input_price_per_1m": "0.100000", "output_price_per_1m": "0.200000", "currency": "USD",
	}))
	assertFields(t, pricing, "id", "channel_id", "channel_name", "upstream_model", "input_price_per_1m", "output_price_per_1m", "cached_input_price_per_1m", "currency")
	assertStringFields(t, pricing, "input_price_per_1m", "output_price_per_1m", "cached_input_price_per_1m")

	key := assertAdminSuccess(t, adminRaw(t, server, http.MethodPost, "/admin/keys", map[string]any{"key_name": "contract", "prefix": "sk-"}))
	assertFields(t, key, "id", "full_key")
	fullKey, ok := key["full_key"].(string)
	if !ok || fullKey == "" {
		t.Fatalf("full_key = %v, want non-empty string", key["full_key"])
	}
	assertNoSecret(t, key, "sk-contract-secret")

	rateLimit := assertAdminSuccess(t, adminRaw(t, server, http.MethodPost, "/admin/rate-limits", map[string]any{
		"rule_name": "contract-rpm", "target_type": "user", "metric": "rpm", "limit_value": 60, "action": "reject",
	}))
	assertFields(t, rateLimit, "id", "rule_name", "target_type", "target_value", "metric", "limit_value", "action", "priority", "enabled", "extras")

	quota := assertAdminSuccess(t, adminRaw(t, server, http.MethodPost, "/admin/quota-policies", map[string]any{
		"policy_name": "contract-day", "scope_type": "user", "scope_id": 1, "period_type": "day", "token_limit": 100,
	}))
	assertFields(t, quota, "id", "policy_name", "scope_type", "scope_id", "period_type", "token_limit", "cost_limit", "enabled")

	deleted := assertAdminSuccess(t, adminRaw(t, server, http.MethodDelete, "/admin/rate-limits/1", nil))
	if deleted["deleted"] != true {
		t.Fatalf("delete response = %+v, want deleted=true", deleted)
	}
}

func TestAdminErrorEnvelopeContract(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
		status int
	}{
		{name: "unknown route", method: http.MethodGet, path: "/admin/does-not-exist", status: http.StatusNotFound},
		{name: "method not allowed", method: http.MethodPatch, path: "/admin/rate-limits", status: http.StatusMethodNotAllowed},
		{name: "invalid json", method: http.MethodPut, path: "/admin/profile", body: []byte("{"), status: http.StatusBadRequest},
		{name: "invalid amount", method: http.MethodPost, path: "/admin/channels", body: jsonBody(map[string]any{"name": "x", "base_url": "https://x.test", "api_key": "sk", "status": 1, "balance": "not-money"}), status: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer()
			var body bytes.Buffer
			body.Write(tt.body)
			req := httptest.NewRequest(tt.method, tt.path, &body)
			if len(tt.body) > 0 {
				req.Header.Set("Content-Type", "application/json")
			}
			res := httptest.NewRecorder()
			server.Admin(res, withTestOwner(req))
			if res.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", res.Code, tt.status, res.Body.String())
			}
			var envelope map[string]any
			decodeJSON(t, res, &envelope)
			if got := int(envelope["code"].(float64)); got != tt.status {
				t.Fatalf("code = %d, want %d; body=%s", got, tt.status, res.Body.String())
			}
			if message, ok := envelope["message"].(string); !ok || message == "" {
				t.Fatalf("message = %v, want non-empty string", envelope["message"])
			}
			data, ok := envelope["data"].(map[string]any)
			if !ok {
				t.Fatalf("data type = %T, want object", envelope["data"])
			}
			if len(data) != 0 {
				t.Fatalf("error data = %+v, want empty object", data)
			}
		})
	}
}

func assertAdminSuccess(t *testing.T, res *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	var envelope map[string]any
	decodeJSON(t, res, &envelope)
	if int(envelope["code"].(float64)) != 0 || envelope["message"] != "ok" {
		t.Fatalf("envelope = %+v, want code=0 message=ok", envelope)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want object", envelope["data"])
	}
	return data
}

func assertAdminList(t *testing.T, data map[string]any) {
	t.Helper()
	list, ok := data["list"].([]any)
	if !ok {
		t.Fatalf("list type = %T, want array", data["list"])
	}
	if _, ok := data["total"].(float64); !ok {
		t.Fatalf("total type = %T, want number; list length=%d", data["total"], len(list))
	}
}

func assertFields(t *testing.T, object map[string]any, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			t.Errorf("missing JSON field %q in %+v", field, object)
		}
	}
}

func assertAbsentFields(t *testing.T, object map[string]any, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if _, ok := object[field]; ok {
			t.Errorf("unexpected JSON field %q in %+v", field, object)
		}
	}
}

func assertStringFields(t *testing.T, object map[string]any, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if _, ok := object[field].(string); !ok {
			t.Errorf("field %q type = %T, want string", field, object[field])
		}
	}
}

func jsonBody(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

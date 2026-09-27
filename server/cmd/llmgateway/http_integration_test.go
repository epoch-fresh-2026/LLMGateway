package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/httpapi"
	"LLMGateway/server/internal/testutil/storefake"
)

// cmdSessionCookies caches one registered session cookie per test server so the
// request helpers can authenticate against the session-protected /admin API.
var cmdSessionCookies sync.Map

type httpAdminEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestHTTPIntegrationHealthz(t *testing.T) {
	server := httptest.NewServer(newRouter(storefake.New()))
	t.Cleanup(server.Close)

	res := getHTTP(t, server, "/healthz")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	var body map[string]string
	decodeHTTPJSON(t, res, &body)
	if body["status"] != "ok" {
		t.Fatalf("health status = %q, want ok", body["status"])
	}
}

func TestHTTPIntegrationDashboardStartup(t *testing.T) {
	server := httptest.NewServer(newRouter(storefake.New()))
	t.Cleanup(server.Close)

	tests := []struct {
		name string
		path string
		list bool
	}{
		{"overview", "/admin/stats/overview?start_time=2026-09-16T10:00:00Z&end_time=2026-09-16T11:00:00Z", false},
		{"daily", "/admin/stats/daily?date_from=2026-09-10&date_to=2026-09-16&page=1&page_size=100", true},
		{"channels", "/admin/channels?page=1&page_size=100", true},
		{"channel stats", "/admin/stats/channels?start_time=2026-09-16T10:00:00Z&end_time=2026-09-16T11:00:00Z", true},
		{"usage logs", "/admin/usage-logs?page=1&page_size=20", true},
		{"profile", "/admin/profile", false},
		{"keys", "/admin/keys?page=1&page_size=100", true},
		{"rate limits", "/admin/rate-limits?page=1&page_size=100&enabled=true", true},
		{"models", "/admin/models?status=1&page=1&page_size=100", true},
		{"quota policies", "/admin/quota-policies?page=1&page_size=100", true},
		{"quota usage", "/admin/quota-usage?page=1&page_size=100", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := getHTTP(t, server, tt.path)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200; body=%s", tt.path, res.StatusCode, readHTTPBody(t, res))
			}
			var envelope httpAdminEnvelope
			decodeHTTPJSON(t, res, &envelope)
			if envelope.Code != 0 || envelope.Message != "ok" {
				t.Fatalf("envelope = %+v, want code=0 message=ok", envelope)
			}
			if tt.list {
				var data struct {
					List  []json.RawMessage `json:"list"`
					Total int               `json:"total"`
				}
				decodeRawHTTPJSON(t, envelope.Data, &data)
				if data.List == nil || data.Total < 0 {
					t.Fatalf("list response = %+v", data)
				}
			}
		})
	}
}

func TestHTTPIntegrationQueryAndErrorContracts(t *testing.T) {
	server := httptest.NewServer(newRouter(storefake.New()))
	t.Cleanup(server.Close)

	badQueries := []string{
		"/admin/stats/usage",
		"/admin/stats/usage?group_by=unknown",
		"/admin/stats/usage?group_by=model&start_time=2026-09-16T00:00:00Z&date_from=2026-09-16",
		"/admin/usage-logs?user_id=not-an-int",
	}
	for _, path := range badQueries {
		t.Run(path, func(t *testing.T) {
			res := getHTTP(t, server, path)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("GET %s status = %d, want 400; body=%s", path, res.StatusCode, readHTTPBody(t, res))
			}
			var envelope httpAdminEnvelope
			decodeHTTPJSON(t, res, &envelope)
			if envelope.Code != http.StatusBadRequest || envelope.Message == "" {
				t.Fatalf("error envelope = %+v", envelope)
			}
			var data map[string]any
			decodeRawHTTPJSON(t, envelope.Data, &data)
			if len(data) != 0 {
				t.Fatalf("error data = %+v, want empty object", data)
			}
		})
	}
}

func TestHTTPIntegrationMutationAndDeleteBody(t *testing.T) {
	cipher, err := crypto.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(newRouter(storefake.New(), httpapi.WithCipher(cipher)))
	t.Cleanup(server.Close)

	res := sendHTTPJSON(t, server, http.MethodPost, "/admin/channels", map[string]any{
		"name": "contract-channel", "base_url": "https://api.example.test", "api_key": "sk-contract", "auth_type": "bearer", "status": 1,
	})
	assertHTTPStatus(t, res, http.StatusOK)
	var channel httpAdminEnvelope
	decodeHTTPJSON(t, res, &channel)
	var channelData map[string]any
	decodeRawHTTPJSON(t, channel.Data, &channelData)
	if _, ok := channelData["api_key"]; ok {
		t.Fatal("channel response leaked api_key")
	}

	res = sendHTTPJSON(t, server, http.MethodPost, "/admin/channels/1/models", map[string]any{
		"model_name": "contract-model", "upstream_model": "contract-upstream", "enabled": true,
	})
	assertHTTPStatus(t, res, http.StatusOK)

	res = sendHTTPJSON(t, server, http.MethodPost, "/admin/pricing", map[string]any{
		"channel_id": 1, "model_name": "contract-model", "input_price_per_1m": "0.100000", "output_price_per_1m": "0.200000", "currency": "USD",
	})
	assertHTTPStatus(t, res, http.StatusOK)
	res = sendHTTPJSON(t, server, http.MethodDelete, "/admin/pricing", map[string]any{"channel_id": 1, "model_name": "contract-model"})
	assertHTTPStatus(t, res, http.StatusOK)
	var deleted httpAdminEnvelope
	decodeHTTPJSON(t, res, &deleted)
	var deletedData map[string]any
	decodeRawHTTPJSON(t, deleted.Data, &deletedData)
	if deletedData["deleted"] != true {
		t.Fatalf("delete response = %+v", deletedData)
	}
}

func getHTTP(t *testing.T, server *httptest.Server, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sessionCookie(t, server))
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func sendHTTPJSON(t *testing.T, server *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie(t, server))
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// sessionCookie registers a user on the integration server and returns the
// resulting session cookie, caching it for the server's lifetime.
func sessionCookie(t *testing.T, server *httptest.Server) *http.Cookie {
	t.Helper()
	if cached, ok := cmdSessionCookies.Load(server); ok {
		return cached.(*http.Cookie)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/admin/auth/register", bytes.NewReader([]byte(`{"username":"cmduser","password":"password123"}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("register integration session: status %d body=%s", res.StatusCode, body)
	}
	for _, cookie := range res.Cookies() {
		if cookie.Name == "llmgateway_session" && cookie.Value != "" {
			cmdSessionCookies.Store(server, cookie)
			return cookie
		}
	}
	t.Fatal("register integration session: no session cookie")
	return nil
}

func assertHTTPStatus(t *testing.T, res *http.Response, want int) {
	t.Helper()
	if res.StatusCode != want {
		t.Fatalf("status = %d, want %d; body=%s", res.StatusCode, want, readHTTPBody(t, res))
	}
}

func decodeHTTPJSON(t *testing.T, res *http.Response, value any) {
	t.Helper()
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(value); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func decodeRawHTTPJSON(t *testing.T, raw json.RawMessage, value any) {
	t.Helper()
	if err := json.Unmarshal(raw, value); err != nil {
		t.Fatalf("decode response data: %v; body=%s", err, raw)
	}
}

func readHTTPBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

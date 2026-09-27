package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/httpcommon"
	"LLMGateway/server/internal/testutil/storefake"
)

// testOwnerID is the identity injected into admin-request helpers. Business
// tests clear enforceSession, so they must supply the identity the middleware
// would otherwise inject.
const testOwnerID = 1

func withIdentity(req *http.Request, userID int) *http.Request {
	return req.WithContext(httpcommon.WithIdentity(req.Context(), httpcommon.Identity{UserID: userID}))
}

func withTestOwner(req *http.Request) *http.Request {
	return withIdentity(req, testOwnerID)
}

// adminRawAs issues an admin request as a specific authenticated user.
func adminRawAs(t *testing.T, server *Server, userID int, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	server.Admin(res, withIdentity(req, userID))
	return res
}

func TestHealthz(t *testing.T) {
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	newTestServer().Healthz(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}

	var body map[string]string
	decodeJSON(t, res, &body)
	if body["status"] != "ok" {
		t.Fatalf("status field = %q, want ok", body["status"])
	}
}

func TestDashboardStartupEndpoints(t *testing.T) {
	tests := []struct {
		path         string
		wantFields   []string
		wantListData bool
	}{
		{"/admin/stats/overview?start_time=2026-09-16T10:00:00Z&end_time=2026-09-16T11:00:00Z", []string{"request_count", "success_count", "error_count", "total_tokens", "total_cost", "active_user_count"}, false},
		{"/admin/stats/ttft?start_time=2026-09-16T10:00:00Z&end_time=2026-09-16T11:00:00Z", []string{"sample_count", "average_ms", "p50_ms", "p95_ms", "p99_ms"}, false},
		{"/admin/stats/daily?date_from=2026-09-10&date_to=2026-09-16&page=1&page_size=100", nil, true},
		{"/admin/channels?page=1&page_size=100", nil, true},
		{"/admin/stats/channels?start_time=2026-09-16T10:00:00Z&end_time=2026-09-16T11:00:00Z", []string{"list"}, false},
		{"/admin/usage-logs?page=1&page_size=20", nil, true},
		{"/admin/users?page=1&page_size=100", nil, true},
		{"/admin/rate-limits?page=1&page_size=100&enabled=true", nil, true},
		{"/admin/models?status=1", nil, true},
		{"/admin/quota-policies?page=1&page_size=100", nil, true},
		{"/admin/quota-usage?page=1&page_size=100", nil, true},
	}

	server := newTestServer()
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			res := httptest.NewRecorder()
			req := withTestOwner(httptest.NewRequest(http.MethodGet, tt.path, nil))
			server.Admin(res, req)

			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
			}

			var body adminResponse
			decodeJSON(t, res, &body)
			if body.Code != 0 || body.Message != "ok" {
				t.Fatalf("response = %+v, want code=0 message=ok", body)
			}

			data, ok := body.Data.(map[string]any)
			if !ok {
				t.Fatalf("data type = %T, want object", body.Data)
			}

			if tt.wantListData {
				assertListResponse(t, data)
			}
			for _, field := range tt.wantFields {
				if _, ok := data[field]; !ok {
					t.Fatalf("missing data.%s in %+v", field, data)
				}
			}
		})
	}
}

func TestAdminSameOriginNoCORSReflection(t *testing.T) {
	server := newTestServer()

	// Same-origin deployment must not reflect arbitrary Origins.
	res := httptest.NewRecorder()
	req := withTestOwner(httptest.NewRequest(http.MethodGet, "/admin/stats/overview", nil))
	req.Header.Set("Origin", "http://example.test")
	server.Admin(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for same-origin deployment", got)
	}
}

func TestPaginationParsing(t *testing.T) {
	page, pageSize := httpcommon.ParsePagination(httptest.NewRequest(http.MethodGet, "/?page=2&page_size=50", nil))
	if page != 2 || pageSize != 50 {
		t.Fatalf("ParsePagination valid = %d,%d; want 2,50", page, pageSize)
	}

	page, pageSize = httpcommon.ParsePagination(httptest.NewRequest(http.MethodGet, "/?page=-1&page_size=abc", nil))
	if page != 1 || pageSize != 20 {
		t.Fatalf("ParsePagination fallback = %d,%d; want 1,20", page, pageSize)
	}
}

// newTestServer builds a server for business-behaviour tests; session
// enforcement is disabled so tests can focus on their target behaviour.
func newTestServer() *Server {
	server := newEnforcedTestServer()
	server.enforceSession = false
	return server
}

// newEnforcedTestServer builds a server with session enforcement enabled and
// cheap bcrypt, for auth/enforcement tests.
func newEnforcedTestServer() *Server {
	return NewServer(storefake.New(),
		WithCipher(testCipher()),
		WithSessionConfig(time.Hour, false, true, crypto.MinPasswordCost))
}

func assertListResponse(t *testing.T, data map[string]any) {
	t.Helper()
	if _, ok := data["list"].([]any); !ok {
		t.Fatalf("data.list type = %T, want array", data["list"])
	}
	if _, ok := data["total"].(float64); !ok {
		t.Fatalf("data.total type = %T, want number", data["total"])
	}
}

func decodeJSON(t *testing.T, res *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), v); err != nil {
		t.Fatalf("decode JSON: %v; body=%s", err, res.Body.String())
	}
}

func adminDo(t *testing.T, server *Server, method, path string, body any) map[string]any {
	t.Helper()
	res := adminRaw(t, server, method, path, body)
	if res.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d, want 200; body=%s", method, path, res.Code, res.Body.String())
	}
	var out map[string]any
	decodeJSON(t, res, &out)
	if out["code"].(float64) != 0 {
		t.Fatalf("%s %s code = %v; body=%s", method, path, out["code"], res.Body.String())
	}
	return out
}

func adminRaw(t *testing.T, server *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	server.Admin(res, withTestOwner(req))
	return res
}

func assertNoSecret(t *testing.T, value any, secret string) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) || strings.Contains(string(b), "api_key") {
		t.Fatalf("response leaked secret or api_key: %s", b)
	}
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

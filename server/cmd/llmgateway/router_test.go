package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LLMGateway/server/internal/testutil/storefake"
)

// TestNewRouterPaths exercises the production route table directly so drift
// between cmd/llmgateway/router.go and the handler entry points is caught.
func TestNewRouterPaths(t *testing.T) {
	router := newRouter(storefake.New())
	cookie := registerRouterSession(t, router)

	tests := []struct {
		name      string
		method    string
		path      string
		want      int
		noSession bool
	}{
		{"healthz", http.MethodGet, "/healthz", http.StatusOK, false},
		{"admin stats overview", http.MethodGet, "/admin/stats/overview", http.StatusOK, false},
		{"admin channels", http.MethodGet, "/admin/channels", http.StatusOK, false},
		{"admin without session", http.MethodGet, "/admin/channels", http.StatusUnauthorized, true},
		{"v1 models requires auth", http.MethodGet, "/v1/models", http.StatusUnauthorized, false},
		{"v1 chat requires auth", http.MethodPost, "/v1/chat/completions", http.StatusUnauthorized, false},
		{"root is not served by backend", http.MethodGet, "/", http.StatusNotFound, false},
		{"unknown path", http.MethodGet, "/does-not-exist", http.StatusNotFound, false},
		{"unknown admin path", http.MethodGet, "/admin/does-not-exist", http.StatusNotFound, false},
		{"unknown v1 path", http.MethodGet, "/v1/does-not-exist", http.StatusNotFound, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if !tt.noSession {
				req.AddCookie(cookie)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			if res.Code != tt.want {
				t.Fatalf("%s %s status = %d, want %d; body=%s", tt.method, tt.path, res.Code, tt.want, res.Body.String())
			}
		})
	}
}

func registerRouterSession(t *testing.T, router http.Handler) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/register", strings.NewReader(`{"username":"routeruser","password":"password123"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("register router session: status %d body=%s", res.Code, res.Body.String())
	}
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == "llmgateway_session" && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatal("register router session: no session cookie")
	return nil
}

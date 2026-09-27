package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/testutil/storefake"
)

const testSessionCookie = "llmgateway_session"

func TestAdminAuthFlow(t *testing.T) {
	server := newTestServer()

	registered := authRequest(t, server, http.MethodPost, "/admin/auth/register", map[string]any{"username": "alice", "password": "password123"}, nil)
	if registered.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200; body=%s", registered.Code, registered.Body.String())
	}
	cookie := sessionCookieFrom(t, registered)
	if !cookie.HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}

	me := authRequest(t, server, http.MethodGet, "/admin/auth/me", nil, cookie)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200; body=%s", me.Code, me.Body.String())
	}
	var meBody map[string]any
	decodeJSON(t, me, &meBody)
	data, _ := meBody["data"].(map[string]any)
	if data["username"] != "alice" {
		t.Fatalf("me data = %+v, want username alice", data)
	}

	anonymous := authRequest(t, server, http.MethodGet, "/admin/auth/me", nil, nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous me status = %d, want 401", anonymous.Code)
	}

	badLogin := authRequest(t, server, http.MethodPost, "/admin/auth/login", map[string]any{"username": "alice", "password": "wrong-password"}, nil)
	if badLogin.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d, want 401", badLogin.Code)
	}

	login := authRequest(t, server, http.MethodPost, "/admin/auth/login", map[string]any{"username": "alice", "password": "password123"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body=%s", login.Code, login.Body.String())
	}
	loginCookie := sessionCookieFrom(t, login)

	logout := authRequest(t, server, http.MethodPost, "/admin/auth/logout", nil, loginCookie)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", logout.Code)
	}
	if !hasClearingCookie(t, logout) {
		t.Fatal("logout must clear the session cookie")
	}

	afterLogout := authRequest(t, server, http.MethodGet, "/admin/auth/me", nil, loginCookie)
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout status = %d, want 401", afterLogout.Code)
	}
}

func TestAdminRoutesRequireSession(t *testing.T) {
	server := newEnforcedTestServer()

	anonymous := authRequest(t, server, http.MethodGet, "/admin/channels", nil, nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous admin status = %d, want 401; body=%s", anonymous.Code, anonymous.Body.String())
	}

	registered := authRequest(t, server, http.MethodPost, "/admin/auth/register", map[string]any{"username": "enforced", "password": "password123"}, nil)
	if registered.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200; body=%s", registered.Code, registered.Body.String())
	}
	cookie := sessionCookieFrom(t, registered)

	authed := authRequest(t, server, http.MethodGet, "/admin/channels", nil, cookie)
	if authed.Code != http.StatusOK {
		t.Fatalf("authenticated admin status = %d, want 200; body=%s", authed.Code, authed.Body.String())
	}
}

func TestAdminAuthRegistrationDisabled(t *testing.T) {
	server := NewServer(storefake.New(),
		WithCipher(testCipher()),
		WithSessionConfig(time.Hour, false, false, crypto.MinPasswordCost))
	res := authRequest(t, server, http.MethodPost, "/admin/auth/register", map[string]any{"username": "bob", "password": "password123"}, nil)
	if res.Code != http.StatusForbidden {
		t.Fatalf("register disabled status = %d, want 403; body=%s", res.Code, res.Body.String())
	}
}

func authRequest(t *testing.T, server *Server, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
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
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	server.Admin(res, req)
	return res
}

func sessionCookieFrom(t *testing.T, res *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == testSessionCookie && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatalf("no session cookie in response: %s", res.Body.String())
	return nil
}

func hasClearingCookie(t *testing.T, res *httptest.ResponseRecorder) bool {
	t.Helper()
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == testSessionCookie && cookie.MaxAge < 0 {
			return true
		}
	}
	return false
}

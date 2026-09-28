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
	assertAccountData(t, registered, "alice")

	me := authRequest(t, server, http.MethodGet, "/admin/auth/me", nil, cookie)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200; body=%s", me.Code, me.Body.String())
	}
	assertAccountData(t, me, "alice")

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
	assertAccountData(t, login, "alice")
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

func TestAdminAuthErrorCodes(t *testing.T) {
	server := newTestServer()

	assertErrorCode(t, authRequest(t, server, http.MethodPost, "/admin/auth/login", map[string]any{"username": "ghost", "password": "password123"}, nil), http.StatusUnauthorized, "username_not_found")

	authRequest(t, server, http.MethodPost, "/admin/auth/register", map[string]any{"username": "carol", "password": "password123"}, nil)
	assertErrorCode(t, authRequest(t, server, http.MethodPost, "/admin/auth/login", map[string]any{"username": "carol", "password": "wrong-password"}, nil), http.StatusUnauthorized, "wrong_password")

	assertErrorCode(t, authRequest(t, server, http.MethodPost, "/admin/auth/register", map[string]any{"username": "carol", "password": "password123"}, nil), http.StatusConflict, "username_taken")

	assertErrorCode(t, authRequest(t, server, http.MethodPost, "/admin/auth/register", map[string]any{"username": "carol", "password": "short"}, nil), http.StatusBadRequest, "password_length")
	assertErrorCode(t, authRequest(t, server, http.MethodPost, "/admin/auth/register", map[string]any{"username": "ab", "password": "password123"}, nil), http.StatusBadRequest, "username_length")
	assertErrorCode(t, authRequest(t, server, http.MethodPost, "/admin/auth/register", map[string]any{"username": "has space", "password": "password123"}, nil), http.StatusBadRequest, "username_whitespace")
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

// assertAccountData checks the auth envelope returns the Account directly in
// data, matching the OpenAPI contract shared with the dashboard.
func assertAccountData(t *testing.T, res *httptest.ResponseRecorder, username string) {
	t.Helper()
	var body struct {
		Data struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode account response: %v; body=%s", err, res.Body.String())
	}
	if body.Data.ID == 0 || body.Data.Username != username {
		t.Fatalf("account data = %+v, want username %q with id", body.Data, username)
	}
}

// assertErrorCode checks the HTTP status and the stable error_code that the
// dashboard localizes from.
func assertErrorCode(t *testing.T, res *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if res.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, wantStatus, res.Body.String())
	}
	var body struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, res.Body.String())
	}
	if body.ErrorCode != wantCode {
		t.Fatalf("error_code = %q, want %q; body=%s", body.ErrorCode, wantCode, res.Body.String())
	}
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

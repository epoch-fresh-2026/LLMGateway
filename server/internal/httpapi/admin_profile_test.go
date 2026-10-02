package httpapi

import (
	"net/http"
	"testing"
	"time"
)

func TestProfileAndSelfKeys(t *testing.T) {
	server := newEnforcedTestServer()

	registered := authRequest(t, server, http.MethodPost, "/admin/auth/register", map[string]any{"username": "alice", "password": "password123"}, nil)
	if registered.Code != http.StatusOK {
		t.Fatalf("register status = %d; body=%s", registered.Code, registered.Body.String())
	}
	cookie := sessionCookieFrom(t, registered)

	profile := authRequest(t, server, http.MethodGet, "/admin/profile", nil, cookie)
	if profile.Code != http.StatusOK {
		t.Fatalf("profile status = %d; body=%s", profile.Code, profile.Body.String())
	}

	updated := authRequest(t, server, http.MethodPut, "/admin/profile", map[string]any{"nickname": "Alice"}, cookie)
	if updated.Code != http.StatusOK {
		t.Fatalf("update profile status = %d; body=%s", updated.Code, updated.Body.String())
	}
	var updatedBody map[string]any
	decodeJSON(t, updated, &updatedBody)
	if updatedBody["data"].(map[string]any)["nickname"] != "Alice" {
		t.Fatalf("profile not updated: %s", updated.Body.String())
	}

	// Changing the password requires the current password.
	if res := authRequest(t, server, http.MethodPut, "/admin/profile", map[string]any{"current_password": "wrong", "new_password": "newpassword123"}, cookie); res.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password status = %d, want 401", res.Code)
	}
	if res := authRequest(t, server, http.MethodPut, "/admin/profile", map[string]any{"current_password": "password123", "new_password": "newpassword123"}, cookie); res.Code != http.StatusOK {
		t.Fatalf("password change status = %d; body=%s", res.Code, res.Body.String())
	}
	if res := authRequest(t, server, http.MethodPost, "/admin/auth/login", map[string]any{"username": "alice", "password": "newpassword123"}, nil); res.Code != http.StatusOK {
		t.Fatalf("login with new password status = %d", res.Code)
	}

	// Self-scoped keys.
	created := authRequest(t, server, http.MethodPost, "/admin/keys", map[string]any{"key_name": "mine", "prefix": "sk-"}, cookie)
	if created.Code != http.StatusOK {
		t.Fatalf("create key status = %d; body=%s", created.Code, created.Body.String())
	}
	var createdBody map[string]any
	decodeJSON(t, created, &createdBody)
	keyID := int(createdBody["data"].(map[string]any)["id"].(float64))

	listed := authRequest(t, server, http.MethodGet, "/admin/keys", nil, cookie)
	var listBody map[string]any
	decodeJSON(t, listed, &listBody)
	if listBody["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("keys total = %v, want 1", listBody["data"])
	}
	key := listBody["data"].(map[string]any)["list"].([]any)[0].(map[string]any)
	assertFields(t, key, "created_at", "key_suffix")
	assertAbsentFields(t, key, "full_key", "key_hash")
	createdAt, ok := key["created_at"].(string)
	if !ok {
		t.Fatal("created_at must be a string")
	}
	if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
		t.Fatalf("invalid created_at: %v", err)
	}
	reset := authRequest(t, server, http.MethodPost, "/admin/keys/"+itoa(keyID)+"/reset", nil, cookie)
	assertAdminSuccess(t, reset)
	toggled := assertAdminSuccess(t, authRequest(t, server, http.MethodPut, "/admin/keys/"+itoa(keyID), map[string]any{"is_active": false}, cookie))
	if toggled["created_at"] != createdAt {
		t.Fatal("created_at changed after reset or update")
	}

	if res := authRequest(t, server, http.MethodDelete, "/admin/keys/"+itoa(keyID), nil, cookie); res.Code != http.StatusOK {
		t.Fatalf("delete key status = %d", res.Code)
	}
	if res := authRequest(t, server, http.MethodDelete, "/admin/keys/"+itoa(keyID), nil, cookie); res.Code != http.StatusNotFound {
		t.Fatalf("second delete key status = %d, want 404", res.Code)
	}
}

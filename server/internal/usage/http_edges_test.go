package usage_test

import (
	"LLMGateway/server/internal/httpcommon"
	"LLMGateway/server/internal/testutil/storefake"
	"LLMGateway/server/internal/usage"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUsageHTTPInputAndIdentity(t *testing.T) {
	a := usage.New(storefake.New())
	mux := http.NewServeMux()
	a.RegisterAdminRoutes(mux)
	for _, path := range []string{"/admin/usage-logs", "/admin/usage-logs/1", "/admin/stats/overview", "/admin/stats/daily", "/admin/stats/channels", "/admin/stats/ttft", "/admin/stats/usage"} {
		t.Run("anonymous "+path, func(t *testing.T) {
			r := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		name, method, path string
		status             int
	}{
		{"list method", "POST", "/admin/usage-logs", 405}, {"item method", "POST", "/admin/usage-logs/1", 405}, {"item id", "GET", "/admin/usage-logs/nope", 400},
		{"user invalid", "GET", "/admin/usage-logs?user_id=bad", 400}, {"channel invalid", "GET", "/admin/usage-logs?channel_id=bad", 400}, {"filters valid", "GET", "/admin/usage-logs?user_id=1&channel_id=2&api_key_id=3", 200},
		{"ttft invalid", "GET", "/admin/stats/ttft?channel_id=bad", 400}, {"aggregate date invalid", "GET", "/admin/stats/usage?group_by=model&date_from=bad", 400}, {"aggregate filter invalid", "GET", "/admin/stats/usage?group_by=model&user_id=-1", 400}, {"aggregate dates", "GET", "/admin/stats/usage?group_by=model&date_from=2026-10-01", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r = r.WithContext(httpcommon.WithIdentity(context.Background(), httpcommon.Identity{UserID: 1}))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	if e := usage.ValidateSince(""); !errors.Is(e, usage.ErrInvalid) {
		t.Fatal(e)
	}
}

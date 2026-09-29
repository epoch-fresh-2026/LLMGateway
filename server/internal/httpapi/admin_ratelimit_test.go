package httpapi

import (
	"net/http"
	"testing"
)

func TestRateLimitCRUDAndFilter(t *testing.T) {
	handler := newTestServer()

	created := adminDo(t, handler, http.MethodPost, "/admin/rate-limits", map[string]any{
		"rule_name": "default user rpm", "target_type": "user", "metric": "rpm", "limit_value": 600, "action": "reject",
	})
	rule := created["data"].(map[string]any)
	if rule["id"].(float64) != 1 || rule["target_value"] != "*" || rule["enabled"] != true {
		t.Fatalf("unexpected rule: %+v", rule)
	}

	// Partial update: only enabled.
	updated := adminDo(t, handler, http.MethodPut, "/admin/rate-limits/1", map[string]any{"enabled": false})
	rule = updated["data"].(map[string]any)
	if rule["enabled"] != false || rule["rule_name"] != "default user rpm" {
		t.Fatalf("partial update lost fields: %+v", rule)
	}

	enabled := adminDo(t, handler, http.MethodGet, "/admin/rate-limits?enabled=true", nil)
	data := enabled["data"].(map[string]any)
	if data["total"].(float64) != 0 {
		t.Fatalf("enabled filter total = %v, want 0", data["total"])
	}

	all := adminDo(t, handler, http.MethodGet, "/admin/rate-limits", nil)
	if all["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("all total = %v, want 1", all["data"])
	}

	adminDo(t, handler, http.MethodDelete, "/admin/rate-limits/1", nil)
	missing := adminRaw(t, handler, http.MethodDelete, "/admin/rate-limits/1", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d, want 404", missing.Code)
	}

	invalid := adminRaw(t, handler, http.MethodPost, "/admin/rate-limits", map[string]any{
		"rule_name": "bad", "target_type": "user", "metric": "bogus", "limit_value": 1, "action": "reject",
	})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid metric status = %d, want 400", invalid.Code)
	}
	queue := adminRaw(t, handler, http.MethodPost, "/admin/rate-limits", map[string]any{
		"rule_name": "queue", "target_type": "user", "metric": "rpm", "limit_value": 1, "action": "queue",
	})
	if queue.Code != http.StatusBadRequest {
		t.Fatalf("queue action status = %d, want 400", queue.Code)
	}
}

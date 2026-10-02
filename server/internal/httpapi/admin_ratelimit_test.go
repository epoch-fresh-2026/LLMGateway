package httpapi

import (
	"net/http"
	"reflect"
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

	for _, body := range []map[string]any{
		{}, {"enabled": nil}, {"enabled": "false"}, {"enabled": 0},
		{"enabled": false, "rule_name": "changed"},
		{"enabled": false, "target_type": "model"},
		{"enabled": false, "target_value": "changed"},
		{"enabled": false, "metric": "tpm"},
		{"enabled": false, "limit_value": 1},
		{"enabled": false, "action": "reject"},
		{"enabled": false, "priority": 1},
		{"enabled": false, "extras": map[string]any{}},
	} {
		if res := adminRaw(t, handler, http.MethodPut, "/admin/rate-limits/1", body); res.Code != http.StatusBadRequest {
			t.Fatalf("update %v status = %d, want 400", body, res.Code)
		}
	}
	if res := adminRawAs(t, handler, 2, http.MethodPut, "/admin/rate-limits/1", map[string]any{"enabled": false}); res.Code != http.StatusNotFound {
		t.Fatalf("foreign update status = %d, want 404", res.Code)
	}
	if res := adminRaw(t, handler, http.MethodPut, "/admin/rate-limits/404", map[string]any{"enabled": false}); res.Code != http.StatusNotFound {
		t.Fatalf("missing update status = %d, want 404", res.Code)
	}
	before := adminDo(t, handler, http.MethodGet, "/admin/rate-limits", nil)["data"].(map[string]any)["list"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(before, rule) {
		t.Fatalf("rejected updates changed rule: %+v", before)
	}
	updated := adminDo(t, handler, http.MethodPut, "/admin/rate-limits/1", map[string]any{"enabled": false})
	rule = updated["data"].(map[string]any)
	before["enabled"] = false
	if !reflect.DeepEqual(before, rule) {
		t.Fatalf("status update changed other fields: %+v", rule)
	}
	stored := adminDo(t, handler, http.MethodGet, "/admin/rate-limits", nil)["data"].(map[string]any)["list"].([]any)[0]
	if !reflect.DeepEqual(stored, rule) {
		t.Fatalf("stored rule differs from response: %+v", stored)
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

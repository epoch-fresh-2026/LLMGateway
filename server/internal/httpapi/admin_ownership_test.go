package httpapi

import (
	"net/http"
	"testing"
)

func TestRateLimitAndQuotaOwnershipIsolation(t *testing.T) {
	handler := newTestServer()

	rule := adminRawAs(t, handler, 1, http.MethodPost, "/admin/rate-limits", map[string]any{
		"rule_name": "mine", "target_type": "user", "target_value": "1", "metric": "rpm", "limit_value": 10, "action": "reject",
	})
	if rule.Code != http.StatusOK {
		t.Fatalf("create rule status = %d; body=%s", rule.Code, rule.Body.String())
	}
	policy := adminRawAs(t, handler, 1, http.MethodPost, "/admin/quota-policies", map[string]any{
		"policy_name": "mine", "scope_type": "user", "scope_id": 1, "period_type": "day", "token_limit": 100,
	})
	if policy.Code != http.StatusOK {
		t.Fatalf("create policy status = %d; body=%s", policy.Code, policy.Body.String())
	}

	if data := decodeAdminData(t, adminRawAs(t, handler, 2, http.MethodGet, "/admin/rate-limits", nil)); data["total"].(float64) != 0 {
		t.Fatalf("other user sees %v rules, want 0", data["total"])
	}
	if data := decodeAdminData(t, adminRawAs(t, handler, 2, http.MethodGet, "/admin/quota-policies", nil)); data["total"].(float64) != 0 {
		t.Fatalf("other user sees %v policies, want 0", data["total"])
	}
	if res := adminRawAs(t, handler, 2, http.MethodDelete, "/admin/rate-limits/1", nil); res.Code != http.StatusNotFound {
		t.Fatalf("other user delete rule status = %d, want 404", res.Code)
	}
	if res := adminRawAs(t, handler, 2, http.MethodDelete, "/admin/quota-policies/1", nil); res.Code != http.StatusNotFound {
		t.Fatalf("other user delete policy status = %d, want 404", res.Code)
	}

	// A policy scoped to another user is rejected.
	foreign := adminRawAs(t, handler, 2, http.MethodPost, "/admin/quota-policies", map[string]any{
		"policy_name": "foreign", "scope_type": "user", "scope_id": 1, "period_type": "day", "token_limit": 100,
	})
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign policy scope status = %d, want 404; body=%s", foreign.Code, foreign.Body.String())
	}
}

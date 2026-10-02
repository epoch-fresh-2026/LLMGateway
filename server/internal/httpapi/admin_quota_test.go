package httpapi

import (
	"net/http"
	"testing"
)

func TestAdminQuotaPolicyCreateListDelete(t *testing.T) {
	handler := newTestServer()
	create := adminDo(t, handler, http.MethodPost, "/admin/quota-policies", map[string]any{"policy_name": "daily", "scope_type": "user", "scope_id": 1, "period_type": "day", "token_limit": 100, "cost_limit": "2.5"})
	if create["data"].(map[string]any)["cost_limit"] != "2.500000" {
		t.Fatalf("create response = %+v", create)
	}
	list := adminDo(t, handler, http.MethodGet, "/admin/quota-policies?scope_type=user&scope_id=1", nil)
	if list["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("list response = %+v", list)
	}
	for _, owner := range []int{1, 2} {
		for _, path := range []string{"/admin/quota-policies/1", "/admin/quota-policies/999"} {
			for _, body := range []any{nil, map[string]any{"enabled": false, "policy_name": "changed", "token_limit": 1, "cost_limit": "0.1"}} {
				res := adminRawAs(t, handler, owner, http.MethodPut, path, body)
				if res.Code != http.StatusMethodNotAllowed {
					t.Fatalf("owner=%d PUT %s: status=%d body=%s", owner, path, res.Code, res.Body.String())
				}
			}
		}
	}
	list = adminDo(t, handler, http.MethodGet, "/admin/quota-policies?scope_type=user&scope_id=1", nil)
	policies := list["data"].(map[string]any)["list"].([]any)
	if len(policies) != 1 {
		t.Fatalf("policies after rejected PUT = %+v", policies)
	}
	policy := policies[0].(map[string]any)
	if policy["enabled"] != true || policy["policy_name"] != "daily" || policy["token_limit"].(float64) != 100 || policy["cost_limit"] != "2.500000" {
		t.Fatalf("policy changed after rejected PUT = %+v", policy)
	}
	usage := adminDo(t, handler, http.MethodGet, "/admin/quota-usage", nil)
	if usage["data"].(map[string]any)["total"].(float64) != 0 {
		t.Fatalf("usage response = %+v", usage)
	}
	adminDo(t, handler, http.MethodDelete, "/admin/quota-policies/1", nil)
	list = adminDo(t, handler, http.MethodGet, "/admin/quota-policies", nil)
	if list["data"].(map[string]any)["total"].(float64) != 0 {
		t.Fatalf("list after delete = %+v", list)
	}
}

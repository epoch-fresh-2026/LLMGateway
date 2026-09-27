package httpcommon

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentityRoundTrip(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/channels", nil)
	if _, ok := IdentityFrom(req); ok {
		t.Fatal("identity should be absent before injection")
	}

	ctx := WithIdentity(req.Context(), Identity{UserID: 42})
	req = req.WithContext(ctx)
	identity, ok := IdentityFrom(req)
	if !ok || identity.UserID != 42 {
		t.Fatalf("IdentityFrom = %+v, %v; want UserID 42", identity, ok)
	}
}

package httpcommon_test

import (
	apperrors "LLMGateway/server/internal/errors"
	"LLMGateway/server/internal/httpcommon"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnhandledResponseAndUnsupportedStorage(t *testing.T) {
	w := httptest.NewRecorder()
	httpcommon.AdminHandler(func(*http.Request) httpcommon.AdminResult { return httpcommon.Unhandled() }).ServeHTTP(w, httptest.NewRequest("GET", "/unknown", nil))
	if w.Code != 404 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if n := httpcommon.StatusFor(apperrors.ErrNotImplemented); n != 501 {
		t.Fatalf("status=%d", n)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r = r.WithContext(httpcommon.WithIdentity(r.Context(), httpcommon.Identity{UserID: 42}))
	id, result := httpcommon.RequireUserID(r)
	if id != 42 || result.Status != 0 {
		t.Fatalf("id=%d result=%+v", id, result)
	}
}

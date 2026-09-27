package httpapi

import (
	"net/http"
	"strings"

	"LLMGateway/server/internal/httpcommon"
)

// requireSession enforces a valid management session on every /admin route
// except the auth endpoints. On success it injects the authenticated user id
// into the request context so business handlers can scope by identity.
func (a *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		account, err := a.accounts.AuthenticateRequest(r)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, adminResponse{Code: http.StatusUnauthorized, Message: "unauthenticated", Data: map[string]any{}})
			return
		}
		ctx := httpcommon.WithIdentity(r.Context(), httpcommon.Identity{UserID: account.ID})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

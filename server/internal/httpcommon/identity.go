package httpcommon

import (
	"context"
	"net/http"
)

type identityContextKey struct{}

// Identity is the authenticated user propagated from the session layer to
// business handlers. It carries only the user id; authorization belongs to the
// owning module.
type Identity struct {
	UserID int
}

// WithIdentity returns a context carrying the authenticated identity.
func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

// IdentityFrom extracts the authenticated identity from the request context.
func IdentityFrom(r *http.Request) (Identity, bool) {
	identity, ok := r.Context().Value(identityContextKey{}).(Identity)
	return identity, ok
}

// RequireUserID returns the authenticated user id, or a 401 admin result when
// the session layer did not inject an identity. Business modules use it to scope
// owned data and must never fall back to unscoped access.
func RequireUserID(r *http.Request) (int, AdminResult) {
	identity, ok := IdentityFrom(r)
	if !ok {
		return 0, HTTPError(http.StatusUnauthorized, "unauthenticated")
	}
	return identity.UserID, AdminResult{}
}

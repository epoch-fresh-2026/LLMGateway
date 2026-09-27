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

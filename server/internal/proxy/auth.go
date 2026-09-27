package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/crypto"
	apperrors "LLMGateway/server/internal/errors"
)

// Authenticate validates the Bearer gateway key and returns the raw auth state.
func (a *Service) Authenticate(ctx context.Context, authorization string) (*accounts.AuthContext, error) {
	token, ok := bearerToken(authorization)
	if !ok {
		return nil, ErrUnauthorized
	}

	auth, err := a.store.AuthenticateKey(ctx, crypto.HashKey(token))
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if !auth.KeyActive {
		return nil, ErrUnauthorized
	}
	if auth.ExpiresAt != nil {
		expires, err := time.Parse(time.RFC3339, *auth.ExpiresAt)
		if err != nil {
			// Fail closed: an unparseable expiry must not grant access.
			return nil, ErrUnauthorized
		}
		if a.now().After(expires) {
			return nil, ErrUnauthorized
		}
	}
	return auth, nil
}

func bearerToken(authorization string) (string, bool) {
	if authorization == "" {
		return "", false
	}
	parts := strings.SplitN(authorization, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

// allowModel reports whether the key's permissions allow the requested model.
// An empty or "*" list allows everything.
func allowModel(auth *accounts.AuthContext, model string) bool {
	if len(auth.Permissions) == 0 {
		return true
	}
	var permissions struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(auth.Permissions, &permissions); err != nil {
		return true
	}
	if len(permissions.Models) == 0 {
		return true
	}
	for _, allowed := range permissions.Models {
		if allowed == "*" || allowed == model {
			return true
		}
	}
	return false
}

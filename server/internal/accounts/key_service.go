package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"LLMGateway/server/internal/crypto"
	apperrors "LLMGateway/server/internal/errors"
)

const (
	defaultKeyName     = "default"
	defaultKeyPrefix   = "sk-"
	keyPrefixBodyLen   = 5
	keySuffixLen       = 4
	defaultPermissions = `{"models":["*"]}`
)

// extractKeySuffix extracts the first 5 chars after prefix and last 4 chars
// for masked display (e.g., "sk-580cf*****0e89"). Returns "prefixBody+suffix"
// format for storage.
func extractKeySuffix(fullKey string) string {
	// Need at least prefix + 5 chars + 4 chars
	minLen := len(defaultKeyPrefix) + keyPrefixBodyLen + keySuffixLen
	if len(fullKey) < minLen {
		return fullKey
	}
	afterPrefix := fullKey[len(defaultKeyPrefix):]
	prefixBody := afterPrefix[:keyPrefixBodyLen]
	suffix := fullKey[len(fullKey)-keySuffixLen:]
	return prefixBody + suffix
}

// CreateKey generates a gateway key, stores only its hash, and returns the
// plaintext once.
func (a *Server) CreateKey(ctx context.Context, userID int, in KeyInput) (KeySecretDTO, error) {
	keyName := in.KeyName
	if keyName == "" {
		keyName = defaultKeyName
	}
	prefix := in.Prefix
	if prefix == "" {
		prefix = defaultKeyPrefix
	}
	isActive := true
	if in.IsActive != nil {
		isActive = *in.IsActive
	}

	// GenerateGatewayKey uses crypto/rand.Read, which is infallible on Go 1.24+.
	fullKey, _ := crypto.GenerateGatewayKey(prefix)

	var id int
	err := a.tx.InTx(ctx, func(tx Tx) error {
		if _, err := tx.GetUser(userID); err != nil {
			return err
		}
		created, err := tx.InsertKey(KeyInsert{
			UserID:             userID,
			KeyName:            keyName,
			Prefix:             prefix,
			KeyHash:            crypto.HashKey(fullKey),
			KeySuffix:          extractKeySuffix(fullKey),
			Permissions:        normalizePermissions(in.Permissions),
			RateLimitOverrides: in.RateLimitOverrides,
			ExpiresAt:          in.ExpiresAt,
			IsActive:           isActive,
		})
		if err != nil {
			return err
		}
		id = created
		return nil
	})
	if err != nil {
		return KeySecretDTO{}, err
	}
	return KeySecretDTO{ID: id, FullKey: fullKey}, nil
}

// UpdateKey toggles a key's active flag. is_active is required.
func (a *Server) UpdateKey(ctx context.Context, userID, keyID int, in KeyUpdateInput) (ClientKeyDTO, error) {
	if in.IsActive == nil {
		return ClientKeyDTO{}, fmt.Errorf("%w: is_active is required", apperrors.ErrInvalid)
	}

	var updated ClientKey
	err := a.tx.InTx(ctx, func(tx Tx) error {
		key, ok, err := tx.UpdateKeyActive(keyID, userID, *in.IsActive)
		if err != nil {
			return err
		}
		if !ok {
			return apperrors.ErrNotFound
		}
		updated = key
		return nil
	})
	if err != nil {
		return ClientKeyDTO{}, err
	}
	return ClientKeyDTO{ID: updated.ID, KeyName: updated.KeyName, Prefix: updated.Prefix, KeySuffix: updated.KeySuffix, IsActive: updated.IsActive, CreatedAt: updated.CreatedAt, LastUsedAt: updated.LastUsedAt, ExpiresAt: updated.ExpiresAt}, nil
}

// DeleteKey releases the key's quota reservations and removes the key in one
// transaction.
func (a *Server) DeleteKey(ctx context.Context, userID, keyID int) error {
	return a.tx.InTx(ctx, func(tx Tx) error {
		if err := tx.DeleteQuotaReservationsForKey(userID, keyID); err != nil {
			return err
		}
		ok, err := tx.DeleteKey(keyID, userID)
		if err != nil {
			return err
		}
		if !ok {
			return apperrors.ErrNotFound
		}
		return nil
	})
}

func normalizePermissions(value json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(defaultPermissions)
	}
	return value
}

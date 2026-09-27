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
	defaultPermissions = `{"models":["*"]}`
)

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

	fullKey, err := crypto.GenerateGatewayKey(prefix)
	if err != nil {
		return KeySecretDTO{}, err
	}

	var id int
	err = a.tx.InTx(ctx, func(tx Tx) error {
		if _, err := tx.GetUser(userID); err != nil {
			return err
		}
		created, err := tx.InsertKey(KeyInsert{
			UserID:             userID,
			KeyName:            keyName,
			Prefix:             prefix,
			KeyHash:            crypto.HashKey(fullKey),
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
	return ClientKeyDTO{ID: updated.ID, KeyName: updated.KeyName, Prefix: updated.Prefix, IsActive: updated.IsActive, LastUsedAt: updated.LastUsedAt, ExpiresAt: updated.ExpiresAt}, nil
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

// ResetKey rotates the stored hash and returns the new plaintext once.
func (a *Server) ResetKey(ctx context.Context, userID, keyID int) (KeySecretDTO, error) {
	var fullKey string
	err := a.tx.InTx(ctx, func(tx Tx) error {
		key, err := tx.GetKey(keyID, userID)
		if err != nil {
			return err
		}
		generated, err := crypto.GenerateGatewayKey(key.Prefix)
		if err != nil {
			return err
		}
		ok, err := tx.UpdateKeySecret(keyID, userID, crypto.HashKey(generated), key.Prefix)
		if err != nil {
			return err
		}
		if !ok {
			return apperrors.ErrNotFound
		}
		fullKey = generated
		return nil
	})
	if err != nil {
		return KeySecretDTO{}, err
	}
	return KeySecretDTO{FullKey: fullKey}, nil
}

func normalizePermissions(value json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(defaultPermissions)
	}
	return value
}

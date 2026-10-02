package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	domain "LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/db/sqlc"
	"LLMGateway/server/internal/store"

	"github.com/jackc/pgx/v5/pgtype"
)

// --- Store reads ---

func (s *Store) ListKeys(ctx context.Context, userID, page, pageSize int) (domain.ListResponse[domain.ClientKeyDTO], error) {
	limit, offset := limitOffset(page, pageSize)

	rows, err := s.queries.ListUserKeys(ctx, sqlc.ListUserKeysParams{UserID: int64(userID), Limit: limit, Offset: offset})
	if err != nil {
		return domain.ListResponse[domain.ClientKeyDTO]{}, mapError(err)
	}
	total, err := s.queries.CountUserKeys(ctx, int64(userID))
	if err != nil {
		return domain.ListResponse[domain.ClientKeyDTO]{}, mapError(err)
	}

	list := []domain.ClientKeyDTO{}
	for _, row := range rows {
		list = append(list, keyDTO(row.ID, row.KeyName, row.Prefix, row.KeySuffix, row.IsActive, row.CreatedAt, row.LastUsedAt, row.ExpiresAt))
	}
	return domain.ListResponse[domain.ClientKeyDTO]{List: list, Total: int(total)}, nil
}

func (s *Store) AuthenticateKey(ctx context.Context, keyHash string) (*domain.AuthContext, error) {
	row, err := s.queries.GetAuthContextByKeyHash(ctx, keyHash)
	if err != nil {
		return nil, mapError(err)
	}
	return &domain.AuthContext{
		KeyID:              int(row.KeyID),
		UserID:             int(row.UserID),
		KeyName:            row.KeyName,
		KeyActive:          row.KeyActive,
		ExpiresAt:          optionalTimestamp(row.ExpiresAt),
		Permissions:        canonicalJSON(json.RawMessage(row.Permissions)),
		RateLimitOverrides: canonicalJSON(json.RawMessage(row.RateLimitOverrides)),
	}, nil
}

func (s *Store) UpdateKeyLastUsed(ctx context.Context, keyID int) error {
	affected, err := s.queries.UpdateKeyLastUsed(ctx, int64(keyID))
	if err != nil {
		return mapError(err)
	}
	if affected == 0 {
		return store.ErrNotFound
	}
	return nil
}

// --- Tx primitives ---

func (t *Tx) GetUser(id int) (domain.User, error) {
	row, err := t.queries.GetUser(t.ctx, int64(id))
	if err != nil {
		return domain.User{}, mapError(err)
	}
	return domain.User{ID: int(row.ID), Username: row.Username, Nickname: row.Nickname}, nil
}

func (t *Tx) UpdateNickname(id int, nickname string) (bool, error) {
	affected, err := t.queries.UpdateUserProfile(t.ctx, sqlc.UpdateUserProfileParams{Nickname: nickname, ID: int64(id)})
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (t *Tx) UpdatePassword(id int, passwordHash string) (bool, error) {
	affected, err := t.queries.UpdateUserPassword(t.ctx, sqlc.UpdateUserPasswordParams{PasswordHash: passwordHash, ID: int64(id)})
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (t *Tx) GetKey(keyID, userID int) (domain.ClientKey, error) {
	row, err := t.queries.GetKey(t.ctx, sqlc.GetKeyParams{ID: int64(keyID), UserID: int64(userID)})
	if err != nil {
		return domain.ClientKey{}, mapError(err)
	}
	return domain.ClientKey{
		ID:         int(row.ID),
		UserID:     int(row.UserID),
		KeyName:    row.KeyName,
		Prefix:     row.Prefix,
		KeySuffix:  row.KeySuffix,
		IsActive:   row.IsActive,
		CreatedAt:  row.CreatedAt.Time.UTC().Format(time.RFC3339),
		LastUsedAt: optionalTimestamp(row.LastUsedAt),
		ExpiresAt:  optionalTimestamp(row.ExpiresAt),
	}, nil
}

func (t *Tx) InsertKey(in domain.KeyInsert) (int, error) {
	id, err := t.queries.CreateKey(t.ctx, sqlc.CreateKeyParams{
		UserID:             int64(in.UserID),
		KeyName:            in.KeyName,
		Prefix:             in.Prefix,
		KeyHash:            in.KeyHash,
		KeySuffix:          in.KeySuffix,
		Permissions:        []byte(in.Permissions),
		RateLimitOverrides: rawJSON(in.RateLimitOverrides),
		ExpiresAt:          in.ExpiresAt,
		IsActive:           in.IsActive,
	})
	if err != nil {
		return 0, mapError(err)
	}
	return int(id), nil
}

func (t *Tx) UpdateKeyActive(keyID, userID int, active bool) (domain.ClientKey, bool, error) {
	affected, err := t.queries.UpdateKeyActive(t.ctx, sqlc.UpdateKeyActiveParams{IsActive: active, ID: int64(keyID), UserID: int64(userID)})
	if err != nil {
		return domain.ClientKey{}, false, mapError(err)
	}
	if affected == 0 {
		return domain.ClientKey{}, false, nil
	}
	key, err := t.GetKey(keyID, userID)
	if err != nil {
		return domain.ClientKey{}, false, err
	}
	return key, true, nil
}

func (t *Tx) DeleteKey(keyID, userID int) (bool, error) {
	affected, err := t.queries.DeleteKey(t.ctx, sqlc.DeleteKeyParams{ID: int64(keyID), UserID: int64(userID)})
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (t *Tx) DeleteQuotaReservationsForUser(userID int) error {
	return cleanupQuotaReservationsTx(t.ctx, t.tx, userID, 0)
}

func (t *Tx) DeleteQuotaReservationsForKey(userID, keyID int) error {
	return cleanupQuotaReservationsTx(t.ctx, t.tx, userID, keyID)
}

// --- mappings ---

func keyDTO(id int64, keyName, prefix, keySuffix string, isActive bool, createdAt, lastUsedAt, expiresAt pgtype.Timestamptz) domain.ClientKeyDTO {
	return domain.ClientKeyDTO{ID: int(id), KeyName: keyName, Prefix: prefix, KeySuffix: keySuffix, IsActive: isActive, CreatedAt: createdAt.Time.UTC().Format(time.RFC3339), LastUsedAt: optionalTimestamp(lastUsedAt), ExpiresAt: optionalTimestamp(expiresAt)}
}

func limitOffset(page, pageSize int) (int32, int32) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	return int32(pageSize), int32((page - 1) * pageSize)
}

func rawJSON(value json.RawMessage) []byte {
	trimmed := trimJSON(value)
	if trimmed == "" {
		return nil
	}
	return []byte(trimmed)
}

func trimJSON(value json.RawMessage) string {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "null" {
		return ""
	}
	return trimmed
}

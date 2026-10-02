package storefake

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	domain "LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/store"
)

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// --- Port reads ---

func (s *Store) ListKeys(_ context.Context, userID, page, pageSize int) (domain.ListResponse[domain.ClientKeyDTO], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := s.sortedKeysLocked(userID)
	start, end := pageBounds(len(keys), page, pageSize)
	list := []domain.ClientKeyDTO{}
	for _, key := range keys[start:end] {
		list = append(list, keyDTO(key))
	}
	return domain.ListResponse[domain.ClientKeyDTO]{List: list, Total: len(keys)}, nil
}

func (s *Store) AuthenticateKey(_ context.Context, keyHash string) (*domain.AuthContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, key := range s.keys {
		if key.keyHash != keyHash {
			continue
		}
		if _, ok := s.users[key.userID]; !ok {
			return nil, store.ErrNotFound
		}
		return &domain.AuthContext{
			KeyID:              key.id,
			UserID:             key.userID,
			KeyName:            key.keyName,
			KeyActive:          key.isActive,
			ExpiresAt:          key.expiresAt,
			Permissions:        key.permissions,
			RateLimitOverrides: key.rateLimitOverrides,
		}, nil
	}
	return nil, store.ErrNotFound
}

func (s *Store) UpdateKeyLastUsed(_ context.Context, keyID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[keyID]
	if !ok {
		return store.ErrNotFound
	}
	now := nowRFC3339()
	key.lastUsedAt = &now
	return nil
}

func (s *Store) GetUserCredentialsByUsername(_ context.Context, username string) (domain.Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, creds := range s.credentials {
		if creds.Username == username {
			return creds, nil
		}
	}
	return domain.Credentials{}, store.ErrNotFound
}

func (s *Store) GetUserCredentialsByID(_ context.Context, id int) (domain.Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	creds, ok := s.credentials[id]
	if !ok {
		return domain.Credentials{}, store.ErrNotFound
	}
	return creds, nil
}

func (s *Store) GetAccountByID(_ context.Context, id int) (domain.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return domain.Account{}, store.ErrNotFound
	}
	account := domain.Account{ID: id, Nickname: user.Nickname}
	if creds, ok := s.credentials[id]; ok {
		account.Username = creds.Username
	}
	return account, nil
}

func (s *Store) GetSessionByTokenHash(_ context.Context, tokenHash string) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[tokenHash]
	if !ok || !session.ExpiresAt.After(s.now()) {
		return domain.Session{}, store.ErrNotFound
	}
	return session, nil
}

func (s *Store) DeleteSessionByTokenHash(_ context.Context, tokenHash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[tokenHash]; !ok {
		return false, nil
	}
	delete(s.sessions, tokenHash)
	return true, nil
}

func (s *Store) DeleteExpiredSessions(_ context.Context, limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	removed := 0
	for hash, session := range s.sessions {
		if session.ExpiresAt.After(s.now()) {
			continue
		}
		delete(s.sessions, hash)
		removed++
		if removed >= limit {
			break
		}
	}
	return removed, nil
}

// --- Tx primitives ---

// accountsRunner runs the callback while holding the store mutex so the
// primitives observe a consistent snapshot, mirroring a database transaction.
type accountsRunner struct {
	store *Store
}

func (s *Store) AccountsTx() domain.TxManager { return accountsRunner{store: s} }

func (r accountsRunner) InTx(_ context.Context, fn func(domain.Tx) error) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	return fn(&accountsTx{s: r.store})
}

type accountsTx struct {
	s *Store
}

func (t *accountsTx) GetUser(id int) (domain.User, error) {
	user, ok := t.s.users[id]
	if !ok {
		return domain.User{}, store.ErrNotFound
	}
	return *user, nil
}

func (t *accountsTx) UpdateNickname(id int, nickname string) (bool, error) {
	user, ok := t.s.users[id]
	if !ok {
		return false, nil
	}
	user.Nickname = nickname
	if creds, ok := t.s.credentials[id]; ok {
		creds.Nickname = nickname
		t.s.credentials[id] = creds
	}
	return true, nil
}

func (t *accountsTx) UpdatePassword(id int, passwordHash string) (bool, error) {
	creds, ok := t.s.credentials[id]
	if !ok {
		return false, nil
	}
	creds.PasswordHash = passwordHash
	t.s.credentials[id] = creds
	return true, nil
}

func (t *accountsTx) InsertUserWithCredentials(in domain.CredentialsInput) (domain.Account, error) {
	for _, creds := range t.s.credentials {
		if creds.Username == in.Username {
			return domain.Account{}, fmt.Errorf("%w: username already exists", store.ErrInvalid)
		}
	}
	user := &domain.User{ID: t.s.nextUserID, Username: in.Username, Nickname: in.Nickname}
	t.s.nextUserID++
	t.s.users[user.ID] = user
	t.s.credentials[user.ID] = domain.Credentials{UserID: user.ID, Username: in.Username, Nickname: in.Nickname, PasswordHash: in.PasswordHash}
	return domain.Account{ID: user.ID, Username: in.Username, Nickname: in.Nickname}, nil
}

func (t *accountsTx) InsertSession(in domain.SessionInput) (int, error) {
	id := t.s.nextSessionID
	t.s.nextSessionID++
	t.s.sessions[in.TokenHash] = domain.Session{ID: id, UserID: in.UserID, ExpiresAt: in.ExpiresAt}
	return id, nil
}

func (t *accountsTx) GetKey(keyID, userID int) (domain.ClientKey, error) {
	key, ok := t.s.keys[keyID]
	if !ok || key.userID != userID {
		return domain.ClientKey{}, store.ErrNotFound
	}
	return clientKey(key), nil
}

func (t *accountsTx) InsertKey(in domain.KeyInsert) (int, error) {
	key := &memoryKey{
		id:                 t.s.nextKeyID,
		userID:             in.UserID,
		keyName:            in.KeyName,
		prefix:             in.Prefix,
		keySuffix:          in.KeySuffix,
		keyHash:            in.KeyHash,
		permissions:        canonicalJSON(normalizeJSON(in.Permissions, defaultPermissions)),
		rateLimitOverrides: canonicalJSON(normalizeJSON(in.RateLimitOverrides, "")),
		isActive:           in.IsActive,
		createdAt:          t.s.now().UTC().Format(time.RFC3339),
		expiresAt:          normalizeTimestampPtr(in.ExpiresAt),
	}
	t.s.nextKeyID++
	t.s.keys[key.id] = key
	return key.id, nil
}

func (t *accountsTx) UpdateKeyActive(keyID, userID int, active bool) (domain.ClientKey, bool, error) {
	key, ok := t.s.keys[keyID]
	if !ok || key.userID != userID {
		return domain.ClientKey{}, false, nil
	}
	key.isActive = active
	return clientKey(key), true, nil
}

func (t *accountsTx) DeleteKey(keyID, userID int) (bool, error) {
	key, ok := t.s.keys[keyID]
	if !ok || key.userID != userID {
		return false, nil
	}
	t.s.cleanupQuotaLocked(userID, keyID)
	delete(t.s.keys, keyID)
	return true, nil
}

func (t *accountsTx) DeleteQuotaReservationsForUser(userID int) error {
	t.s.cleanupQuotaLocked(userID, 0)
	return nil
}

func (t *accountsTx) DeleteQuotaReservationsForKey(userID, keyID int) error {
	t.s.cleanupQuotaLocked(userID, keyID)
	return nil
}

// SeedUser inserts a credential user directly, bypassing bcrypt, for tests
// that only need an identity. It is test-only.
func (s *Store) SeedUser(username, passwordHash string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextUserID
	s.nextUserID++
	s.users[id] = &domain.User{ID: id, Username: username, Nickname: username}
	s.credentials[id] = domain.Credentials{UserID: id, Username: username, Nickname: username, PasswordHash: passwordHash}
	return id
}

// --- mappings ---

func keyDTO(key *memoryKey) domain.ClientKeyDTO {
	return domain.ClientKeyDTO{ID: key.id, KeyName: key.keyName, Prefix: key.prefix, KeySuffix: key.keySuffix, IsActive: key.isActive, CreatedAt: key.createdAt, LastUsedAt: key.lastUsedAt, ExpiresAt: key.expiresAt}
}

func clientKey(key *memoryKey) domain.ClientKey {
	return domain.ClientKey{ID: key.id, UserID: key.userID, KeyName: key.keyName, Prefix: key.prefix, KeySuffix: key.keySuffix, IsActive: key.isActive, CreatedAt: key.createdAt, LastUsedAt: key.lastUsedAt, ExpiresAt: key.expiresAt}
}

func (s *Store) sortedKeysLocked(userID int) []*memoryKey {
	keys := []*memoryKey{}
	for _, key := range s.keys {
		if userID == 0 || key.userID == userID {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].id < keys[j].id })
	return keys
}

const defaultPermissions = `{"models":["*"]}`

func normalizeJSON(value json.RawMessage, fallback string) json.RawMessage {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" || trimmed == "null" {
		if fallback == "" {
			return nil
		}
		return json.RawMessage(fallback)
	}
	return value
}

// normalizeTimestampPtr parses an RFC3339 timestamp and returns it in UTC so
// the fake store matches the PostgreSQL timestamptz output. Unparseable
// values are preserved as-is.
func normalizeTimestampPtr(value string) *string {
	if value == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		formatted := parsed.UTC().Format(time.RFC3339)
		return &formatted
	}
	return &value
}

func pageBounds(total, page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}

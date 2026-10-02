package accounts

import (
	"context"
	"encoding/json"
)

// Port covers account reads and single-statement writes that need no
// orchestration. Multistep and rule-bearing operations live on Server and use
// a TxManager.
type Port interface {
	// ListKeys lists the gateway keys owned by userID.
	ListKeys(ctx context.Context, userID, page, pageSize int) (ListResponse[ClientKeyDTO], error)

	// AuthenticateKey looks up a gateway key by its hash and returns the raw
	// key + user authentication state. Missing keys return ErrNotFound.
	AuthenticateKey(ctx context.Context, keyHash string) (*AuthContext, error)
	// UpdateKeyLastUsed records key usage. Missing keys return ErrNotFound.
	UpdateKeyLastUsed(ctx context.Context, keyID int) error

	// GetUserCredentialsByUsername reads the login identity for a username.
	// Missing usernames return ErrNotFound.
	GetUserCredentialsByUsername(ctx context.Context, username string) (Credentials, error)
	// GetUserCredentialsByID reads the login identity for a user id, used by
	// profile password changes. Missing users return ErrNotFound.
	GetUserCredentialsByID(ctx context.Context, id int) (Credentials, error)
	// GetAccountByID reads the self-view of a user. Missing users return ErrNotFound.
	GetAccountByID(ctx context.Context, id int) (Account, error)
	// GetSessionByTokenHash returns the live session for a token hash. Expired
	// sessions and unknown tokens both return ErrNotFound.
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	// DeleteSessionByTokenHash removes a session; found=false means no such token.
	DeleteSessionByTokenHash(ctx context.Context, tokenHash string) (found bool, err error)
	// DeleteExpiredSessions removes at most limit expired sessions and reports
	// how many rows were removed. It is intended for the background reaper.
	DeleteExpiredSessions(ctx context.Context, limit int) (int, error)
}

// Tx is the transaction-scoped persistence surface. It exposes only CRUD,
// locking and existence primitives: implementations must not encode business
// rules or multi-step flows, which belong to Server.
type Tx interface {
	GetUser(id int) (User, error)
	UpdateNickname(id int, nickname string) (bool, error)
	UpdatePassword(id int, passwordHash string) (bool, error)

	InsertUserWithCredentials(in CredentialsInput) (Account, error)
	InsertSession(in SessionInput) (int, error)

	GetKey(keyID, userID int) (ClientKey, error)
	InsertKey(in KeyInsert) (int, error)
	UpdateKeyActive(keyID, userID int, active bool) (ClientKey, bool, error)
	DeleteKey(keyID, userID int) (bool, error)

	// DeleteQuotaReservationsForUser and DeleteQuotaReservationsForKey release
	// and remove quota reservations referencing the identity. Identity deletion
	// and quota admission can overlap, so both must run in the caller's
	// transaction to preserve the shared lock order.
	DeleteQuotaReservationsForUser(userID int) error
	DeleteQuotaReservationsForKey(userID, keyID int) error
}

// TxManager runs fn inside a single database transaction.
type TxManager interface {
	InTx(ctx context.Context, fn func(Tx) error) error
}

// KeyInsert describes a gateway key row to persist. KeyHash must already be a
// hash of the plaintext key; plaintext is never stored.
type KeyInsert struct {
	UserID             int
	KeyName            string
	Prefix             string
	KeyHash            string
	KeySuffix          string
	Permissions        json.RawMessage
	RateLimitOverrides json.RawMessage
	ExpiresAt          string
	IsActive           bool
}

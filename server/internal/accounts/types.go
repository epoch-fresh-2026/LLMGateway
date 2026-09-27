package accounts

import (
	"encoding/json"
	"time"

	"LLMGateway/server/internal/pagination"
)

type ListResponse[T any] = pagination.List[T]

type User struct {
	ID               int
	Nickname         string
	UserGroup        string
	Status           string
	AvailableBalance string
	FrozenBalance    string
}

type UserDTO struct {
	ID        int        `json:"id"`
	Nickname  string     `json:"nickname"`
	UserGroup string     `json:"user_group"`
	Status    string     `json:"status"`
	Balance   BalanceDTO `json:"balance"`
}

type BalanceDTO struct {
	AvailableBalance string `json:"available_balance"`
	FrozenBalance    string `json:"frozen_balance"`
}

type BalanceUpdateDTO struct {
	BalanceAfter string `json:"balance_after"`
}

type UserInput struct {
	Nickname  string `json:"nickname"`
	UserGroup string `json:"user_group"`
	Status    string `json:"status"`
	// Password is accepted for API compatibility only. This version does not
	// implement login and never persists a password.
	Password string `json:"password"`
}

type UserStatusInput struct {
	Status string `json:"status"`
}

type RechargeInput struct {
	Amount         string `json:"amount"`
	RelatedOrderID string `json:"related_order_id"`
	Description    string `json:"description"`
}

type BalanceTransactionDTO struct {
	ID           int    `json:"id"`
	TxType       string `json:"tx_type"`
	Amount       string `json:"amount"`
	BalanceAfter string `json:"balance_after"`
	CreatedAt    string `json:"created_at"`
}

type ClientKey struct {
	ID         int     `json:"id"`
	UserID     int     `json:"user_id"`
	KeyName    string  `json:"key_name"`
	Prefix     string  `json:"prefix"`
	IsActive   bool    `json:"is_active"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
}

type ClientKeyDTO struct {
	ID         int     `json:"id"`
	UserID     int     `json:"user_id"`
	KeyName    string  `json:"key_name"`
	Prefix     string  `json:"prefix"`
	IsActive   bool    `json:"is_active"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
}

type KeySecretDTO struct {
	ID      int    `json:"id,omitempty"`
	FullKey string `json:"full_key"`
}

type KeyInput struct {
	KeyName            string          `json:"key_name"`
	Prefix             string          `json:"prefix"`
	Permissions        json.RawMessage `json:"permissions"`
	RateLimitOverrides json.RawMessage `json:"rate_limit_overrides"`
	ExpiresAt          string          `json:"expires_at"`
	IsActive           *bool           `json:"is_active"`
}

type KeyUpdateInput struct {
	IsActive *bool `json:"is_active"`
}

// Credentials is the stored login identity for a self-service account.
// PasswordHash is a bcrypt hash and must never be returned in a DTO.
type Credentials struct {
	UserID       int
	Username     string
	Nickname     string
	PasswordHash string
}

// Account is the self-view of an authenticated user, returned by /admin/auth/me.
type Account struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
}

// CredentialsInput creates a user together with its login credentials.
type CredentialsInput struct {
	Username     string
	Nickname     string
	PasswordHash string
}

// Session is a persisted server-side session. Only the token hash is stored.
type Session struct {
	ID        int
	UserID    int
	ExpiresAt time.Time
}

// SessionInput persists a new session row for a user.
type SessionInput struct {
	TokenHash string
	UserID    int
	ExpiresAt time.Time
}

// AuthContext is the raw authentication state for a downstream gateway key.
// It intentionally excludes the key hash and any plaintext secret; #6 decides
// the HTTP semantics (401/403/402) from these fields.
type AuthContext struct {
	KeyID              int
	UserID             int
	KeyName            string
	KeyActive          bool
	ExpiresAt          *string
	Permissions        json.RawMessage
	RateLimitOverrides json.RawMessage
	UserStatus         string
	AvailableBalance   string
	FrozenBalance      string
}

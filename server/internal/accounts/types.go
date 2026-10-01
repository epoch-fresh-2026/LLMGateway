package accounts

import (
	"encoding/json"
	"time"

	"LLMGateway/server/internal/pagination"
)

type ListResponse[T any] = pagination.List[T]

// User is the minimal credential-centric account record used by key creation.
type User struct {
	ID       int
	Username string
	Nickname string
}

type ClientKey struct {
	ID         int     `json:"id"`
	UserID     int     `json:"user_id"`
	KeyName    string  `json:"key_name"`
	Prefix     string  `json:"prefix"`
	KeySuffix  string  `json:"key_suffix"`
	IsActive   bool    `json:"is_active"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
}

type ClientKeyDTO struct {
	ID         int     `json:"id"`
	KeyName    string  `json:"key_name"`
	Prefix     string  `json:"prefix"`
	KeySuffix  string  `json:"key_suffix"`
	IsActive   bool    `json:"is_active"`
	CreatedAt  string  `json:"created_at"`
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

// ProfileUpdateInput updates the self-service account. When NewPassword is set
// the CurrentPassword must match, so a stolen session cannot rotate the
// password silently.
type ProfileUpdateInput struct {
	Nickname        *string `json:"nickname"`
	CurrentPassword string  `json:"current_password"`
	NewPassword     string  `json:"new_password"`
}

// AuthContext is the raw authentication state for a downstream gateway key.
// It intentionally excludes the key hash and any plaintext secret; the HTTP
// layer decides the 401/403 semantics from these fields.
type AuthContext struct {
	KeyID              int
	UserID             int
	KeyName            string
	KeyActive          bool
	ExpiresAt          *string
	Permissions        json.RawMessage
	RateLimitOverrides json.RawMessage
}

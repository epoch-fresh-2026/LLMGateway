package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"LLMGateway/server/internal/crypto"
	apperrors "LLMGateway/server/internal/errors"
)

// ErrRegistrationDisabled is returned when public self-registration is off.
var ErrRegistrationDisabled = errors.New("registration disabled")

// ErrInvalidCredentials reports a wrong password. It is deliberately distinct
// from ErrUserNotFound so callers can render specific copy; deployments that
// care about username enumeration should treat both as one failure.
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrUserNotFound reports a login attempt for an unknown username.
var ErrUserNotFound = errors.New("user not found")

// ErrUsernameTaken reports a registration whose username already exists.
var ErrUsernameTaken = errors.New("username taken")

// Credential-shape errors. They carry no HTTP status by themselves; the
// accounts HTTP layer maps each to a stable error_code.
var (
	ErrUsernameLength     = errors.New("username length")
	ErrUsernameWhitespace = errors.New("username whitespace")
	ErrPasswordLength     = errors.New("password length")
)

// LoginResult carries the freshly minted session token and the account it
// belongs to. Token is plaintext and is delivered to the browser once.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	Account   Account
}

// Register creates a self-service account and immediately establishes a
// session (registration logs the user in). A duplicate username surfaces as
// ErrUsernameTaken; the unique index is the authoritative guard, and the
// post-error lookup keeps the sentinel independent of database error text.
func (a *Server) Register(ctx context.Context, username, password string) (LoginResult, error) {
	if !a.auth.RegistrationOpen {
		return LoginResult{}, ErrRegistrationDisabled
	}
	username = strings.TrimSpace(username)
	if err := validateCredentials(username, password); err != nil {
		return LoginResult{}, err
	}
	hash, err := crypto.HashPassword(password, a.auth.BcryptCost)
	if err != nil {
		return LoginResult{}, err
	}
	token, expiresAt := a.newSessionToken()

	var account Account
	err = a.tx.InTx(ctx, func(tx Tx) error {
		created, err := tx.InsertUserWithCredentials(CredentialsInput{
			Username:     username,
			Nickname:     username,
			PasswordHash: hash,
		})
		if err != nil {
			return err
		}
		if _, err := tx.InsertSession(SessionInput{
			TokenHash: crypto.HashSessionToken(token),
			UserID:    created.ID,
			ExpiresAt: expiresAt,
		}); err != nil {
			return err
		}
		account = created
		return nil
	})
	if err != nil {
		// The unique index is the race-safe guard. Re-check after rollback so a
		// duplicate is a stable sentinel rather than a database-specific message.
		if errors.Is(err, apperrors.ErrInvalid) {
			if _, lookupErr := a.store.GetUserCredentialsByUsername(ctx, username); lookupErr == nil {
				return LoginResult{}, ErrUsernameTaken
			}
		}
		return LoginResult{}, err
	}
	return LoginResult{Token: token, ExpiresAt: expiresAt, Account: account}, nil
}

// Login verifies credentials and creates a new session (avoiding session
// fixation by always minting a fresh token).
func (a *Server) Login(ctx context.Context, username, password string) (LoginResult, error) {
	creds, err := a.store.GetUserCredentialsByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return LoginResult{}, ErrUserNotFound
		}
		return LoginResult{}, err
	}
	if !crypto.VerifyPassword(creds.PasswordHash, password) {
		return LoginResult{}, ErrInvalidCredentials
	}

	token, expiresAt := a.newSessionToken()
	err = a.tx.InTx(ctx, func(tx Tx) error {
		_, err := tx.InsertSession(SessionInput{
			TokenHash: crypto.HashSessionToken(token),
			UserID:    creds.UserID,
			ExpiresAt: expiresAt,
		})
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		Token:     token,
		ExpiresAt: expiresAt,
		Account:   Account{ID: creds.UserID, Username: creds.Username, Nickname: creds.Nickname},
	}, nil
}

// Logout deletes the session for a token. A blank or already-deleted token is a
// no-op so logout is idempotent.
func (a *Server) Logout(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	_, err := a.store.DeleteSessionByTokenHash(ctx, crypto.HashSessionToken(token))
	return err
}

// AuthenticateSession resolves a session token to its account. Unknown,
// deleted or expired tokens all return ErrNotFound.
func (a *Server) AuthenticateSession(ctx context.Context, token string) (Account, error) {
	if strings.TrimSpace(token) == "" {
		return Account{}, apperrors.ErrNotFound
	}
	session, err := a.store.GetSessionByTokenHash(ctx, crypto.HashSessionToken(token))
	if err != nil {
		return Account{}, err
	}
	if !session.ExpiresAt.After(a.now()) {
		return Account{}, apperrors.ErrNotFound
	}
	return a.store.GetAccountByID(ctx, session.UserID)
}

// ReapExpiredSessions deletes up to limit expired sessions for the background
// worker. It is best-effort: callers log and continue on error.
func (a *Server) ReapExpiredSessions(ctx context.Context, limit int) (int, error) {
	return a.store.DeleteExpiredSessions(ctx, limit)
}

func (a *Server) newSessionToken() (string, time.Time) {
	// GenerateSessionToken uses crypto/rand.Read, which is infallible on Go 1.24+.
	token, _ := crypto.GenerateSessionToken()
	return token, a.now().Add(a.auth.SessionTTL)
}

// validateCredentials enforces the username/password shape. Each failure wraps
// both apperrors.ErrInvalid (for generic mapping) and a specific sentinel that
// the HTTP layer turns into a stable error_code.
func validateCredentials(username, password string) error {
	if len(username) < 3 || len(username) > 64 {
		return fmt.Errorf("%w: %w: username must be 3-64 characters", apperrors.ErrInvalid, ErrUsernameLength)
	}
	if strings.ContainsAny(username, " \t\r\n") {
		return fmt.Errorf("%w: %w: username must not contain whitespace", apperrors.ErrInvalid, ErrUsernameWhitespace)
	}
	return validatePassword(password)
}

// validatePassword bounds the password to bcrypt's 72-byte input limit, which
// would otherwise be silently truncated.
func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return fmt.Errorf("%w: %w: password must be 8-72 bytes", apperrors.ErrInvalid, ErrPasswordLength)
	}
	return nil
}

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

// ErrInvalidCredentials is the single login failure returned for both unknown
// usernames and wrong passwords, so responses cannot reveal which accounts exist.
var ErrInvalidCredentials = errors.New("invalid credentials")

// LoginResult carries the freshly minted session token and the account it
// belongs to. Token is plaintext and is delivered to the browser once.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	Account   Account
}

// Register creates a self-service account and immediately establishes a
// session (registration logs the user in). A duplicate username surfaces as
// ErrInvalid from the unique index.
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
	token, expiresAt, err := a.newSessionToken()
	if err != nil {
		return LoginResult{}, err
	}

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
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	if !crypto.VerifyPassword(creds.PasswordHash, password) {
		return LoginResult{}, ErrInvalidCredentials
	}

	token, expiresAt, err := a.newSessionToken()
	if err != nil {
		return LoginResult{}, err
	}
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

func (a *Server) newSessionToken() (string, time.Time, error) {
	token, err := crypto.GenerateSessionToken()
	if err != nil {
		return "", time.Time{}, err
	}
	return token, a.now().Add(a.auth.SessionTTL), nil
}

// validateCredentials enforces the username/password shape.
func validateCredentials(username, password string) error {
	if len(username) < 3 || len(username) > 64 {
		return fmt.Errorf("%w: username must be 3-64 characters", apperrors.ErrInvalid)
	}
	if strings.ContainsAny(username, " \t\r\n") {
		return fmt.Errorf("%w: username must not contain whitespace", apperrors.ErrInvalid)
	}
	return validatePassword(password)
}

// validatePassword bounds the password to bcrypt's 72-byte input limit, which
// would otherwise be silently truncated.
func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return fmt.Errorf("%w: password must be 8-72 bytes", apperrors.ErrInvalid)
	}
	return nil
}

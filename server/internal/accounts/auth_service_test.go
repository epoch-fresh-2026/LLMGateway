package accounts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/store"
	"LLMGateway/server/internal/testutil/storefake"
)

// newAuthServer builds an account server with cheap bcrypt and a controllable
// clock so session-expiry behaviour is deterministic.
func newAuthServer(t *testing.T, now *time.Time) *accounts.Server {
	t.Helper()
	clock := func() time.Time { return *now }
	st := storefake.NewWithClock(clock)
	return accounts.New(st, st.AccountsTx(),
		accounts.WithClock(clock),
		accounts.WithAuthConfig(accounts.AuthConfig{
			SessionTTL:       time.Hour,
			CookieSecure:     false,
			RegistrationOpen: true,
			BcryptCost:       crypto.MinPasswordCost,
		}),
	)
}

func TestAuthRegisterLoginSession(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	srv := newAuthServer(t, &now)
	ctx := context.Background()

	reg, err := srv.Register(ctx, "alice", "password123")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.Token == "" || reg.Account.Username != "alice" {
		t.Fatalf("unexpected register result: %+v", reg)
	}

	account, err := srv.AuthenticateSession(ctx, reg.Token)
	if err != nil {
		t.Fatalf("AuthenticateSession: %v", err)
	}
	if account.ID != reg.Account.ID || account.Username != "alice" {
		t.Fatalf("session account = %+v, want %+v", account, reg.Account)
	}

	if _, err := srv.Register(ctx, "alice", "password123"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("duplicate username err = %v, want ErrInvalid", err)
	}
}

func TestAuthLoginRejectsBadCredentials(t *testing.T) {
	now := time.Now()
	srv := newAuthServer(t, &now)
	ctx := context.Background()

	if _, err := srv.Register(ctx, "bob", "password123"); err != nil {
		t.Fatal(err)
	}

	if _, err := srv.Login(ctx, "bob", "wrong-password"); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("wrong password err = %v, want ErrInvalidCredentials", err)
	}
	if _, err := srv.Login(ctx, "nobody", "password123"); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("unknown user err = %v, want ErrInvalidCredentials", err)
	}

	login, err := srv.Login(ctx, "bob", "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := srv.AuthenticateSession(ctx, login.Token); err != nil {
		t.Fatalf("login session invalid: %v", err)
	}
}

func TestAuthLogoutInvalidatesSession(t *testing.T) {
	now := time.Now()
	srv := newAuthServer(t, &now)
	ctx := context.Background()

	reg, err := srv.Register(ctx, "carol", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Logout(ctx, reg.Token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := srv.AuthenticateSession(ctx, reg.Token); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session after logout err = %v, want ErrNotFound", err)
	}
	// Logout must be idempotent.
	if err := srv.Logout(ctx, reg.Token); err != nil {
		t.Fatalf("second Logout: %v", err)
	}
}

func TestAuthSessionExpiryAndReap(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	srv := newAuthServer(t, &now)
	ctx := context.Background()

	reg, err := srv.Register(ctx, "dave", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.AuthenticateSession(ctx, reg.Token); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Hour)
	if _, err := srv.AuthenticateSession(ctx, reg.Token); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired session err = %v, want ErrNotFound", err)
	}
	reaped, err := srv.ReapExpiredSessions(ctx, 100)
	if err != nil {
		t.Fatalf("ReapExpiredSessions: %v", err)
	}
	if reaped == 0 {
		t.Fatal("expected at least one expired session to be reaped")
	}
}

func TestAuthRegisterValidation(t *testing.T) {
	now := time.Now()
	srv := newAuthServer(t, &now)
	ctx := context.Background()

	for _, tc := range []struct{ username, password string }{
		{"ab", "password123"},
		{"has space", "password123"},
		{"alice", "short"},
	} {
		if _, err := srv.Register(ctx, tc.username, tc.password); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("Register(%q,%q) err = %v, want ErrInvalid", tc.username, tc.password, err)
		}
	}
}

func TestAuthRegistrationDisabled(t *testing.T) {
	now := time.Now()
	st := storefake.New()
	srv := accounts.New(st, st.AccountsTx(),
		accounts.WithClock(func() time.Time { return now }),
		accounts.WithAuthConfig(accounts.AuthConfig{
			SessionTTL:       time.Hour,
			RegistrationOpen: false,
			BcryptCost:       crypto.MinPasswordCost,
		}),
	)
	if _, err := srv.Register(context.Background(), "erin", "password123"); !errors.Is(err, accounts.ErrRegistrationDisabled) {
		t.Fatalf("Register err = %v, want ErrRegistrationDisabled", err)
	}
}

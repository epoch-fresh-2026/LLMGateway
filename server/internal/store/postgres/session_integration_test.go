package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/store"
)

func TestPGRegisterLoginSession(t *testing.T) {
	st := testStore(t)
	now := time.Now()
	acc := accounts.New(st, st.AccountsTx(),
		accounts.WithClock(func() time.Time { return now }),
		accounts.WithAuthConfig(accounts.AuthConfig{
			SessionTTL:       time.Hour,
			RegistrationOpen: true,
			BcryptCost:       crypto.MinPasswordCost,
		}),
	)
	ctx := context.Background()

	reg, err := acc.Register(ctx, "alice", "password123")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.Account.Username != "alice" || reg.Token == "" {
		t.Fatalf("unexpected register result: %+v", reg)
	}

	account, err := acc.AuthenticateSession(ctx, reg.Token)
	if err != nil {
		t.Fatalf("AuthenticateSession: %v", err)
	}
	if account.ID != reg.Account.ID || account.Username != "alice" {
		t.Fatalf("session account = %+v, want %+v", account, reg.Account)
	}

	if _, err := acc.Register(ctx, "alice", "password123"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("duplicate username err = %v, want ErrInvalid", err)
	}
	if _, err := acc.Login(ctx, "alice", "wrong-password"); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("wrong password err = %v, want ErrInvalidCredentials", err)
	}

	login, err := acc.Login(ctx, "alice", "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := acc.Logout(ctx, login.Token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := acc.AuthenticateSession(ctx, login.Token); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session after logout err = %v, want ErrNotFound", err)
	}
}

func TestPGSessionExpiryAndReap(t *testing.T) {
	st := testStore(t)
	now := time.Now()
	acc := accounts.New(st, st.AccountsTx(),
		accounts.WithClock(func() time.Time { return now }),
		accounts.WithAuthConfig(accounts.AuthConfig{
			SessionTTL:       time.Hour,
			RegistrationOpen: true,
			BcryptCost:       crypto.MinPasswordCost,
		}),
	)
	ctx := context.Background()

	reg, err := acc.Register(ctx, "carol", "password123")
	if err != nil {
		t.Fatal(err)
	}

	// The service's clock marks the session expired even though the database
	// clock (used by the reaper) has not advanced.
	now = now.Add(2 * time.Hour)
	if _, err := acc.AuthenticateSession(ctx, reg.Token); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired session err = %v, want ErrNotFound", err)
	}

	// Insert a row that is already expired relative to the database clock so
	// the reaper has something deterministic to delete.
	expired := time.Now().Add(-time.Hour)
	if err := st.AccountsTx().InTx(ctx, func(tx accounts.Tx) error {
		_, err := tx.InsertSession(accounts.SessionInput{TokenHash: "expired-token", UserID: reg.Account.ID, ExpiresAt: expired})
		return err
	}); err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	reaped, err := acc.ReapExpiredSessions(ctx, 100)
	if err != nil {
		t.Fatalf("ReapExpiredSessions: %v", err)
	}
	if reaped != 1 {
		t.Fatalf("reaped = %d, want 1", reaped)
	}
}

package storefake

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/store"
)

func TestCredentialsAndProfile(t *testing.T) {
	st := New()
	userID := st.SeedUser("alice", "hash")

	creds, err := st.GetUserCredentialsByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetUserCredentialsByUsername: %v", err)
	}
	if creds.UserID != userID || creds.PasswordHash != "hash" {
		t.Fatalf("credentials = %+v", creds)
	}
	if _, err := st.GetUserCredentialsByUsername(context.Background(), "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing username err = %v, want ErrNotFound", err)
	}

	if err := st.AccountsTx().InTx(context.Background(), func(tx domain.Tx) error {
		if _, err := tx.UpdateNickname(userID, "Alice"); err != nil {
			return err
		}
		_, err := tx.UpdatePassword(userID, "newhash")
		return err
	}); err != nil {
		t.Fatalf("profile tx: %v", err)
	}

	account, err := st.GetAccountByID(context.Background(), userID)
	if err != nil || account.Nickname != "Alice" {
		t.Fatalf("account = %+v, %v", account, err)
	}
	updated, _ := st.GetUserCredentialsByID(context.Background(), userID)
	if updated.PasswordHash != "newhash" || updated.Nickname != "Alice" {
		t.Fatalf("credentials not updated: %+v", updated)
	}
}

func TestSessionLifecycle(t *testing.T) {
	now := time.Unix(0, 0)
	st := NewWithClock(func() time.Time { return now })
	userID := st.SeedUser("alice", "hash")
	ctx := context.Background()

	if err := st.AccountsTx().InTx(ctx, func(tx domain.Tx) error {
		_, err := tx.InsertSession(domain.SessionInput{TokenHash: "abc", UserID: userID, ExpiresAt: now.Add(time.Hour)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSessionByTokenHash(ctx, "abc"); err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	if _, err := st.DeleteSessionByTokenHash(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSessionByTokenHash(ctx, "abc"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted session err = %v, want ErrNotFound", err)
	}
}

func TestAuthenticateKeyHasNoBillingState(t *testing.T) {
	st := New()
	userID := st.SeedUser("alice", "hash")
	acc := newAccounts(st)
	key, err := acc.CreateKey(context.Background(), userID, domain.KeyInput{})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := st.AuthenticateKey(context.Background(), domainHash(key.FullKey))
	if err != nil {
		t.Fatalf("AuthenticateKey: %v", err)
	}
	if auth.UserID != userID || !auth.KeyActive {
		t.Fatalf("auth = %+v", auth)
	}
}

func TestKeyListIsSelfScoped(t *testing.T) {
	st := New()
	aliceID := st.SeedUser("alice", "hash")
	bobID := st.SeedUser("bob", "hash")
	acc := newAccounts(st)
	if _, err := acc.CreateKey(context.Background(), aliceID, domain.KeyInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.CreateKey(context.Background(), bobID, domain.KeyInput{}); err != nil {
		t.Fatal(err)
	}

	aliceKeys, err := st.ListKeys(context.Background(), aliceID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if aliceKeys.Total != 1 {
		t.Fatalf("alice keys = %d, want 1", aliceKeys.Total)
	}
	bobKeys, _ := st.ListKeys(context.Background(), bobID, 1, 20)
	if bobKeys.Total != 1 {
		t.Fatalf("bob keys = %d, want 1", bobKeys.Total)
	}
}

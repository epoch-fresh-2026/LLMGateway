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

func TestPGProfileAndKeys(t *testing.T) {
	st := testStore(t)
	acc := accounts.New(st, st.AccountsTx())
	ctx := context.Background()
	owner := testOwner(t, st)

	account, err := acc.Profile(ctx, owner)
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if account.ID != owner {
		t.Fatalf("profile = %+v", account)
	}

	nickname := "Renamed"
	if _, err := acc.UpdateProfile(ctx, owner, accounts.ProfileUpdateInput{Nickname: &nickname}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	updated, _ := st.GetAccountByID(ctx, owner)
	if updated.Nickname != "Renamed" {
		t.Fatalf("nickname not updated: %+v", updated)
	}

	hash, err := crypto.HashPassword("newpassword123", crypto.MinPasswordCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AccountsTx().InTx(ctx, func(tx accounts.Tx) error {
		_, err := tx.UpdatePassword(owner, hash)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	creds, _ := st.GetUserCredentialsByID(ctx, owner)
	if !crypto.VerifyPassword(creds.PasswordHash, "newpassword123") {
		t.Fatal("password hash not updated")
	}

	created, err := acc.CreateKey(ctx, owner, accounts.KeyInput{KeyName: "first", Prefix: "sk-"})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if _, err := acc.CreateKey(ctx, 404, accounts.KeyInput{}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CreateKey missing user err = %v, want ErrNotFound", err)
	}

	listed, err := st.ListKeys(ctx, owner, 1, 20)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if listed.Total != 1 || listed.List[0].ID != created.ID {
		t.Fatalf("keys = %+v", listed)
	}
	var persistedCreatedAt time.Time
	if err := st.pool.QueryRow(ctx, "SELECT created_at FROM client_api_keys WHERE id = $1", created.ID).Scan(&persistedCreatedAt); err != nil {
		t.Fatalf("read creation timestamp: %v", err)
	}
	if listed.List[0].CreatedAt != persistedCreatedAt.UTC().Format(time.RFC3339) {
		t.Fatal("listed created_at does not match stored timestamp")
	}
	if err := st.UpdateKeyLastUsed(ctx, created.ID); err != nil {
		t.Fatalf("UpdateKeyLastUsed: %v", err)
	}

	toggled, err := acc.UpdateKey(ctx, owner, created.ID, accounts.KeyUpdateInput{IsActive: boolPtr(false)})
	if err != nil || toggled.IsActive {
		t.Fatalf("UpdateKey = %+v, %v", toggled, err)
	}
	if toggled.CreatedAt != listed.List[0].CreatedAt {
		t.Fatal("created_at changed after usage or update")
	}
	reset, err := acc.ResetKey(ctx, owner, created.ID)
	if err != nil || reset.FullKey == "" || reset.FullKey == created.FullKey {
		t.Fatalf("ResetKey = %+v, %v", reset, err)
	}
	if err := acc.DeleteKey(ctx, owner, created.ID); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if err := acc.DeleteKey(ctx, owner, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second DeleteKey err = %v, want ErrNotFound", err)
	}
}

func boolPtr(value bool) *bool { return &value }

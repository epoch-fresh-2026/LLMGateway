package accounts_test

import (
	"context"
	"errors"
	"testing"

	"LLMGateway/server/internal/store"
	"LLMGateway/server/internal/testutil/app"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func boolPtr(value bool) *bool { return &value }

func TestServiceProfileAndKeys(t *testing.T) {
	deps := app.New()
	acc := deps.Accounts
	ctx := context.Background()
	userID := deps.Store.SeedUser("alice", "hash")

	account, err := acc.Profile(ctx, userID)
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if account.Username != "alice" {
		t.Fatalf("profile = %+v", account)
	}

	nickname := "Alice"
	updated, err := acc.UpdateProfile(ctx, userID, domain.ProfileUpdateInput{Nickname: &nickname})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.Nickname != "Alice" {
		t.Fatalf("updated profile = %+v", updated)
	}

	key, err := acc.CreateKey(ctx, userID, domain.KeyInput{KeyName: "default", Prefix: "sk-"})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if key.FullKey == "" {
		t.Fatal("full_key missing")
	}
	if _, err := acc.CreateKey(ctx, 404, domain.KeyInput{}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CreateKey missing user err = %v, want ErrNotFound", err)
	}

	toggled, err := acc.UpdateKey(ctx, userID, key.ID, domain.KeyUpdateInput{IsActive: boolPtr(false)})
	if err != nil {
		t.Fatalf("UpdateKey: %v", err)
	}
	if toggled.IsActive {
		t.Fatal("key not deactivated")
	}
	if _, err := acc.UpdateKey(ctx, userID, key.ID, domain.KeyUpdateInput{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("missing is_active err = %v, want ErrInvalid", err)
	}

	if err := acc.DeleteKey(ctx, userID, key.ID); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if err := acc.DeleteKey(ctx, userID, key.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second DeleteKey err = %v, want ErrNotFound", err)
	}
}

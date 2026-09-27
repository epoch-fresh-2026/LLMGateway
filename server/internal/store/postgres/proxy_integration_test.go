package postgres

import (
	"context"
	"errors"
	"testing"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func createKeyAndHash(t *testing.T, acc *accounts.Server, userID int) (int, string) {
	t.Helper()
	created, err := acc.CreateKey(context.Background(), userID, domain.KeyInput{KeyName: "default", Prefix: "sk-"})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	return created.ID, crypto.HashKey(created.FullKey)
}

func TestPGProxyStoreCapabilities(t *testing.T) {
	st := testStore(t)
	cat := testCatalog(t, st)
	acc := accounts.New(st, st.AccountsTx())
	owner := testOwner(t, st)
	ctx := context.Background()

	keyID, keyHash := createKeyAndHash(t, acc, owner)
	auth, err := st.AuthenticateKey(ctx, keyHash)
	if err != nil {
		t.Fatalf("AuthenticateKey: %v", err)
	}
	if auth.KeyID != keyID || auth.UserID != owner || !auth.KeyActive {
		t.Fatalf("unexpected auth context: %+v", auth)
	}
	if _, err := st.AuthenticateKey(ctx, crypto.HashKey("sk-unknown")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown key err = %v, want ErrNotFound", err)
	}
	if err := st.UpdateKeyLastUsed(ctx, keyID); err != nil {
		t.Fatalf("UpdateKeyLastUsed: %v", err)
	}
	if err := st.UpdateKeyLastUsed(ctx, 404); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing key err = %v, want ErrNotFound", err)
	}

	created, err := cat.CreateChannel(ctx, owner, domain.ChannelInput{Name: "A", BaseURL: "https://a.test", APIKey: "sk", Status: 1, Priority: 10, Weight: 100, Balance: strPtr("5.000000")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(ctx, owner, created.ID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up-gpt", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.UpsertPricing(ctx, owner, domain.PricingInput{ChannelID: created.ID, ModelName: "gpt", InputPricePer1M: "0.10000000", OutputPricePer1M: "0.20000000", CachedInputPricePer1M: "0.05000000", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	pricing, err := st.GetPricing(ctx, created.ID, "gpt")
	if err != nil {
		t.Fatalf("GetPricing: %v", err)
	}
	if pricing.InputPricePer1M != "0.10000000" || pricing.UpstreamModel != "up-gpt" {
		t.Fatalf("unexpected pricing: %+v", pricing)
	}
	if _, err := st.GetPricing(ctx, created.ID, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing pricing err = %v, want ErrNotFound", err)
	}

	candidates, err := cat.RouteCandidates(ctx, owner, "gpt")
	if err != nil {
		t.Fatalf("RouteCandidates: %v", err)
	}
	if candidates.Total != 1 || candidates.List[0].ChannelID != created.ID {
		t.Fatalf("candidates = %+v", candidates)
	}

	if _, err := st.InsertUsageLog(ctx, domain.UsageLogInput{RequestID: "req-1", UserID: &owner, APIKeyID: &keyID, ChannelID: &created.ID, Model: "gpt", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertUsageLog(ctx, domain.UsageLogInput{RequestID: "req-2", UserID: &owner, APIKeyID: &keyID, ChannelID: &created.ID, Model: "gpt", Status: "error"}); err != nil {
		t.Fatal(err)
	}
	count, err := st.CountRequestsSince(ctx, domain.UsageCountFilter{UserID: owner, Since: "1970-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

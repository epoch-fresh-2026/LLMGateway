package storefake

import (
	"context"
	"errors"
	"testing"

	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func strPtr(value string) *string {
	return &value
}

func createTestChannel(t *testing.T, cat *catalog.Server, balance *string) domain.ChannelDTO {
	t.Helper()
	created, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk-secret", Status: 1, Balance: balance})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	return created
}

func TestCreateChannelNormalizesBalance(t *testing.T) {
	st := New()
	cat := newCatalog(st)

	created := createTestChannel(t, cat, strPtr("10.5"))
	if got := *created.Balance; got != "10.500000" {
		t.Fatalf("balance = %q, want 10.500000", got)
	}

	empty := createTestChannel(t, cat, strPtr(""))
	if empty.Balance != nil {
		t.Fatalf("empty balance should be nil, got %v", empty.Balance)
	}

	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "bad", BaseURL: "https://api.test", APIKey: "sk-secret", Balance: strPtr("abc")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid balance err = %v, want ErrInvalid", err)
	}
}

func TestUpdateChannelNormalizesBalance(t *testing.T) {
	st := New()
	cat := newCatalog(st)
	createTestChannel(t, cat, strPtr("1.000000"))

	updated, err := cat.UpdateChannel(context.Background(), 1, 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", Balance: strPtr("10.5")})
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}
	if got := *updated.Balance; got != "10.500000" {
		t.Fatalf("balance = %q, want 10.500000", got)
	}

	if _, err := cat.UpdateChannel(context.Background(), 1, 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", Balance: strPtr("bad")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid balance err = %v, want ErrInvalid", err)
	}
}

func TestCreateChannelModelRejectsDuplicate(t *testing.T) {
	st := New()
	cat := newCatalog(st)
	createTestChannel(t, cat, nil)

	if _, err := cat.CreateChannelModel(context.Background(), 1, 1, domain.ChannelModel{ModelName: "gpt-4o-mini", UpstreamModel: "gpt-4o-mini-up", Enabled: true}); err != nil {
		t.Fatalf("first mapping: %v", err)
	}
	if _, err := cat.CreateChannelModel(context.Background(), 1, 1, domain.ChannelModel{ModelName: "gpt-4o-mini", UpstreamModel: "other", Enabled: true}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("duplicate mapping err = %v, want ErrInvalid", err)
	}
}

func TestUpdateChannelModelRenamesAliasWithoutTouchingPricing(t *testing.T) {
	st := New()
	cat := newCatalog(st)
	channel := createTestChannel(t, cat, nil)
	mapping, err := cat.CreateChannelModel(context.Background(), 1, channel.ID, domain.ChannelModel{ModelName: "alias", UpstreamModel: "real", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: channel.ID, UpstreamModel: "real", InputPricePer1M: "0.1", OutputPricePer1M: "0.2", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}

	renamed, err := cat.UpdateChannelModel(context.Background(), 1, channel.ID, mapping.ID, "renamed", true)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.ModelName != "renamed" || renamed.UpstreamModel != "real" {
		t.Fatalf("renamed = %+v", renamed)
	}

	// Pricing is keyed by the upstream model, so renaming the alias keeps it.
	prices, err := cat.ListPricing(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(prices.List) != 1 || prices.List[0].UpstreamModel != "real" {
		t.Fatalf("pricing after rename = %+v, want upstream real kept", prices.List)
	}

	if _, err := cat.CreateChannelModel(context.Background(), 1, channel.ID, domain.ChannelModel{ModelName: "other", UpstreamModel: "other", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.UpdateChannelModel(context.Background(), 1, channel.ID, mapping.ID, "other", true); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("duplicate rename err = %v, want ErrInvalid", err)
	}
}

func TestMultipleAliasesShareUpstreamPricing(t *testing.T) {
	st := New()
	cat := newCatalog(st)
	channel := createTestChannel(t, cat, nil)
	for _, alias := range []string{"a", "b"} {
		if _, err := cat.CreateChannelModel(context.Background(), 1, channel.ID, domain.ChannelModel{ModelName: alias, UpstreamModel: "real", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: channel.ID, UpstreamModel: "real", InputPricePer1M: "0.1", OutputPricePer1M: "0.2", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	prices, err := cat.ListPricing(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(prices.List) != 1 {
		t.Fatalf("pricing rows = %d, want 1 shared row", len(prices.List))
	}
}

func TestUpsertPricingNormalizesAndValidates(t *testing.T) {
	st := New()
	cat := newCatalog(st)
	createTestChannel(t, cat, nil)
	if _, err := cat.CreateChannelModel(context.Background(), 1, 1, domain.ChannelModel{ModelName: "gpt-4o-mini", UpstreamModel: "gpt-4o-mini-up", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	dto, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: 1, UpstreamModel: "gpt-4o-mini-up", InputPricePer1M: "0.1", OutputPricePer1M: "0.2", Currency: "USD"})
	if err != nil {
		t.Fatalf("UpsertPricing: %v", err)
	}
	if dto.InputPricePer1M != "0.10000000" || dto.OutputPricePer1M != "0.20000000" {
		t.Fatalf("prices not normalized: %+v", dto)
	}

	if _, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: 1, UpstreamModel: "gpt-4o-mini-up", InputPricePer1M: "bad", OutputPricePer1M: "0.2"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid input price err = %v, want ErrInvalid", err)
	}
	if _, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: 1, UpstreamModel: "gpt-4o-mini-up", InputPricePer1M: "0.1"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("missing output price err = %v, want ErrInvalid", err)
	}
	if _, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: 1, UpstreamModel: "gpt-4o-mini-up", InputPricePer1M: "0.1", OutputPricePer1M: "0.2", CachedInputPricePer1M: "bad"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid cached price err = %v, want ErrInvalid", err)
	}
}

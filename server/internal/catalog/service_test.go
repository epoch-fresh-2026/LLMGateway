package catalog_test

import (
	"context"
	"errors"
	"testing"

	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/store"
	"LLMGateway/server/internal/testutil/app"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func TestServiceChannelLifecycle(t *testing.T) {
	deps := app.New()
	cat := deps.Catalog

	balance := "10.5"
	created, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk-secret", Status: 1, Balance: &balance})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if created.AuthType != "bearer" || created.Weight != 100 {
		t.Fatalf("defaults not applied: %+v", created)
	}
	if created.Balance == nil || *created.Balance != "10.500000" {
		t.Fatalf("balance not normalized: %+v", created.Balance)
	}

	secret, err := cat.GetChannelSecret(context.Background(), 1, created.ID)
	if err != nil {
		t.Fatalf("GetChannelSecret: %v", err)
	}
	if secret.APIKey != "sk-secret" {
		t.Fatalf("api_key = %q, want sk-secret", secret.APIKey)
	}

	adjusted, err := cat.UpdateChannelBalance(context.Background(), 1, created.ID, "", "2.5")
	if err != nil {
		t.Fatalf("UpdateChannelBalance: %v", err)
	}
	if adjusted.Balance == nil || *adjusted.Balance != "13.000000" {
		t.Fatalf("balance after delta = %+v, want 13.000000", adjusted.Balance)
	}
	if _, err := cat.UpdateChannelBalance(context.Background(), 1, created.ID, "", "abc"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid delta err = %v, want ErrInvalid", err)
	}
	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "no-key", BaseURL: "https://x.test"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("missing api_key err = %v, want ErrInvalid", err)
	}

	if err := cat.DeleteChannel(context.Background(), 1, created.ID); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	if err := cat.DeleteChannel(context.Background(), 1, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second DeleteChannel err = %v, want ErrNotFound", err)
	}
}

func TestServiceHealthStateMachine(t *testing.T) {
	deps := app.New()
	cat := deps.Catalog

	channel, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := cat.RecordChannelFailure(context.Background(), channel.ID, catalog.FailureUpstream5xx); err != nil {
			t.Fatal(err)
		}
	}
	health, err := cat.GetChannelHealth(context.Background(), channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if health.State != catalog.HealthOpen {
		t.Fatalf("state = %s, want open", health.State)
	}
	if err := cat.ResetChannelHealth(context.Background(), channel.ID); err != nil {
		t.Fatal(err)
	}
	health, _ = cat.GetChannelHealth(context.Background(), channel.ID)
	if health.State != catalog.HealthClosed || health.FailureCount != 5 || health.ConsecutiveFailures != 0 || health.OpenedAt != nil {
		t.Fatalf("after reset: %+v", health)
	}
}

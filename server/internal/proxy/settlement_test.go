package proxy

import (
	"context"
	"errors"
	"testing"
	"time"

	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/store"
	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func newSettlementService(st *storefake.Store) *Service {
	return NewService(st, newTestCatalog(st), newTestQuota(st, nil), newTestRateLimit(st, nil), nil, func(int) int { return 0 }, time.Now)
}

func TestSettleDebitsChannelAndWritesUsage(t *testing.T) {
	st := storefake.New()
	cat := newTestCatalog(st)
	channelBalance := "5.000000"
	channel, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1, Balance: &channelBalance})
	if err != nil {
		t.Fatal(err)
	}

	usageID, err := newSettlementService(st).Settle(context.Background(), settlement.Input{
		UserID:       1,
		ChannelID:    &channel.ID,
		Cost:         "1.250000",
		DebitChannel: true,
		UsageLog:     successUsageInput("req-1", 1, channel.ID),
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if usageID == 0 {
		t.Fatal("usage id not returned")
	}
	secret, _ := cat.GetChannelSecret(context.Background(), 1, channel.ID)
	if secret.Balance == nil || *secret.Balance != "3.750000" {
		t.Fatalf("channel balance = %v, want 3.750000", secret.Balance)
	}
	logs, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 20})
	if logs.Total != 1 || logs.List[0].RequestID != "req-1" || logs.List[0].Status != "success" {
		t.Fatalf("unexpected usage logs: %+v", logs)
	}
}

func TestSettleCostZeroWritesUsageWithoutChannelDebit(t *testing.T) {
	st := storefake.New()
	cat := newTestCatalog(st)
	channelBalance := "1.000000"
	channel, _ := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1, Balance: &channelBalance})

	if _, err := newSettlementService(st).Settle(context.Background(), settlement.Input{UserID: 1, ChannelID: &channel.ID, Cost: "0.000000", DebitChannel: true, UsageLog: successUsageInput("req-free", 1, channel.ID)}); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	secret, _ := cat.GetChannelSecret(context.Background(), 1, channel.ID)
	if secret.Balance == nil || *secret.Balance != "1.000000" {
		t.Fatalf("zero-cost settlement changed channel balance: %v", secret.Balance)
	}
	logs, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 20})
	if logs.Total != 1 {
		t.Fatalf("zero-cost settlement did not write usage log: %+v", logs)
	}
}

func TestSettleDuplicateUsageRollsBack(t *testing.T) {
	st := storefake.New()
	cat := newTestCatalog(st)
	channelBalance := "5.000000"
	channel, _ := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1, Balance: &channelBalance})
	if _, err := st.InsertUsageLog(context.Background(), successUsageInput("dup", 1, channel.ID)); err != nil {
		t.Fatal(err)
	}

	_, err := newSettlementService(st).Settle(context.Background(), settlement.Input{UserID: 1, ChannelID: &channel.ID, Cost: "1.000000", DebitChannel: true, UsageLog: successUsageInput("dup", 1, channel.ID)})
	if !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	secret, _ := cat.GetChannelSecret(context.Background(), 1, channel.ID)
	if secret.Balance == nil || *secret.Balance != "5.000000" {
		t.Fatalf("channel balance not rolled back: %v", secret.Balance)
	}
}

func successUsageInput(requestID string, userID, channelID int) domain.UsageLogInput {
	return domain.UsageLogInput{RequestID: requestID, UserID: &userID, ChannelID: &channelID, Model: "gpt", UpstreamModel: "up-gpt", InputTokens: 100, OutputTokens: 50, TotalTokens: 150, UnitPriceInputPer1M: "0.10000000", UnitPriceOutputPer1M: "0.20000000", TotalCost: "1.250000", Status: "success"}
}

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"LLMGateway/server/internal/proxy"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func TestPGSettleChannelRollsBackOnUsageInsertFailure(t *testing.T) {
	st := testStore(t)
	cat := testCatalog(t, st)
	owner := testOwner(t, st)
	channelBalance := "5.000000"
	channel, err := cat.CreateChannel(context.Background(), owner, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1, Balance: &channelBalance})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertUsageLog(context.Background(), successUsageInput("dup-pg", owner, channel.ID)); err != nil {
		t.Fatal(err)
	}

	service := proxy.NewService(st, cat, testQuota(t, st), testRateLimit(t, st), nil, func(int) int { return 0 }, time.Now)
	_, err = service.Settle(context.Background(), settlement.Input{UserID: owner, ChannelID: &channel.ID, Cost: "1.000000", DebitChannel: true, UsageLog: successUsageInput("dup-pg", owner, channel.ID)})
	if !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	secret, _ := cat.GetChannelSecret(context.Background(), owner, channel.ID)
	if secret.Balance == nil || *secret.Balance != "5.000000" {
		t.Fatalf("channel balance not rolled back: %v", secret.Balance)
	}
}

func TestPGSettlePersistsTTFT(t *testing.T) {
	st := testStore(t)
	cat := testCatalog(t, st)
	owner := testOwner(t, st)
	channel, err := cat.CreateChannel(context.Background(), owner, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	ttft := 42
	usage := successUsageInput("stream-ttft-pg", owner, channel.ID)
	usage.TTFTMs = &ttft
	service := proxy.NewService(st, cat, testQuota(t, st), testRateLimit(t, st), nil, func(int) int { return 0 }, time.Now)
	if _, err := service.Settle(context.Background(), settlement.Input{UserID: owner, ChannelID: &channel.ID, Cost: "0.000100", UsageLog: usage}); err != nil {
		t.Fatal(err)
	}
	logs, err := st.ListUsageLogs(context.Background(), owner, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || logs.List[0].TTFTMs == nil || *logs.List[0].TTFTMs != ttft {
		t.Fatalf("usage logs = %+v, want TTFT %d", logs, ttft)
	}
}

func successUsageInput(requestID string, userID, channelID int) domain.UsageLogInput {
	return domain.UsageLogInput{RequestID: requestID, UserID: &userID, ChannelID: &channelID, Model: "gpt", UpstreamModel: "up-gpt", InputTokens: 100, OutputTokens: 50, TotalTokens: 150, UnitPriceInputPer1M: "0.10000000", UnitPriceOutputPer1M: "0.20000000", TotalCost: "1.250000", Status: "success"}
}

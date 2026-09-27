package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/proxy"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func TestPGSettleChatCompletionRollsBackOnUsageInsertFailure(t *testing.T) {
	st := testStore(t)
	cat := testCatalog(t, st)
	acc := accounts.New(st, st.AccountsTx())
	if _, err := acc.CreateUser(context.Background(), domain.UserInput{Nickname: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.RechargeUser(context.Background(), 1, domain.RechargeInput{Amount: "10.000000"}); err != nil {
		t.Fatal(err)
	}
	channelBalance := "5.000000"
	channel, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1, Balance: &channelBalance})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertUsageLog(context.Background(), successUsageInput("dup-pg", 1, channel.ID)); err != nil {
		t.Fatal(err)
	}

	service := proxy.NewService(st, cat, testQuota(t, st), testRateLimit(t, st), nil, func(int) int { return 0 }, time.Now)
	_, err = service.Settle(context.Background(), settlement.Input{UserID: 1, ChannelID: &channel.ID, Cost: "1.000000", DebitChannel: true, Description: "chat", UsageLog: successUsageInput("dup-pg", 1, channel.ID)})
	if !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	balance, _ := st.GetUserBalance(context.Background(), 1)
	if balance.AvailableBalance != "10.000000" {
		t.Fatalf("user balance not rolled back: %s", balance.AvailableBalance)
	}
	secret, _ := cat.GetChannelSecret(context.Background(), 1, channel.ID)
	if secret.Balance == nil || *secret.Balance != "5.000000" {
		t.Fatalf("channel balance not rolled back: %v", secret.Balance)
	}
	txs, _ := st.ListBalanceTransactions(context.Background(), 1, 1, 20)
	if txs.Total != 1 {
		t.Fatalf("consume transaction was partially committed: %+v", txs)
	}
}

func TestPGSettleChatCompletionPersistsTTFT(t *testing.T) {
	st := testStore(t)
	cat := testCatalog(t, st)
	acc := accounts.New(st, st.AccountsTx())
	if _, err := acc.CreateUser(context.Background(), domain.UserInput{Nickname: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.RechargeUser(context.Background(), 1, domain.RechargeInput{Amount: "10.000000"}); err != nil {
		t.Fatal(err)
	}
	channel, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	ttft := 42
	usage := successUsageInput("stream-ttft-pg", 1, channel.ID)
	usage.TTFTMs = &ttft
	service := proxy.NewService(st, cat, testQuota(t, st), testRateLimit(t, st), nil, func(int) int { return 0 }, time.Now)
	if _, err := service.Settle(context.Background(), settlement.Input{UserID: 1, ChannelID: &channel.ID, Cost: "0.000100", Description: "stream chat", UsageLog: usage}); err != nil {
		t.Fatal(err)
	}
	logs, err := st.ListUsageLogs(context.Background(), domain.UsageLogFilter{Page: 1, PageSize: 10})
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

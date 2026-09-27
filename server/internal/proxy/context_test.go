package proxy

import (
	"context"
	"errors"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

// TestDetachedCtxIgnoresParentCancellation pins the core detached-task
// semantic: a canceled parent must not cancel the derived context, while
// parent values remain visible.
func TestDetachedCtxIgnoresParentCancellation(t *testing.T) {
	type ctxKey struct{}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "sentinel"))
	cancel()

	detached, stop := detachedCtx(parent, bestEffortTimeout)
	defer stop()

	if err := detached.Err(); err != nil {
		t.Fatalf("detached ctx err = %v, want nil despite canceled parent", err)
	}
	if detached.Value(ctxKey{}) != "sentinel" {
		t.Fatal("detached ctx dropped parent values")
	}
}

// TestDetachedCtxHonorsTimeout ensures detached work is still bounded.
func TestDetachedCtxHonorsTimeout(t *testing.T) {
	detached, stop := detachedCtx(context.Background(), time.Millisecond)
	defer stop()

	<-detached.Done()
	if !errors.Is(detached.Err(), context.DeadlineExceeded) {
		t.Fatalf("detached ctx err = %v, want DeadlineExceeded", detached.Err())
	}
}

// TestSettleCompletesDespiteCanceledParent verifies the settlement semantic: a
// charge for work the upstream already performed must land even after the
// downstream request context is canceled.
func TestSettleCompletesDespiteCanceledParent(t *testing.T) {
	st := storefake.New()
	cat := newTestCatalog(st)
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	usageID, err := newSettlementService(st).Settle(ctx, settlement.Input{
		UserID:      1,
		ChannelID:   &channel.ID,
		Cost:        "1.250000",
		Description: "chat completion req-canceled",
		UsageLog:    successUsageInput("req-canceled", 1, channel.ID),
	})
	if err != nil {
		t.Fatalf("Settle with canceled parent: %v", err)
	}
	if usageID == 0 {
		t.Fatal("usage id not returned")
	}
	balance, _ := st.GetUserBalance(context.Background(), 1)
	if balance.AvailableBalance != "8.750000" {
		t.Fatalf("user balance = %s, want 8.750000 after canceled-parent settlement", balance.AvailableBalance)
	}
	logs, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 20})
	if logs.Total != 1 || logs.List[0].RequestID != "req-canceled" {
		t.Fatalf("usage log missing after canceled-parent settlement: %+v", logs)
	}
}

// TestBestEffortWritesCompleteDespiteCanceledParent verifies the best-effort
// semantic: usage logging and channel health recording must still land when the
// downstream request context is canceled, because they must not change the
// response.
func TestBestEffortWritesCompleteDespiteCanceledParent(t *testing.T) {
	st := storefake.New()
	svc := newRouteTestApp(st, func(int) int { return 0 })
	cat := newTestCatalog(st)
	channel, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "c", BaseURL: "https://c.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	auth := &accounts.AuthContext{UserID: 1, KeyID: 1}
	svc.logUsage(ctx, "req-best-effort", auth, &channel.ID, "up-model", "model", nil, "0.000000", "", "", 5, "127.0.0.1", "error", "rate_limited")
	svc.recordChannelHealth(ctx, channel.ID, true, "")

	logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || logs.List[0].RequestID != "req-best-effort" {
		t.Fatalf("best-effort usage log missing after cancellation: %+v", logs)
	}
	health, err := cat.GetChannelHealth(context.Background(), channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if health.SuccessCount != 1 || health.State != domain.HealthClosed {
		t.Fatalf("best-effort channel health not recorded: %+v", health)
	}
}

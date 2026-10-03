package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func TestBufferedSettlementAfterUsageCancellation(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline", "pricing_failure", "settlement_failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var requestCtx context.Context
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requestCtx = r.Context()
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("usage")), Header: make(http.Header)}, nil
			})
			service, st, auth := newProtocolSeamService(t, transport, seamAdapter())
			if mode == "deadline" {
				service.ConfigureRequest(30*time.Millisecond, 1)
			}
			service.adapter.ParseUsage = func([]byte) *Usage {
				if mode == "deadline" {
					<-requestCtx.Done()
				} else {
					cancel()
				}
				service.catalog = checkedPricingCatalog{Catalog: service.catalog, t: t, fail: mode == "pricing_failure"}
				if mode == "settlement_failure" {
					service.settleTx = failingSettlementTx{}
				}
				return &Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500}
			}
			_, err := service.ChatCompletions(ctx, auth, ChatRequest{Model: "public-model", Body: []byte(`{}`)}, "")
			failed := strings.HasSuffix(mode, "failure")
			if (err != nil) != failed {
				t.Fatalf("err = %v", err)
			}
			logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			if logs.Total != 1 || logs.List[0].TotalTokens != 1500 {
				t.Fatalf("logs = %+v", logs)
			}
			if failed {
				code := "pricing_error"
				if mode == "settlement_failure" {
					code = "settlement_failed"
				}
				if logs.List[0].Status != "error" || logs.List[0].ErrorCode != code || logs.List[0].TotalCost != "0.000000" {
					t.Fatalf("log = %+v", logs.List[0])
				}
			} else if logs.List[0].Status != "success" || logs.List[0].TotalCost != "0.000450" {
				t.Fatalf("log = %+v", logs.List[0])
			}
		})
	}
}

type failingSettlementTx struct{}

func (failingSettlementTx) InTx(context.Context, func(settlement.Tx) error) error {
	return errors.New("settlement failed")
}

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
	balance := "10.000000"
	channel, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1, Balance: &balance})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	usageID, err := newSettlementService(st).Settle(ctx, settlement.Input{
		UserID:       1,
		ChannelID:    &channel.ID,
		Cost:         "1.250000",
		DebitChannel: true,
		UsageLog:     successUsageInput("req-canceled", 1, channel.ID),
	})
	if err != nil {
		t.Fatalf("Settle with canceled parent: %v", err)
	}
	if usageID == 0 {
		t.Fatal("usage id not returned")
	}
	secret, err := cat.GetChannelSecret(context.Background(), 1, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if secret.Balance == nil || *secret.Balance != "8.750000" {
		t.Fatalf("channel balance = %v, want 8.750000 after canceled-parent settlement", secret.Balance)
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

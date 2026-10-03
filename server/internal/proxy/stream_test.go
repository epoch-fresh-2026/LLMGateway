package proxy

import (
	"context"
	"errors"
	"time"

	"LLMGateway/server/internal/catalog"
	"io"
	"net/http"
	"strings"
	"testing"

	domain "LLMGateway/server/internal/testutil/testtypes"
)

func streamAdapter(parseStream func(io.Reader, string, func(StreamEvent) error) error) ProtocolAdapter {
	return ProtocolAdapter{
		RewriteRequest: func(body []byte, model string) ([]byte, error) { return body, nil },
		EstimateUsage: func([]byte, int) (EstimatedUsage, error) {
			return EstimatedUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}, nil
		},
		ParseStream: parseStream,
		StreamError: func(code, message string) []byte { return []byte("data: error\n\n") },
	}
}

func streamTransport() roundTripFunc {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("stream")), Header: make(http.Header)}, nil
	}
}

func TestCompletionStreamCanceledSettlement(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline", "missing_done", "protocol", "success_cancel", "success_deadline", "estimated_cancel", "estimated_deadline", "estimated_missing_done", "estimated_protocol"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var requestCtx context.Context
			adapter := streamAdapter(func(_ io.Reader, _ string, emit func(StreamEvent) error) error {
				event := StreamEvent{Frame: []byte("data: usage\n\n"), Data: true, Text: "hello"}
				if !strings.HasPrefix(mode, "estimated_") {
					event.Usage = &Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500}
				}
				if err := emit(event); err != nil {
					return err
				}
				if strings.Contains(mode, "cancel") {
					cancel()
				}
				if strings.Contains(mode, "deadline") {
					<-requestCtx.Done()
				}
				if strings.HasPrefix(mode, "success_") {
					return emit(StreamEvent{Done: true})
				}
				if strings.HasSuffix(mode, "protocol") {
					return ErrInvalidStream
				}
				return requestCtx.Err()
			})
			adapter.CountTextTokens = func(string, string) (int, error) { return 3, nil }
			if !strings.HasPrefix(mode, "estimated_") {
				adapter.EstimateUsage = func([]byte, int) (EstimatedUsage, error) {
					return EstimatedUsage{PromptTokens: 10, InputTokens: 2000, OutputTokens: 1000, TotalTokens: 3000}, nil
				}
			}
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requestCtx = r.Context()
				return streamTransport()(r)
			})
			service, st, auth := newProtocolSeamService(t, transport, adapter)
			q := newTestQuota(st, nil)
			name, scope, period, scopeID, limit := "daily", "user", "day", 1, int64(10000)
			if _, err := q.CreateQuotaPolicy(context.Background(), 1, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &scopeID, PeriodType: &period, TokenLimit: &limit}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(mode, "deadline") {
				service.ConfigureRequest(30*time.Millisecond, 1)
			}
			response, err := service.ChatCompletions(ctx, auth, ChatRequest{Model: "public-model", Stream: true, Body: []byte(`{}`)}, "")
			if err != nil {
				t.Fatal(err)
			}
			service.catalog = checkedPricingCatalog{Catalog: service.catalog, t: t}
			err = response.Stream.Forward(func([]byte) error { return nil })
			if strings.HasPrefix(mode, "success_") && err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(mode, "success_") && err == nil {
				t.Fatal("expected partial error")
			}
			if response.Stream.Forward(func([]byte) error { return nil }) == nil {
				t.Fatal("second Forward accepted")
			}
			response.Stream.Close()
			logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			tokens, usedTokens := 1500, 1500
			cost, balance := "0.000450", "9.999550"
			if strings.HasPrefix(mode, "estimated_") {
				tokens, usedTokens = 13, 0
				cost, balance = "0.000000", "10.000000"
			}
			secret, err := newTestCatalog(st).GetChannelSecret(context.Background(), 1, 1)
			if err != nil {
				t.Fatal(err)
			}
			if secret.Balance == nil || *secret.Balance != balance {
				t.Fatalf("channel balance = %v, want %s", secret.Balance, balance)
			}
			active, err := st.CountActiveRateLimitReservations(context.Background(), auth.UserID, &auth.KeyID, "public-model", nil)
			if err != nil || active != 0 {
				t.Fatalf("active rate reservations = %d, err = %v", active, err)
			}
			rateTokens, err := st.CountTokensSince(context.Background(), domain.TokenCountFilter{UserID: auth.UserID, Model: "public-model", Since: time.Now().Add(-time.Minute).Format(time.RFC3339)})
			if err != nil || rateTokens != int64(usedTokens) {
				t.Errorf("rate tokens = %d, want %d, err = %v", rateTokens, usedTokens, err)
			}
			if logs.Total != 1 || logs.List[0].TotalTokens != int64(tokens) || logs.List[0].TotalCost != cost {
				t.Fatalf("logs = %+v", logs)
			}
			quotaUsage, err := q.ListQuotaUsage(context.Background(), domain.QuotaPolicyFilter{OwnerUserID: 1, Page: 1, PageSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			if quotaUsage.Total != 1 || quotaUsage.List[0].UsedTokens != int64(usedTokens) || quotaUsage.List[0].ReservedTokens != 0 || quotaUsage.List[0].UsedCost != cost {
				t.Fatalf("quota usage = %+v", quotaUsage)
			}
			stats, err := st.StatsOverview(context.Background(), 1, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if stats.TotalTokens != quotaUsage.List[0].UsedTokens || stats.TotalCost != quotaUsage.List[0].UsedCost {
				t.Fatalf("stats = %+v, quota usage = %+v", stats, quotaUsage)
			}
			if !strings.HasPrefix(mode, "success_") {
				prefix := "partial_actual_"
				if strings.HasPrefix(mode, "estimated_") {
					prefix = "partial_estimated_"
				}
				code := "upstream_stream_interrupted"
				if strings.Contains(mode, "cancel") {
					code = "client_canceled"
				}
				if strings.Contains(mode, "deadline") {
					code = "upstream_timeout"
				}
				if strings.HasSuffix(mode, "protocol") {
					code = "upstream_stream_protocol_error"
				}
				if logs.List[0].Status != "error" || logs.List[0].ErrorCode != prefix+code {
					t.Fatalf("log = %+v", logs.List[0])
				}
			}
		})
	}
}

func TestCompletionStreamPartialPricingFailure(t *testing.T) {
	for _, actual := range []bool{false, true} {
		name := "estimated"
		if actual {
			name = "actual"
		}
		t.Run(name, func(t *testing.T) {
			adapter := streamAdapter(func(_ io.Reader, _ string, emit func(StreamEvent) error) error {
				event := StreamEvent{Frame: []byte("data: text\n\n"), Data: true, Text: "hello"}
				if actual {
					event.Usage = &Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13}
				}
				return emit(event)
			})
			adapter.CountTextTokens = func(string, string) (int, error) { return 3, nil }
			service, st, auth := newProtocolSeamService(t, streamTransport(), adapter)
			q := newTestQuota(st, nil)
			policyName, scope, period, scopeID, limit := "daily", "user", "day", 1, int64(10000)
			if _, err := q.CreateQuotaPolicy(context.Background(), 1, domain.QuotaPolicyInput{PolicyName: &policyName, ScopeType: &scope, ScopeID: &scopeID, PeriodType: &period, TokenLimit: &limit}); err != nil {
				t.Fatal(err)
			}
			response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Stream: true, Body: []byte(`{}`)}, "")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Stream.Close()
			service.catalog = checkedPricingCatalog{Catalog: service.catalog, t: t, fail: true}
			if err := response.Stream.Forward(func([]byte) error { return nil }); err == nil {
				t.Fatal("expected interruption")
			}
			logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			code := "partial_estimated_upstream_stream_interrupted"
			if actual {
				code = "partial_actual_pricing_error"
			}
			if logs.Total != 1 || logs.List[0].Status != "error" || logs.List[0].ErrorCode != code || logs.List[0].TotalTokens != 13 || logs.List[0].TotalCost != "0.000000" {
				t.Fatalf("logs = %+v", logs)
			}
			quotaUsage, err := q.ListQuotaUsage(context.Background(), domain.QuotaPolicyFilter{OwnerUserID: 1, Page: 1, PageSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			stats, err := st.StatsOverview(context.Background(), 1, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if quotaUsage.Total != 1 || quotaUsage.List[0].UsedTokens != 0 || quotaUsage.List[0].ReservedTokens != 0 || stats.TotalTokens != quotaUsage.List[0].UsedTokens || stats.TotalCost != quotaUsage.List[0].UsedCost {
				t.Fatalf("quota = %+v, stats = %+v", quotaUsage, stats)
			}
		})
	}
}

func TestCompletionStreamSettlesOnceWithTTFT(t *testing.T) {
	adapter := streamAdapter(func(reader io.Reader, publicModel string, emit func(StreamEvent) error) error {
		if err := emit(StreamEvent{Frame: []byte("data: {\"chunk\":1}\n\n"), Data: true, Text: "hi", Usage: &Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500}}); err != nil {
			return err
		}
		return emit(StreamEvent{Done: true, Frame: []byte("data: [DONE]\n\n")})
	})
	service, st, auth := newProtocolSeamService(t, streamTransport(), adapter)
	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Stream: true, Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil {
		t.Fatalf("ChatCompletions: %v", err)
	}
	if response.Stream == nil {
		t.Fatal("expected a stream response")
	}
	defer response.Stream.Close()
	var frames []byte
	if err := response.Stream.Forward(func(frame []byte) error { frames = append(frames, frame...); return nil }); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if !strings.Contains(string(frames), "[DONE]") {
		t.Fatalf("frames = %q, want terminal DONE", frames)
	}
	logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || logs.List[0].Status != "success" || logs.List[0].TTFTMs == nil {
		t.Fatalf("usage logs = %+v, want one success with TTFT", logs)
	}
}

type checkedPricingCatalog struct {
	Catalog
	t    *testing.T
	fail bool
}

func (c checkedPricingCatalog) GetPricing(ctx context.Context, id int, model string) (catalog.PricingDTO, error) {
	if ctx.Err() != nil {
		c.t.Fatalf("pricing context canceled: %v", ctx.Err())
	}
	if _, ok := ctx.Deadline(); !ok {
		c.t.Fatal("pricing context has no deadline")
	}
	if c.fail {
		return catalog.PricingDTO{}, errors.New("pricing failed")
	}
	return c.Catalog.GetPricing(ctx, id, model)
}

func TestStickyStreamBindsOnlyCompletedSettlement(t *testing.T) {
	for _, mode := range []string{"success", "cancel", "protocol", "pricing", "close", "write"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			adapter := streamAdapter(func(_ io.Reader, _ string, emit func(StreamEvent) error) error {
				if err := emit(StreamEvent{Data: true, Frame: []byte("text"), Usage: &Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}); err != nil {
					return err
				}
				if mode == "cancel" {
					cancel()
				}
				if mode == "protocol" {
					return ErrInvalidStream
				}
				return emit(StreamEvent{Done: true})
			})
			service, st, auth := newProtocolSeamService(t, streamTransport(), adapter)
			addSeamFallback(t, st, "fallback", "http://fallback.test")
			counted := &routingCountCatalog{Catalog: service.catalog}
			service.catalog = counted
			key := stickyKey{owner: auth.UserID, key: auth.KeyID, model: "public-model"}
			response, err := service.ChatCompletions(ctx, auth, ChatRequest{Model: key.model, Stream: true, Body: []byte(`{}`)}, "")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Stream.Close()
			if service.sticky.get(key, service.now()).channelID != 0 {
				t.Fatal("2xx headers bound before forwarding")
			}
			if mode == "close" {
				response.Stream.Close()
				return
			}
			if mode == "pricing" {
				service.catalog = checkedPricingCatalog{Catalog: counted, t: t, fail: true}
			}
			err = response.Stream.Forward(func([]byte) error {
				if service.sticky.get(key, service.now()).channelID != 0 {
					t.Fatal("bound before terminal frame")
				}
				if mode == "write" {
					return errors.New("client disconnected")
				}
				return nil
			})
			if mode == "success" {
				if err != nil || service.sticky.get(key, service.now()).channelID != 1 {
					t.Fatalf("success binding err=%v", err)
				}
				response.Stream.Close()
				if service.sticky.get(key, service.now()).channelID != 1 {
					t.Fatal("Close removed completed success")
				}
			} else if service.sticky.get(key, service.now()).channelID != 0 {
				t.Fatal("incomplete stream bound")
			}
			if counted.listCalls != 1 || counted.singleCalls != 0 {
				t.Fatal("stream failure triggered fallback")
			}
		})
	}
}

func TestCompletionStreamInterruptedAuditsForwardedTextWithoutCharge(t *testing.T) {
	adapter := streamAdapter(func(reader io.Reader, publicModel string, emit func(StreamEvent) error) error {
		if err := emit(StreamEvent{Frame: []byte("data: {\"chunk\":1}\n\n"), Data: true, Text: "hello world"}); err != nil {
			return err
		}
		return ErrInvalidStream
	})
	adapter.CountTextTokens = func(model, text string) (int, error) { return 3, nil }
	adapter.EstimateUsage = func([]byte, int) (EstimatedUsage, error) {
		return EstimatedUsage{PromptTokens: 4, InputTokens: 100, OutputTokens: 5, TotalTokens: 105}, nil
	}
	service, st, auth := newProtocolSeamService(t, streamTransport(), adapter)
	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Stream: true, Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil {
		t.Fatalf("ChatCompletions: %v", err)
	}
	defer response.Stream.Close()
	if err := response.Stream.Forward(func([]byte) error { return nil }); err == nil {
		t.Fatal("Forward should surface the stream interruption")
	}
	logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || logs.List[0].ErrorCode != "partial_estimated_upstream_stream_protocol_error" || logs.List[0].InputTokens != 4 || logs.List[0].TotalTokens != 7 || logs.List[0].TotalCost != "0.000000" {
		t.Fatalf("partial usage log = %+v", logs)
	}
}

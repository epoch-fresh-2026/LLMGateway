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
	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func newProtocolSeamService(t *testing.T, transport http.RoundTripper, adapter ProtocolAdapter) (*Service, *storefake.Store, *domain.AuthContext) {
	t.Helper()
	st := storefake.New()
	cat := newTestCatalog(st)
	acc := accounts.New(st, st.AccountsTx())
	if _, err := acc.CreateUser(context.Background(), domain.UserInput{Nickname: "seam-user"}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.RechargeUser(context.Background(), 1, domain.RechargeInput{Amount: "10.000000"}); err != nil {
		t.Fatal(err)
	}
	createdKey, err := acc.CreateKey(context.Background(), 1, domain.KeyInput{KeyName: "seam-key", Prefix: "sk-"})
	if err != nil {
		t.Fatal(err)
	}
	balance := "10.000000"
	channel, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{
		Name: "seam-upstream", BaseURL: "http://upstream.test", APIKey: "secret", AuthType: "bearer",
		Status: 1, Priority: 1, Weight: 1, Balance: &balance,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(context.Background(), 1, channel.ID, domain.ChannelModel{ModelName: "public-model", UpstreamModel: "configured-model", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: channel.ID, ModelName: "public-model", InputPricePer1M: "0.15000000", OutputPricePer1M: "0.60000000", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}

	service := NewService(st, cat, newTestQuota(st, nil), newTestRateLimit(st, nil), &http.Client{Transport: transport}, func(int) int { return 0 }, time.Now, adapter)
	auth := &domain.AuthContext{KeyID: createdKey.ID, UserID: 1, KeyActive: true, UserStatus: "active", AvailableBalance: "10.000000"}
	return service, st, auth
}

func TestChatCompletionsUsesProtocolNeutralAdapterAndSettles(t *testing.T) {
	var upstreamBody string
	adapter := ProtocolAdapter{
		RewriteRequest: func(body []byte, model string) ([]byte, error) {
			if string(body) != `{"model":"public-model"}` || model != "configured-model" {
				t.Fatalf("rewrite input = %s, model = %q", body, model)
			}
			return []byte(`{"model":"configured-model"}`), nil
		},
		ParseUsage: func(body []byte) *Usage {
			if string(body) != `{"upstream":true}` {
				t.Fatalf("parse body = %s", body)
			}
			return &Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500}
		},
		RewriteResponse: func(body []byte, model string) []byte {
			if string(body) != `{"upstream":true}` || model != "public-model" {
				t.Fatalf("response rewrite input = %s, model = %q", body, model)
			}
			return []byte(`{"model":"public-model"}`)
		},
		EstimateUsage: func([]byte, int) (EstimatedUsage, error) {
			return EstimatedUsage{InputTokens: 100, OutputTokens: 100, TotalTokens: 200}, nil
		},
	}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		upstreamBody = string(body)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"upstream":true}`)), Header: make(http.Header)}, nil
	})
	service, st, auth := newProtocolSeamService(t, transport, adapter)
	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil {
		t.Fatalf("ChatCompletions: %v", err)
	}
	if upstreamBody != `{"model":"configured-model"}` {
		t.Fatalf("upstream body = %s", upstreamBody)
	}
	if response.Status != http.StatusOK || string(response.Body) != `{"model":"public-model"}` {
		t.Fatalf("response = %+v", response)
	}
	if response.Usage == nil || response.Usage.TotalTokens != 1500 {
		t.Fatalf("response usage = %+v", response.Usage)
	}
	logs, err := st.ListUsageLogs(context.Background(), domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || len(logs.List) != 1 || logs.List[0].Status != "success" {
		t.Fatalf("usage logs = %+v", logs)
	}
}

func TestChatCompletionsPassesThroughUpstreamErrors(t *testing.T) {
	adapter := ProtocolAdapter{
		RewriteRequest: func(body []byte, model string) ([]byte, error) { return body, nil },
		ParseUsage:     func([]byte) *Usage { t.Fatal("ParseUsage called for upstream error"); return nil },
		RewriteResponse: func([]byte, string) []byte {
			t.Fatal("RewriteResponse called for upstream error")
			return nil
		},
		EstimateUsage: func([]byte, int) (EstimatedUsage, error) {
			return EstimatedUsage{InputTokens: 100, OutputTokens: 100, TotalTokens: 200}, nil
		},
	}
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader(`{"error":"busy"}`)), Header: make(http.Header)}, nil
	})
	service, _, auth := newProtocolSeamService(t, transport, adapter)
	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil {
		t.Fatalf("ChatCompletions: %v", err)
	}
	if response.Status != http.StatusTooManyRequests || string(response.Body) != `{"error":"busy"}` {
		t.Fatalf("response = %+v", response)
	}
}

func TestChatCompletionsFailsOverAndSettlesOnlyFinalSuccess(t *testing.T) {
	adapter := ProtocolAdapter{
		RewriteRequest: func(body []byte, model string) ([]byte, error) { return body, nil },
		ParseUsage: func(body []byte) *Usage {
			if string(body) != `{"upstream":true}` {
				t.Fatalf("unexpected success body: %s", body)
			}
			return &Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}
		},
		RewriteResponse: func(body []byte, model string) []byte { return body },
		EstimateUsage:   func([]byte, int) (EstimatedUsage, error) { return EstimatedUsage{TotalTokens: 2}, nil },
	}
	var calls []string
	service, st, auth := newProtocolSeamService(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.Host)
		if req.URL.Host == "first.test" {
			return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader(`busy`)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"upstream":true}`)), Header: make(http.Header)}, nil
	}), adapter)
	cat := newTestCatalog(st)
	secondBalance := "10.000000"
	second, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "second", BaseURL: "http://second.test", APIKey: "secret", AuthType: "bearer", Status: 1, Priority: 1, Weight: 1, Balance: &secondBalance})
	if err != nil {
		t.Fatal(err)
	}
	first, err := cat.GetChannelSecret(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	first.BaseURL = "http://first.test"
	if _, err := cat.UpdateChannel(context.Background(), 1, 1, domain.ChannelInput{Name: first.Name, BaseURL: first.BaseURL, APIKey: first.APIKey, AuthType: first.AuthType, Status: 1, Priority: 2, Weight: 1, Balance: first.Balance}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(context.Background(), 1, second.ID, domain.ChannelModel{ModelName: "public-model", UpstreamModel: "configured-model", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: second.ID, ModelName: "public-model", InputPricePer1M: "0.15000000", OutputPricePer1M: "0.60000000", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil || response.Status != http.StatusOK {
		t.Fatalf("response = %+v, err=%v", response, err)
	}
	if len(calls) != 2 || calls[0] != "first.test" || calls[1] != "second.test" {
		t.Fatalf("calls = %+v, want first then second", calls)
	}
	logs, _ := st.ListUsageLogs(context.Background(), domain.UsageLogFilter{Page: 1, PageSize: 10})
	if logs.Total != 1 || logs.List[0].Status != "success" || logs.List[0].ChannelID == nil || *logs.List[0].ChannelID != second.ID {
		t.Fatalf("usage logs = %+v, want one final success", logs)
	}
}

func TestChatCompletionsDoesNotFailOverCallerHTTPError(t *testing.T) {
	adapter := ProtocolAdapter{
		RewriteRequest: func(body []byte, model string) ([]byte, error) { return body, nil },
		ParseUsage:     func([]byte) *Usage { t.Fatal("ParseUsage called for caller error"); return nil },
		RewriteResponse: func(body []byte, model string) []byte {
			return body
		},
		EstimateUsage: func([]byte, int) (EstimatedUsage, error) { return EstimatedUsage{TotalTokens: 2}, nil },
	}
	calls := 0
	service, st, auth := newProtocolSeamService(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`bad request`)), Header: make(http.Header)}, nil
	}), adapter)
	cat := newTestCatalog(st)
	second, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "second", BaseURL: "http://second.test", APIKey: "secret", AuthType: "bearer", Status: 1, Priority: 1, Weight: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(context.Background(), 1, second.ID, domain.ChannelModel{ModelName: "public-model", UpstreamModel: "configured-model", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil || response.Status != http.StatusBadRequest || calls != 1 {
		t.Fatalf("response/calls = %+v/%d, want one 400 attempt", response, calls)
	}
}

func TestChatCompletionsPreservesFinalCallerErrorAfterEarlierRetryableFailure(t *testing.T) {
	adapter := ProtocolAdapter{
		RewriteRequest:  func(body []byte, model string) ([]byte, error) { return body, nil },
		ParseUsage:      func([]byte) *Usage { return nil },
		RewriteResponse: func(body []byte, model string) []byte { return body },
		EstimateUsage:   func([]byte, int) (EstimatedUsage, error) { return EstimatedUsage{TotalTokens: 2}, nil },
	}
	calls := 0
	service, st, auth := newProtocolSeamService(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		status, body := http.StatusBadGateway, `retry`
		if calls == 2 {
			status, body = http.StatusBadRequest, `caller error`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}), adapter)
	cat := newTestCatalog(st)
	second, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "second", BaseURL: "http://second.test", APIKey: "secret", AuthType: "bearer", Status: 1, Priority: 1, Weight: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(context.Background(), 1, second.ID, domain.ChannelModel{ModelName: "public-model", UpstreamModel: "configured-model", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil || response.Status != http.StatusBadRequest || string(response.Body) != "caller error" || calls != 2 {
		t.Fatalf("response/calls = %+v/%d, err=%v", response, calls, err)
	}
}

func TestChatCompletionsStopsAttemptsWhenRequestContextCanceled(t *testing.T) {
	adapter := ProtocolAdapter{
		RewriteRequest: func(body []byte, model string) ([]byte, error) { return body, nil },
		EstimateUsage:  func([]byte, int) (EstimatedUsage, error) { return EstimatedUsage{TotalTokens: 2}, nil },
	}
	started := make(chan struct{})
	service, _, auth := newProtocolSeamService(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}), adapter)
	service.ConfigureRequest(time.Second, 3)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := service.ChatCompletions(ctx, auth, ChatRequest{Model: "public-model", Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
		result <- err
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ChatCompletions did not stop after cancellation")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

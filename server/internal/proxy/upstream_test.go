package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func strPtr(value string) *string { return &value }
func intPtr(value int) *int       { return &value }
func int64Ptr(value int64) *int64 { return &value }

func seamAdapter() ProtocolAdapter {
	return ProtocolAdapter{
		RewriteRequest:  func(body []byte, model string) ([]byte, error) { return body, nil },
		ParseUsage:      func(body []byte) *Usage { return &Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15} },
		RewriteResponse: func(body []byte, model string) []byte { return body },
		EstimateUsage: func([]byte, int) (EstimatedUsage, error) {
			return EstimatedUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}, nil
		},
	}
}

// addSeamFallback registers a lower-priority fallback channel so the seam
// channel (priority 1) stays the deterministic first candidate.
func addSeamFallback(t *testing.T, st *storefake.Store, name, baseURL string) int {
	t.Helper()
	cat := newTestCatalog(st)
	channel, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: name, BaseURL: baseURL, APIKey: "secret", AuthType: "bearer", Status: 1, Priority: 0, Weight: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(context.Background(), 1, channel.ID, domain.ChannelModel{ModelName: "public-model", UpstreamModel: "configured-model", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: channel.ID, UpstreamModel: "configured-model", InputPricePer1M: "0.15000000", OutputPricePer1M: "0.60000000", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	return channel.ID
}

func TestChatCompletionsChannelRateLimitFailsOverToNextCandidate(t *testing.T) {
	var calls int
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"upstream":true}`)), Header: make(http.Header)}, nil
	})
	service, st, auth := newProtocolSeamService(t, transport, seamAdapter())
	fallback := addSeamFallback(t, st, "fallback", "http://fallback.test")

	// The preferred channel is already at its channel rpm limit, so the loop
	// must record the failure and fall over to the next candidate.
	one := 1
	if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "prior-channel", UserID: &one, APIKeyID: &one, ChannelID: &one, Model: "public-model", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestRateLimit(st, nil).CreateRateLimit(context.Background(), 1, domain.RateLimitInput{RuleName: strPtr("channel rpm"), TargetType: strPtr("channel"), TargetValue: strPtr("1"), Metric: strPtr("rpm"), LimitValue: int64Ptr(1), Action: strPtr("reject")}); err != nil {
		t.Fatal(err)
	}

	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil || response.Status != http.StatusOK {
		t.Fatalf("response = %+v, err = %v", response, err)
	}
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1 (only the fallback candidate)", calls)
	}
	logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 2 || logs.List[0].ChannelID == nil || *logs.List[0].ChannelID != fallback {
		t.Fatalf("usage logs = %+v, want the newest success on fallback channel %d", logs, fallback)
	}
	health, err := service.catalog.GetChannelHealth(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if health.ConsecutiveFailures != 1 {
		t.Fatalf("channel 1 failures = %d, want 1", health.ConsecutiveFailures)
	}
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errReadCloser) Close() error             { return nil }

func TestChatCompletionsReadErrorFailsOverToNextCandidate(t *testing.T) {
	var calls int
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusOK, Body: errReadCloser{}, Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"upstream":true}`)), Header: make(http.Header)}, nil
	})
	service, st, auth := newProtocolSeamService(t, transport, seamAdapter())
	fallback := addSeamFallback(t, st, "fallback", "http://fallback.test")

	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil || response.Status != http.StatusOK {
		t.Fatalf("response = %+v, err = %v", response, err)
	}
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2 (read error then retry)", calls)
	}
	logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || logs.List[0].ChannelID == nil || *logs.List[0].ChannelID != fallback {
		t.Fatalf("usage logs = %+v, want one success on fallback channel %d", logs, fallback)
	}
}

func TestChatCompletionsMaxAttemptsTruncatesCandidates(t *testing.T) {
	var calls int
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("boom")), Header: make(http.Header)}, nil
	})
	service, st, auth := newProtocolSeamService(t, transport, seamAdapter())
	addSeamFallback(t, st, "fallback", "http://fallback.test")
	addSeamFallback(t, st, "third", "http://third.test")
	service.ConfigureRequest(time.Minute, 2)

	if _, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1"); !errors.Is(err, ErrUpstream) {
		t.Fatalf("err = %v, want ErrUpstream", err)
	}
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2 (maxAttempts truncation)", calls)
	}
}

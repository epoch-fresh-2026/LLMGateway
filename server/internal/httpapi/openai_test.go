package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	openaiwire "LLMGateway/server/internal/proxy/openai"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

const upstreamKey = "up-secret-key"

func upstreamSuccess() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		model, _ := payload["model"].(string)
		if r.Header.Get("Authorization") != "Bearer "+upstreamKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl-1",
			"object":  "chat.completion",
			"model":   model,
			"choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": "hi"}}},
			"usage":   map[string]any{"prompt_tokens": 1000, "completion_tokens": 500, "total_tokens": 1500},
		})
	})
}

type proxyFixture struct {
	server    *Server
	store     Port
	accounts  *accounts.Server
	catalog   *catalog.Server
	quota     *quota.Server
	ratelimit *ratelimit.Server
	fullKey   string
	upstream  *httptest.Server
}

func newProxyFixture(t *testing.T, upstream http.Handler, opts ...Option) *proxyFixture {
	t.Helper()
	return newProxyFixtureWithStore(t, upstream, storefake.New(), opts...)
}

func newProxyFixtureWithStore(t *testing.T, upstream http.Handler, st Port, opts ...Option) *proxyFixture {
	t.Helper()

	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)

	handler := NewServer(st, append([]Option{WithCipher(testCipher())}, opts...)...)
	acc := handler.accounts
	seeder, ok := st.(interface {
		SeedUser(username, passwordHash string) int
	})
	if !ok {
		t.Fatal("test store does not support SeedUser")
	}
	seeder.SeedUser("alice", "hash")
	created, err := acc.CreateKey(context.Background(), 1, domain.KeyInput{KeyName: "default", Prefix: "sk-"})
	if err != nil {
		t.Fatal(err)
	}

	balance := "10.000000"
	channel, err := handler.catalog.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "upstream", BaseURL: server.URL, APIKey: upstreamKey, Status: 1, Priority: 10, Weight: 100, Balance: &balance})
	if err != nil {
		t.Fatal(err)
	}
	channelID := channel.ID
	if _, err := handler.catalog.CreateChannelModel(context.Background(), 1, channelID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up-gpt", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.catalog.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: channelID, UpstreamModel: "up-gpt", InputPricePer1M: "0.15000000", OutputPricePer1M: "0.60000000", CachedInputPricePer1M: "0.07500000", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}

	return &proxyFixture{
		server:    handler,
		store:     st,
		accounts:  acc,
		catalog:   handler.catalog,
		quota:     handler.quota,
		ratelimit: handler.ratelimit,
		fullKey:   created.FullKey,
		upstream:  server,
	}
}

func proxyDo(t *testing.T, f *proxyFixture, method, path, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res := httptest.NewRecorder()
	f.server.OpenAI(res, req)
	return res
}

func TestOpenAIModels(t *testing.T) {
	f := newProxyFixture(t, upstreamSuccess())

	ok := proxyDo(t, f, http.MethodGet, "/v1/models", f.fullKey, "")
	if ok.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", ok.Code, ok.Body.String())
	}
	var list openaiwire.OpenAIModelList
	if err := json.Unmarshal(ok.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Object != "list" || len(list.Data) != 1 || list.Data[0].ID != "gpt" {
		t.Fatalf("unexpected model list: %+v", list)
	}

	unauthorized := proxyDo(t, f, http.MethodGet, "/v1/models", "", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("no auth status = %d, want 401", unauthorized.Code)
	}
	invalid := proxyDo(t, f, http.MethodGet, "/v1/models", "sk-invalid", "")
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("invalid key status = %d, want 401", invalid.Code)
	}
}

func TestChatCompletionsSuccess(t *testing.T) {
	f := newProxyFixture(t, upstreamSuccess())

	body := `{"model":"gpt","messages":[{"role":"user","content":"hi"}]}`
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, body)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"model":"gpt"`) {
		t.Fatalf("response model not rewritten: %s", res.Body.String())
	}
	if strings.Contains(res.Body.String(), "up-gpt") {
		t.Fatalf("response leaked upstream model: %s", res.Body.String())
	}
	if strings.Contains(res.Body.String(), upstreamKey) {
		t.Fatalf("response leaked upstream key: %s", res.Body.String())
	}

	channelBalance, err := f.catalog.GetChannelSecret(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if channelBalance.Balance == nil || *channelBalance.Balance != "9.999550" {
		t.Fatalf("channel balance = %v, want 9.999550", channelBalance.Balance)
	}

	logs, err := f.store.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 {
		t.Fatalf("usage logs total = %d, want 1", logs.Total)
	}
	entry := logs.List[0]
	if entry.Status != "success" || entry.TotalTokens != 1500 || entry.TotalCost != "0.000450" {
		t.Fatalf("unexpected usage log: %+v", entry)
	}
	if entry.ChannelID == nil || *entry.ChannelID != 1 {
		t.Fatalf("usage log channel mismatch: %+v", entry)
	}
	if entry.UpstreamModel != "up-gpt" || entry.Model != "gpt" {
		t.Fatalf("usage log model mismatch: %+v", entry)
	}
	if entry.UnitPriceInputPer1M != "0.15000000" || entry.UnitPriceOutputPer1M != "0.60000000" {
		t.Fatalf("usage log unit prices missing: %+v", entry)
	}

	keys, err := f.store.ListKeys(context.Background(), 1, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if keys.List[0].LastUsedAt == nil {
		t.Fatal("last_used_at not updated")
	}
}

func TestChatCompletionsCachedTokensBilling(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "chatcmpl-cached", "object": "chat.completion", "model": "up-gpt", "choices": []any{},
			"usage": map[string]any{
				"prompt_tokens": 1000, "completion_tokens": 500, "total_tokens": 1500,
				"prompt_tokens_details": map[string]any{"cached_tokens": 200},
			},
		})
	}))

	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","messages":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}

	// (1000-200)*0.15/1e6 + 200*0.075/1e6 + 500*0.6/1e6 = 0.000435
	secret, _ := f.catalog.GetChannelSecret(context.Background(), 1, 1)
	if secret.Balance == nil || *secret.Balance != "9.999565" {
		t.Fatalf("channel balance = %v, want 9.999565", secret.Balance)
	}
	logs, _ := f.store.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 20})
	entry := logs.List[0]
	if entry.TotalCost != "0.000435" {
		t.Fatalf("cost = %v, want 0.000435 (cached tokens must not be double charged)", entry.TotalCost)
	}
	if entry.CachedInputTokens != 200 {
		t.Fatalf("cached_input_tokens = %v, want 200", entry.CachedInputTokens)
	}
}

func TestChatCompletionsAuthFailures(t *testing.T) {
	f := newProxyFixture(t, upstreamSuccess())
	body := `{"model":"gpt","messages":[]}`

	if res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", "", body); res.Code != http.StatusUnauthorized {
		t.Fatalf("no auth status = %d, want 401", res.Code)
	}
	if res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", "sk-bad", body); res.Code != http.StatusUnauthorized {
		t.Fatalf("invalid key status = %d, want 401", res.Code)
	}

	// Disabled key.
	created, err := f.accounts.CreateKey(context.Background(), 1, domain.KeyInput{KeyName: "second"})
	if err != nil {
		t.Fatal(err)
	}
	secondKey := created.FullKey
	if _, err := f.accounts.UpdateKey(context.Background(), 1, created.ID, domain.KeyUpdateInput{IsActive: boolPointer(false)}); err != nil {
		t.Fatal(err)
	}
	if res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", secondKey, body); res.Code != http.StatusUnauthorized {
		t.Fatalf("disabled key status = %d, want 401", res.Code)
	}

	// Expired key.
	expired, err := f.accounts.CreateKey(context.Background(), 1, domain.KeyInput{KeyName: "expired", ExpiresAt: "2020-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", expired.FullKey, body); res.Code != http.StatusUnauthorized {
		t.Fatalf("expired key status = %d, want 401", res.Code)
	}
}

func TestChatCompletionsStreamingSuccess(t *testing.T) {
	st := &countingSettlementStore{Store: storefake.New()}
	f := newProxyFixtureWithStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		options, _ := payload["stream_options"].(map[string]any)
		if options["include_usage"] != true {
			t.Fatalf("stream_options = %#v, want include_usage=true", options)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream\",\"model\":\"up-gpt\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream\",\"model\":\"up-gpt\",\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":500,\"total_tokens\":1500}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}), st)
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","stream":true,"messages":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if strings.Contains(res.Body.String(), "up-gpt") || !strings.Contains(res.Body.String(), `"model":"gpt"`) {
		t.Fatalf("stream model was not rewritten: %s", res.Body.String())
	}
	if !strings.HasSuffix(res.Body.String(), "data: [DONE]\n\n") {
		t.Fatalf("stream missing terminal DONE: %s", res.Body.String())
	}
	secret, _ := f.catalog.GetChannelSecret(context.Background(), 1, 1)
	if secret.Balance == nil || *secret.Balance != "9.999550" {
		t.Fatalf("channel balance = %v, want 9.999550", secret.Balance)
	}
	logs, _ := f.store.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if logs.Total != 1 || logs.List[0].Status != "success" || logs.List[0].TTFTMs == nil {
		t.Fatalf("usage logs = %+v, want one success with TTFT", logs)
	}
	if calls := st.settlementCalls.Load(); calls != 1 {
		t.Fatalf("settlement calls = %d, want 1", calls)
	}
}

func TestChatCompletionsStreamingWithoutUsageDoesNotCharge(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream\",\"model\":\"up-gpt\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","stream":true,"messages":[]}`)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "upstream_usage_missing") {
		t.Fatalf("status/body = %d %s, want SSE usage error", res.Code, res.Body.String())
	}
	secret, _ := f.catalog.GetChannelSecret(context.Background(), 1, 1)
	if secret.Balance == nil || *secret.Balance != "10.000000" {
		t.Fatalf("channel balance changed without usage: %v", secret.Balance)
	}
	logs, _ := f.store.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if logs.Total != 1 || logs.List[0].Status != "error" || logs.List[0].ErrorCode != "upstream_usage_missing" {
		t.Fatalf("usage logs = %+v", logs)
	}
}

func TestChatCompletionsStreamingWithoutDoneFailsAndTripsBreaker(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream\",\"model\":\"up-gpt\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n")
	}))
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","stream":true,"messages":[]}`)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "upstream_stream_interrupted") {
		t.Fatalf("status/body = %d %s, want interrupted SSE error", res.Code, res.Body.String())
	}
	health, err := f.catalog.GetChannelHealth(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if health.ConsecutiveFailures != 1 {
		t.Fatalf("consecutive failures = %d, want 1", health.ConsecutiveFailures)
	}
	secret, _ := f.catalog.GetChannelSecret(context.Background(), 1, 1)
	if secret.Balance == nil || *secret.Balance != "10.000000" {
		t.Fatalf("channel balance changed after interrupted stream: %v", secret.Balance)
	}
	logs, _ := f.store.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if logs.Total != 1 || logs.List[0].TTFTMs == nil {
		t.Fatalf("interrupted stream must retain observed TTFT: %+v", logs)
	}
}

func TestChatCompletionsInterruptedStreamChargesForwardedTextEstimate(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream\",\"model\":\"up-gpt\",\"choices\":[{\"delta\":{\"content\":\"hello world\"}}]}\n\n")
	}))
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","stream":true,"messages":[]}`)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "upstream_stream_interrupted") {
		t.Fatalf("status/body = %d %s, want interrupted SSE error", res.Code, res.Body.String())
	}
	logs, err := f.store.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || logs.List[0].ErrorCode != "partial_estimated_upstream_stream_interrupted" || logs.List[0].TotalTokens <= 0 || logs.List[0].TotalCost == "0.000000" {
		t.Fatalf("partial usage log = %+v", logs)
	}
	secret, err := f.catalog.GetChannelSecret(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if secret.Balance == nil || *secret.Balance == "10.000000" {
		t.Fatal("channel balance was not charged for forwarded text")
	}
}

func TestChatCompletionsStreamingMalformedDataTripsBreaker(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {not-json}\n\n")
	}))
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","stream":true,"messages":[]}`)
	if !strings.Contains(res.Body.String(), "upstream_stream_protocol_error") {
		t.Fatalf("body = %s, want protocol error", res.Body.String())
	}
	health, _ := f.catalog.GetChannelHealth(context.Background(), 1)
	if health.ConsecutiveFailures != 1 {
		t.Fatalf("consecutive failures = %d, want 1", health.ConsecutiveFailures)
	}
}

func TestChatCompletionsStreamingUpstreamErrorPassesThrough(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOpenAIError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "busy")
	}))
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","stream":true,"messages":[]}`)
	if res.Code != http.StatusTooManyRequests || res.Header().Get("Content-Type") == "text/event-stream" {
		t.Fatalf("status/content type = %d %q", res.Code, res.Header().Get("Content-Type"))
	}
	if !strings.Contains(res.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("upstream error body not preserved: %s", res.Body.String())
	}
}

func TestChatCompletionsStreamingCancellationReachesUpstream(t *testing.T) {
	upstreamCanceled := make(chan struct{})
	firstFrame := make(chan struct{})
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream\",\"model\":\"up-gpt\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		w.(http.Flusher).Flush()
		close(firstFrame)
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	name, scope, period, scopeID, limit := "stream daily", "user", "day", 1, int64(10000)
	if _, err := f.quota.CreateQuotaPolicy(context.Background(), 1, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &scopeID, PeriodType: &period, TokenLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewServer(http.HandlerFunc(f.server.OpenAI))
	t.Cleanup(gateway.Close)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(`{"model":"gpt","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.fullKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatalf("read first frame: %v", err)
	}
	<-firstFrame
	cancel()
	_, _ = io.Copy(io.Discard, resp.Body)

	select {
	case <-upstreamCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request was not canceled")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		usage, err := f.quota.ListQuotaUsage(context.Background(), domain.QuotaPolicyFilter{OwnerUserID: 1, Page: 1, PageSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		if usage.Total == 1 && usage.List[0].ReservedTokens == 0 && usage.List[0].UsedTokens > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("forwarded stream usage was not settled after cancellation: %+v", usage)
		}
		time.Sleep(10 * time.Millisecond)
	}
	logs, err := f.store.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || logs.List[0].ErrorCode != "partial_estimated_client_canceled" || logs.List[0].TotalTokens <= 0 || logs.List[0].TTFTMs == nil {
		t.Fatalf("canceled stream must retain observed TTFT: %+v", logs)
	}
}

func TestChatCompletionsTokenRateLimitRejectsConservatively(t *testing.T) {
	f := newProxyFixture(t, upstreamSuccess())
	metric, target, action := "tpm", "user", "reject"
	limit, priority := int64(1), 1
	if _, err := f.ratelimit.CreateRateLimit(context.Background(), 1, domain.RateLimitInput{RuleName: stringPointer("token limit"), TargetType: &target, TargetValue: stringPointer("1"), Metric: &metric, LimitValue: &limit, Action: &action, Priority: &priority}); err != nil {
		t.Fatal(err)
	}
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","messages":[]}`)
	if res.Code != http.StatusTooManyRequests || !strings.Contains(res.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("status/body = %d %s", res.Code, res.Body.String())
	}
}

func TestChatCompletionsNoChannel(t *testing.T) {
	f := newProxyFixture(t, upstreamSuccess())
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"unknown-model","messages":[]}`)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.Code)
	}
}

func TestChatCompletionsUpstreamFailureDoesNotCharge(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"message": "boom"}})
	}))

	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","messages":[]}`)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want upstream 500 passthrough", res.Code)
	}

	secret, _ := f.catalog.GetChannelSecret(context.Background(), 1, 1)
	if secret.Balance == nil || *secret.Balance != "10.000000" {
		t.Fatalf("channel balance changed on upstream failure: %v", secret.Balance)
	}
	logs, _ := f.store.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 20})
	if logs.Total != 1 {
		t.Fatalf("usage logs total = %d, want 1 failure log", logs.Total)
	}
	entry := logs.List[0]
	if entry.Status != "error" || entry.TotalCost != "0.000000" {
		t.Fatalf("unexpected failure log: %+v", entry)
	}
}

func TestChatCompletionsRateLimited(t *testing.T) {
	f := newProxyFixture(t, upstreamSuccess())
	rule, err := f.ratelimit.CreateRateLimit(context.Background(), 1, domain.RateLimitInput{
		RuleName: stringPointer("user rpm"), TargetType: stringPointer("user"), TargetValue: stringPointer("1"), Metric: stringPointer("rpm"),
		LimitValue: int64Pointer(1), Action: stringPointer("reject"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rule.ID == 0 {
		t.Fatal("rule not created")
	}

	body := `{"model":"gpt","messages":[]}`
	first := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, body)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200; body=%s", first.Code, first.Body.String())
	}
	second := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, body)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429; body=%s", second.Code, second.Body.String())
	}
}

func TestChatCompletionsQuotaExceededBeforeUpstream(t *testing.T) {
	var calls atomic.Int32
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		upstreamSuccess().ServeHTTP(w, r)
	}))
	name, scope, period, scopeID := "key daily", "api_key", "day", 1
	limit := int64(10)
	if _, err := f.quota.CreateQuotaPolicy(context.Background(), 1, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &scopeID, PeriodType: &period, TokenLimit: &limit}); err != nil {
		t.Fatal(err)
	}

	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","max_tokens":20,"messages":[{"role":"user","content":"hello"}]}`)
	if res.Code != http.StatusTooManyRequests || !strings.Contains(res.Body.String(), "insufficient_quota") {
		t.Fatalf("status/body = %d %s, want 429 insufficient_quota", res.Code, res.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls.Load())
	}
}

func TestChatCompletionsQuotaSettlementUsesActualUsage(t *testing.T) {
	f := newProxyFixture(t, upstreamSuccess())
	name, scope, period, scopeID := "user daily", "user", "day", 1
	limit := int64(10000)
	if _, err := f.quota.CreateQuotaPolicy(context.Background(), 1, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &scopeID, PeriodType: &period, TokenLimit: &limit}); err != nil {
		t.Fatal(err)
	}

	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","max_tokens":2000,"messages":[{"role":"user","content":"hello"}]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", res.Code, res.Body.String())
	}
	usage, err := f.quota.ListQuotaUsage(context.Background(), domain.QuotaPolicyFilter{OwnerUserID: 1, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if usage.Total != 1 || usage.List[0].ReservedTokens != 0 || usage.List[0].UsedTokens != 1500 || usage.List[0].UsedCost != "0.000450" {
		t.Fatalf("quota usage = %+v", usage)
	}
}

func TestChatCompletionsUpstreamFailureReleasesQuota(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "boom"})
	}))
	name, scope, period, scopeID := "user daily", "user", "day", 1
	limit := int64(10000)
	if _, err := f.quota.CreateQuotaPolicy(context.Background(), 1, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &scopeID, PeriodType: &period, TokenLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","max_tokens":100,"messages":[]}`)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", res.Code)
	}
	usage, _ := f.quota.ListQuotaUsage(context.Background(), domain.QuotaPolicyFilter{OwnerUserID: 1, Page: 1, PageSize: 10})
	if usage.Total != 1 || usage.List[0].ReservedTokens != 0 || usage.List[0].UsedTokens != 0 {
		t.Fatalf("quota usage = %+v", usage)
	}
}

func TestChatCompletionsModelRateLimitCountsOnlySameModel(t *testing.T) {
	current := time.Now().UTC()
	st := storefake.NewWithClock(func() time.Time { return current })
	f := newProxyFixtureWithStore(t, upstreamSuccess(), st, WithClock(func() time.Time { return current }))
	if _, err := f.catalog.CreateChannelModel(context.Background(), 1, 1, domain.ChannelModel{ModelName: "gpt-other", UpstreamModel: "up-other", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.catalog.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: 1, UpstreamModel: "up-other", InputPricePer1M: "0.15000000", OutputPricePer1M: "0.60000000", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "prior-other", UserID: intPointer(1), APIKeyID: intPointer(1), ChannelID: intPointer(1), Model: "gpt-other", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ratelimit.CreateRateLimit(context.Background(), 1, domain.RateLimitInput{RuleName: stringPointer("gpt rpm"), TargetType: stringPointer("model"), TargetValue: stringPointer("gpt"), Metric: stringPointer("rpm"), LimitValue: int64Pointer(1), Action: stringPointer("reject")}); err != nil {
		t.Fatal(err)
	}

	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","messages":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
}

func TestChatCompletionsChannelRateLimitAfterRouting(t *testing.T) {
	current := time.Now().UTC()
	st := storefake.NewWithClock(func() time.Time { return current })
	var calls int32
	f := newProxyFixtureWithStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		upstreamSuccess().ServeHTTP(w, r)
	}), st, WithClock(func() time.Time { return current }))
	if _, err := f.store.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "prior-channel", UserID: intPointer(1), APIKeyID: intPointer(1), ChannelID: intPointer(1), Model: "gpt", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ratelimit.CreateRateLimit(context.Background(), 1, domain.RateLimitInput{RuleName: stringPointer("channel rpm"), TargetType: stringPointer("channel"), TargetValue: stringPointer("1"), Metric: stringPointer("rpm"), LimitValue: int64Pointer(1), Action: stringPointer("reject")}); err != nil {
		t.Fatal(err)
	}

	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","messages":[]}`)
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", res.Code, res.Body.String())
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("upstream calls = %d, want 0", got)
	}
	logs, _ := f.store.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Status: "error", Page: 1, PageSize: 20})
	if logs.Total != 1 || logs.List[0].ErrorCode != "rate_limited" || logs.List[0].ChannelID == nil || *logs.List[0].ChannelID != 1 {
		t.Fatalf("rate-limited usage log missing channel/error_code: %+v", logs)
	}
}

func TestChatCompletionsTripsBreakerAndSkipsChannel(t *testing.T) {
	var calls int32
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"message": "boom"}})
	})
	f := newProxyFixture(t, upstream)
	body := `{"model":"gpt","messages":[]}`

	for i := 0; i < 5; i++ {
		res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, body)
		if res.Code != http.StatusInternalServerError {
			t.Fatalf("attempt %d status = %d, want 500", i, res.Code)
		}
	}

	health, err := f.catalog.GetChannelHealth(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if health.State != domain.HealthOpen {
		t.Fatalf("state = %s, want open after 5 failures", health.State)
	}

	// The tripped channel is skipped: degraded 503 without another upstream call.
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, body)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "no_healthy_channel") {
		t.Fatalf("degraded body = %s, want no_healthy_channel", res.Body.String())
	}
	if got := atomic.LoadInt32(&calls); got != 5 {
		t.Fatalf("upstream calls = %d, want 5 (tripped channel must be skipped)", got)
	}

	// The degraded request is recorded as a failure usage log.
	logs, _ := f.store.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 50})
	found := false
	for _, item := range logs.List {
		if item.ErrorCode == "no_healthy_channel" {
			found = true
		}
	}
	if !found {
		t.Fatal("degraded request did not write a no_healthy_channel usage log")
	}
}

func TestChatCompletionsHalfOpenRecovers(t *testing.T) {
	var failing atomic.Bool
	failing.Store(true)
	var calls int32
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if failing.Load() {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "boom"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion", "model": "up-gpt", "choices": []any{},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	})

	current := time.Now().UTC()
	st := storefake.NewWithClock(func() time.Time { return current })
	f := newProxyFixtureWithStore(t, upstream, st)
	body := `{"model":"gpt","messages":[]}`

	for i := 0; i < 5; i++ {
		proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, body)
	}
	health, _ := f.catalog.GetChannelHealth(context.Background(), 1)
	if health.State != domain.HealthOpen {
		t.Fatalf("state = %s, want open", health.State)
	}

	// After the cooldown a half-open probe succeeds and closes the breaker.
	current = current.Add(31 * time.Second)
	failing.Store(false)
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, body)
	if res.Code != http.StatusOK {
		t.Fatalf("probe status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	health, _ = f.catalog.GetChannelHealth(context.Background(), 1)
	if health.State != domain.HealthClosed {
		t.Fatalf("state = %s, want closed after successful probe", health.State)
	}
}

func TestChatCompletionsHealthRecordFailureDoesNotBreakSuccess(t *testing.T) {
	f := newProxyFixtureWithStore(t, upstreamSuccess(), failingHealthStore{Store: storefake.New()})
	res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, `{"model":"gpt","messages":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when health recording fails; body=%s", res.Code, res.Body.String())
	}
}

// failingHealthStore makes catalog transactions fail so tests can prove health
// recording is best-effort and cannot turn a successful request into an error.
type failingHealthStore struct {
	*storefake.Store
}

func (f failingHealthStore) CatalogTx() catalog.TxManager {
	return failingCatalogTx{}
}

type failingCatalogTx struct{}

func (failingCatalogTx) InTx(context.Context, func(catalog.Tx) error) error {
	return errors.New("health store unavailable")
}

type countingSettlementStore struct {
	*storefake.Store
	settlementCalls atomic.Int32
}

func (s *countingSettlementStore) SettlementTx() settlement.TxManager {
	return countingSettlementTx{inner: s.Store.SettlementTx(), calls: &s.settlementCalls}
}

type countingSettlementTx struct {
	inner settlement.TxManager
	calls *atomic.Int32
}

func (c countingSettlementTx) InTx(ctx context.Context, fn func(settlement.Tx) error) error {
	c.calls.Add(1)
	return c.inner.InTx(ctx, fn)
}

func (f failingHealthStore) RecordChannelSuccess(int) (domain.ChannelHealth, error) {
	return domain.ChannelHealth{}, errors.New("health store unavailable")
}

func (f failingHealthStore) RecordChannelFailure(int, domain.FailureReason) (domain.ChannelHealth, error) {
	return domain.ChannelHealth{}, errors.New("health store unavailable")
}

func TestChatCompletionsClientErrorDoesNotTripBreaker(t *testing.T) {
	var calls int32
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
	})
	f := newProxyFixture(t, upstream)
	body := `{"model":"gpt","messages":[]}`

	for i := 0; i < 6; i++ {
		res := proxyDo(t, f, http.MethodPost, "/v1/chat/completions", f.fullKey, body)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d status = %d, want 400 passthrough", i, res.Code)
		}
	}

	health, _ := f.catalog.GetChannelHealth(context.Background(), 1)
	if health.State != domain.HealthClosed {
		t.Fatalf("state = %s, want closed (client errors must not trip the breaker)", health.State)
	}
	if got := atomic.LoadInt32(&calls); got != 6 {
		t.Fatalf("upstream calls = %d, want 6 (channel must stay routable)", got)
	}
}

func stringPointer(v string) *string { return &v }
func intPointer(v int) *int          { return &v }
func int64Pointer(v int64) *int64    { return &v }
func boolPointer(v bool) *bool       { return &v }

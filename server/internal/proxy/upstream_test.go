package proxy

import (
	"context"
	"errors"
	"fmt"

	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

type countingRateLimit struct {
	RateLimit
	owners       []int
	reservations []ratelimit.RateLimitReservationInput
	reserveErr   error
}

func (r *countingRateLimit) ListRateLimits(ctx context.Context, owner int, enabled *bool, page, pageSize int) (ratelimit.ListResponse[ratelimit.RateLimitRuleDTO], error) {
	r.owners = append(r.owners, owner)
	return r.RateLimit.ListRateLimits(ctx, owner, enabled, page, pageSize)
}

func (r *countingRateLimit) ReserveRateLimit(ctx context.Context, in ratelimit.RateLimitReservationInput) (ratelimit.RateLimitReservation, error) {
	r.reservations = append(r.reservations, in)
	if r.reserveErr != nil {
		return ratelimit.RateLimitReservation{}, r.reserveErr
	}
	return r.RateLimit.ReserveRateLimit(ctx, in)
}

type countingQuota struct {
	Quota
	reservations []quota.QuotaReserveInput
}

func (q *countingQuota) ReserveQuota(ctx context.Context, in quota.QuotaReserveInput) (quota.QuotaReservation, error) {
	q.reservations = append(q.reservations, in)
	return q.Quota.ReserveQuota(ctx, in)
}

func TestChatCompletionsReusesRequestEstimatesAndRateLimitSnapshot(t *testing.T) {
	for _, mode := range []string{"success", "upstream_fallback", "channel_fallback", "reject", "override", "reservation_reject"} {
		t.Run(mode, func(t *testing.T) {
			estimateCalls, upstreamCalls := 0, 0
			adapter := seamAdapter()
			estimate := EstimatedUsage{PromptTokens: 3, InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
			adapter.EstimateUsage = func([]byte, int) (EstimatedUsage, error) {
				estimateCalls++
				return estimate, nil
			}
			service, st, auth := newProtocolSeamService(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				upstreamCalls++
				status := http.StatusOK
				if mode == "upstream_fallback" && upstreamCalls == 1 {
					status = http.StatusBadGateway
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
			}), adapter)
			rates := &countingRateLimit{RateLimit: service.ratelimit}
			quotas := &countingQuota{Quota: service.quota}
			service.ratelimit, service.quota = rates, quotas
			if mode == "upstream_fallback" || mode == "channel_fallback" {
				addSeamFallback(t, st, "fallback", "http://fallback.test")
			}
			if mode == "reject" || mode == "override" || mode == "channel_fallback" {
				target, metric, limit := "api_key", "tpm", int64(15)
				if mode == "channel_fallback" {
					target = "channel"
				}
				if _, err := newTestRateLimit(st, nil).CreateRateLimit(context.Background(), auth.UserID, domain.RateLimitInput{RuleName: strPtr("boundary"), TargetType: &target, TargetValue: strPtr("1"), Metric: &metric, LimitValue: &limit, Action: strPtr("reject")}); err != nil {
					t.Fatal(err)
				}
				if mode == "override" {
					auth.RateLimitOverrides = []byte(`{"tpm":100}`)
				}
			}
			if mode == "reservation_reject" {
				rates.reserveErr = ratelimit.ErrInvalid
			}
			_, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{}`)}, "")
			rejected := mode == "reject" || mode == "reservation_reject"
			if rejected && !errors.Is(err, ErrRateLimited) || !rejected && err != nil {
				t.Fatalf("err = %v", err)
			}
			if estimateCalls != 1 || len(rates.owners) != 1 || rates.owners[0] != auth.UserID {
				t.Fatalf("estimate calls = %d, rate limit owners = %v", estimateCalls, rates.owners)
			}
			if rejected {
				if upstreamCalls != 0 || len(quotas.reservations) != 0 {
					t.Fatalf("rejected request reached upstream/quota: %d/%d", upstreamCalls, len(quotas.reservations))
				}
			} else {
				wantCalls := 1
				if mode == "upstream_fallback" {
					wantCalls = 2
				}
				if upstreamCalls != wantCalls || len(rates.reservations) != 1 || rates.reservations[0].EstimatedTokens != 15 || len(quotas.reservations) != 1 {
					t.Fatalf("calls = %d, rate reservations = %+v, quota reservations = %+v", upstreamCalls, rates.reservations, quotas.reservations)
				}
				reserved := quotas.reservations[0]
				if reserved.EstimatedTokens != 15 || reserved.EstimatedCost != "0.000004" {
					t.Fatalf("conservative quota reservation = %+v", reserved)
				}
			}
			active, err := rates.CountActiveRateLimitReservations(context.Background(), auth.UserID, &auth.KeyID, "public-model", nil)
			if err != nil || active != 0 {
				t.Fatalf("active reservations = %d, err = %v", active, err)
			}
		})
	}
}

func TestChatCompletionsPreservesQuotaAdmissionWithInFlightReservation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	service, st, auth := newProtocolSeamService(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	}), seamAdapter())
	if _, err := newTestQuota(st, nil).CreateQuotaPolicy(context.Background(), auth.UserID, quota.QuotaPolicyInput{PolicyName: strPtr("one reservation"), ScopeType: strPtr("user"), ScopeID: &auth.UserID, PeriodType: strPtr("day"), TokenLimit: int64Ptr(15)}); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{}`)}, "")
		result <- err
	}()
	select {
	case <-started:
	case err := <-result:
		t.Fatalf("first request failed before upstream: %v", err)
	case <-time.After(time.Second):
		close(release)
		t.Fatal("first request did not reach upstream")
	}
	_, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{}`)}, "")
	close(release)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("second request err = %v, want ErrQuotaExceeded", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("first request err = %v", err)
	}
}

func TestRateLimitSnapshotIsOwnerScopedAndRequestScoped(t *testing.T) {
	service, st, auth := newProtocolSeamService(t, nil, seamAdapter())
	rates := &countingRateLimit{RateLimit: service.ratelimit}
	service.ratelimit = rates
	snapshot := &rateLimitSnapshot{}
	tokens := int64(15)
	if err := service.checkRateLimitWithSnapshot(context.Background(), auth, "public-model", &tokens, snapshot); err != nil {
		t.Fatal(err)
	}
	other := *auth
	other.UserID++
	if err := service.checkChannelRateLimitWithSnapshot(context.Background(), &other, "public-model", 1, tokens, snapshot); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-owner snapshot err = %v", err)
	}
	if _, err := newTestRateLimit(st, nil).CreateRateLimit(context.Background(), auth.UserID, domain.RateLimitInput{RuleName: strPtr("new rule"), TargetType: strPtr("api_key"), TargetValue: strPtr("1"), Metric: strPtr("tpm"), LimitValue: int64Ptr(15), Action: strPtr("reject")}); err != nil {
		t.Fatal(err)
	}
	if err := service.checkRateLimit(context.Background(), auth, "public-model", &tokens); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("new request did not load new rule: %v", err)
	}
	if len(rates.owners) != 2 {
		t.Fatalf("rule loads = %v, want two request-scoped loads", rates.owners)
	}
}

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

type contextReadCloser struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (r contextReadCloser) Read([]byte) (int, error) {
	if r.cancel != nil {
		r.cancel()
	}
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}
func (r contextReadCloser) Close() error { return nil }

func TestUpstreamContextErrorsLogged(t *testing.T) {
	for _, phase := range []string{"do", "read", "next_attempt"} {
		for _, deadline := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/deadline=%v", phase, deadline), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
					stop := cancel
					if deadline {
						stop = nil
					}
					body := contextReadCloser{ctx: r.Context(), cancel: stop}
					if phase == "read" {
						return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, nil
					}
					_, err := body.Read(nil)
					return nil, err
				})
				service, st, auth := newProtocolSeamService(t, transport, seamAdapter())
				if deadline {
					service.ConfigureRequest(30*time.Millisecond, 1)
				}
				var err error
				if phase == "next_attempt" {
					attemptCtx := ctx
					if deadline {
						var stop context.CancelFunc
						attemptCtx, stop = context.WithDeadline(ctx, time.Now().Add(-time.Second))
						defer stop()
					} else {
						cancel()
					}
					_, err = service.attemptUpstreams(attemptCtx, upstreamAttemptInput{requestID: "next-attempt", auth: auth, req: ChatRequest{Model: "public-model"}, candidates: []catalog.RouteCandidate{{ChannelID: 1, UpstreamModel: "configured-model"}}, start: time.Now()})
				} else {
					_, err = service.ChatCompletions(ctx, auth, ChatRequest{Model: "public-model", Body: []byte(`{}`)}, "")
				}
				wantErr, code := context.Canceled, "client_canceled"
				if deadline {
					wantErr, code = context.DeadlineExceeded, "upstream_timeout"
				}
				if !errors.Is(err, wantErr) {
					t.Fatalf("err = %v, want %v", err, wantErr)
				}
				logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
				if err != nil {
					t.Fatal(err)
				}
				if logs.Total != 1 || logs.List[0].Status != "error" || logs.List[0].ErrorCode != code {
					t.Fatalf("logs = %+v", logs)
				}
				health, err := service.catalog.GetChannelHealth(context.Background(), 1)
				if err != nil {
					t.Fatal(err)
				}
				failures := 0
				if deadline && phase != "next_attempt" {
					failures = 1
				}
				if health.ConsecutiveFailures != failures {
					t.Fatalf("health = %+v", health)
				}
			})
		}
	}
}

type stickyFailSettlement struct{}

func (stickyFailSettlement) InTx(context.Context, func(settlement.Tx) error) error {
	return errors.New("settlement failed")
}

func TestStickySettlementFailureDoesNotBind(t *testing.T) {
	for _, stream := range []bool{false, true} {
		adapter := seamAdapter()
		if stream {
			adapter = streamAdapter(func(_ io.Reader, _ string, emit func(StreamEvent) error) error {
				if err := emit(StreamEvent{Usage: &Usage{TotalTokens: 15}}); err != nil {
					return err
				}
				return emit(StreamEvent{Done: true})
			})
		}
		service, st, auth := newProtocolSeamService(t, streamTransport(), adapter)
		service.settleTx = stickyFailSettlement{}
		key := stickyKey{owner: auth.UserID, key: auth.KeyID, model: "public-model"}
		service.sticky.put(key, 1, service.now())
		response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: key.model, Stream: stream, Body: []byte(`{}`)}, "")
		if stream {
			if err != nil {
				t.Fatal(err)
			}
			err = response.Stream.Forward(func([]byte) error { return nil })
			response.Stream.Close()
		}
		if err == nil || service.sticky.get(key, service.now()).channelID != 0 {
			t.Fatalf("failed settlement bound: stream=%v err=%v", stream, err)
		}
		active, err := st.CountActiveRateLimitReservations(context.Background(), auth.UserID, &auth.KeyID, key.model, nil)
		if err != nil || active != 0 {
			t.Fatalf("reservation leaked: %d %v", active, err)
		}
	}
}

func TestStickyCacheLifecycle(t *testing.T) {
	var cache stickyCache
	now := time.Now()
	key := stickyKey{owner: 1, key: 1, model: "m"}
	cache.put(key, 1, now)
	old := cache.get(key, now)
	cache.put(key, 1, now)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.invalidate(key, old)
			cache.get(key, now)
		}()
	}
	wg.Wait()
	if cache.get(key, now).channelID != 1 {
		t.Fatal("late invalidation removed newer success")
	}
	if cache.get(key, now.Add(stickyTTL)).channelID != 0 {
		t.Fatal("expired binding survived")
	}
	for i := 0; i <= stickyCapacity; i++ {
		cache.put(stickyKey{key: i}, i+1, now)
	}
	if len(cache.entries) != stickyCapacity || cache.get(stickyKey{key: 0}, now).channelID != 0 {
		t.Fatal("capacity not enforced")
	}
	cache.get(stickyKey{key: 1}, now)
	cache.put(stickyKey{key: stickyCapacity + 1}, 1, now)
	if cache.get(stickyKey{key: 1}, now).channelID == 0 || cache.get(stickyKey{key: 2}, now).channelID != 0 {
		t.Fatal("LRU eviction mismatch")
	}
}

func TestStickyFastPathAndRetry(t *testing.T) {
	for _, mode := range []string{"success", "retry", "limit", "caller", "cancel", "read", "transport", "channel"} {
		t.Run(mode, func(t *testing.T) {
			calls := []string{}
			phase := false
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service, st, auth := newProtocolSeamService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.Host)
				status := 200
				if phase {
					switch mode {
					case "retry", "limit":
						status = 502
					case "caller":
						status = 400
					case "cancel":
						cancel()
						return nil, context.Canceled
					case "read":
						if len(calls) == 1 {
							return &http.Response{StatusCode: 200, Body: errReadCloser{}, Header: make(http.Header)}, nil
						}
					case "transport":
						if len(calls) == 1 {
							return nil, errors.New("connection failed")
						}
					}
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
			}), seamAdapter())
			fallback := addSeamFallback(t, st, "fallback", "http://fallback.test")
			third := addSeamFallback(t, st, "third", "http://third.test")
			fourth := addSeamFallback(t, st, "fourth", "http://fourth.test")
			counted := &routingCountCatalog{Catalog: service.catalog, duplicate: true}
			service.catalog = counted
			rates := &countingRateLimit{RateLimit: service.ratelimit}
			quotas := &countingQuota{Quota: service.quota}
			service.ratelimit, service.quota = rates, quotas
			request := ChatRequest{Model: "public-model", Body: []byte(`{}`)}
			if _, err := service.ChatCompletions(ctx, auth, request, ""); err != nil {
				t.Fatal(err)
			}
			key := stickyKey{owner: auth.UserID, key: auth.KeyID, model: request.Model}
			if service.sticky.get(key, service.now()).channelID != 1 {
				t.Fatal("successful settlement not bound")
			}
			phase, calls = true, nil
			if mode == "limit" {
				service.ConfigureRequest(time.Minute, 1)
			}
			if mode == "retry" {
				counted.halfOpen = true
			}
			if mode == "channel" {
				_, err := newTestRateLimit(st, nil).CreateRateLimit(ctx, 1, domain.RateLimitInput{RuleName: strPtr("channel"), TargetType: strPtr("channel"), TargetValue: strPtr("1"), Metric: strPtr("rpm"), LimitValue: int64Ptr(1), Action: strPtr("reject")})
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := service.ChatCompletions(ctx, auth, request, "")
			wantList, wantCalls := 1, 1
			if mode == "retry" {
				wantList, wantCalls = 2, 3
			}
			if mode == "read" || mode == "transport" {
				wantList, wantCalls = 2, 2
			}
			if mode == "channel" {
				wantList = 2
			}
			if counted.listCalls != wantList || counted.singleCalls != 1 || len(calls) != wantCalls {
				t.Fatalf("lists=%d singles=%d calls=%v err=%v", counted.listCalls, counted.singleCalls, calls, err)
			}
			seen := map[string]bool{}
			for _, host := range calls {
				if seen[host] {
					t.Fatal("duplicate attempt")
				}
				seen[host] = true
			}
			if mode == "retry" && len(counted.released) != 3 {
				t.Fatalf("fallback probes leaked: %v", counted.released)
			}
			binding := service.sticky.get(key, service.now())
			if mode == "success" && binding.channelID != 1 {
				t.Fatal("fast success not bound")
			}
			if (mode == "read" || mode == "transport" || mode == "channel") && binding.channelID != fallback && binding.channelID != third && binding.channelID != fourth {
				t.Fatal("fallback success not preferred")
			}
			if (mode == "cancel" || mode == "caller" || mode == "limit" || mode == "retry") && binding.channelID != 0 {
				t.Fatal("failed request bound")
			}
			if len(rates.reservations) != 2 || len(quotas.reservations) != 2 {
				t.Fatal("admission repeated during fallback")
			}
			if binding.channelID != 0 {
				phase, calls = false, nil
				lists, singles := counted.listCalls, counted.singleCalls
				if _, err := service.ChatCompletions(ctx, auth, request, ""); err != nil {
					t.Fatal(err)
				}
				wantHost := "upstream.test"
				if binding.channelID == fallback {
					wantHost = "fallback.test"
				}
				if binding.channelID == third {
					wantHost = "third.test"
				}
				if binding.channelID == fourth {
					wantHost = "fourth.test"
				}
				if counted.listCalls != lists || counted.singleCalls != singles+1 || len(calls) != 1 || calls[0] != wantHost {
					t.Fatalf("recent success not reused: %v lists=%d singles=%d", calls, counted.listCalls, counted.singleCalls)
				}
			}
		})
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

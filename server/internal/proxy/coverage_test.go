package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	apperrors "LLMGateway/server/internal/errors"
	"LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/usage"
)

var coverageFailure = errors.New("persistence unavailable")

type coveragePort struct {
	Port
	auth     *accounts.AuthContext
	err      error
	requests int
	tokens   int64
}

func (p coveragePort) AuthenticateKey(context.Context, string) (*accounts.AuthContext, error) {
	return p.auth, p.err
}
func (p coveragePort) CountRequestsSince(context.Context, usage.UsageCountFilter) (int, error) {
	return p.requests, p.err
}
func (p coveragePort) CountTokensSince(context.Context, usage.TokenCountFilter) (int64, error) {
	return p.tokens, p.err
}

type coverageCatalog struct {
	Catalog
	err       error
	routes    []catalog.RouteCandidate
	models    []catalog.CatalogModelDTO
	pricing   *catalog.PricingDTO
	singleErr error
	secretErr error
	baseURL   string
}

func (c coverageCatalog) RouteCandidates(ctx context.Context, u int, m string) (catalog.ListResponse[catalog.RouteCandidate], error) {
	if c.routes == nil && (c.err == nil || c.pricing != nil) {
		return c.Catalog.RouteCandidates(ctx, u, m)
	}
	return catalog.ListResponse[catalog.RouteCandidate]{List: c.routes}, c.err
}
func (c coverageCatalog) ListCatalogModels(context.Context, int, bool) (catalog.ListResponse[catalog.CatalogModelDTO], error) {
	return catalog.ListResponse[catalog.CatalogModelDTO]{List: c.models}, c.err
}
func (c coverageCatalog) RouteCandidate(ctx context.Context, u int, m string, id int) (catalog.RouteCandidate, bool, error) {
	if c.singleErr != nil {
		return catalog.RouteCandidate{}, false, c.singleErr
	}
	return c.Catalog.RouteCandidate(ctx, u, m, id)
}
func (c coverageCatalog) GetPricing(ctx context.Context, id int, m string) (catalog.PricingDTO, error) {
	if c.pricing != nil {
		return *c.pricing, c.err
	}
	if c.err != nil {
		return catalog.PricingDTO{}, c.err
	}
	return c.Catalog.GetPricing(ctx, id, m)
}
func (c coverageCatalog) GetChannelSecret(ctx context.Context, u, id int) (*catalog.Channel, error) {
	if c.secretErr != nil {
		return nil, c.secretErr
	}
	v, err := c.Catalog.GetChannelSecret(ctx, u, id)
	if c.baseURL != "" && v != nil {
		v.BaseURL = c.baseURL
	}
	return v, err
}

type coverageRate struct {
	RateLimit
	rules                         []ratelimit.RateLimitRuleDTO
	listErr, reserveErr, countErr error
	active                        int64
}

func (r coverageRate) ListRateLimits(context.Context, int, *bool, int, int) (ratelimit.ListResponse[ratelimit.RateLimitRuleDTO], error) {
	return ratelimit.ListResponse[ratelimit.RateLimitRuleDTO]{List: r.rules}, r.listErr
}
func (r coverageRate) ReserveRateLimit(ctx context.Context, in ratelimit.RateLimitReservationInput) (ratelimit.RateLimitReservation, error) {
	if r.reserveErr != nil {
		return ratelimit.RateLimitReservation{}, r.reserveErr
	}
	return r.RateLimit.ReserveRateLimit(ctx, in)
}
func (r coverageRate) CountActiveRateLimitReservations(context.Context, int, *int, string, *int) (int64, error) {
	return r.active, r.countErr
}

type coverageQuota struct {
	Quota
	err error
}

func (q coverageQuota) ReserveQuota(context.Context, quota.QuotaReserveInput) (quota.QuotaReservation, error) {
	return quota.QuotaReservation{}, q.err
}

func coverageAdapter() ProtocolAdapter {
	return ProtocolAdapter{
		RewriteRequest:  func(b []byte, _ string) ([]byte, error) { return b, nil },
		RewriteResponse: func(b []byte, _ string) []byte { return b },
		ParseUsage:      func([]byte) *Usage { return &Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3} },
		EstimateUsage: func([]byte, int) (EstimatedUsage, error) {
			return EstimatedUsage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}, nil
		},
	}
}

func TestAuthenticationAndModelPermissions(t *testing.T) {
	service, _, auth := newProtocolSeamService(t, streamTransport(), coverageAdapter())
	service.store = coveragePort{Port: service.store, err: coverageFailure}
	if _, err := service.Authenticate(context.Background(), "Bearer key"); !errors.Is(err, coverageFailure) {
		t.Fatalf("store error=%v", err)
	}
	expiry := "invalid"
	copyAuth := *auth
	copyAuth.ExpiresAt = &expiry
	service.store = coveragePort{Port: service.store, auth: &copyAuth}
	if _, err := service.Authenticate(context.Background(), "Bearer key"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expiry error=%v", err)
	}
	for _, header := range []string{"", "Basic key", "Bearer", "Bearer "} {
		if _, err := service.Authenticate(context.Background(), header); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("header %q error=%v", header, err)
		}
	}
	for _, tc := range []struct {
		json string
		want bool
	}{{"", true}, {"{", true}, {`{"models":[]}`, true}, {`{"models":["other"]}`, false}, {`{"models":["*"]}`, true}} {
		auth.Permissions = []byte(tc.json)
		if got := allowModel(auth, "public-model"); got != tc.want {
			t.Fatalf("permissions %s=%v", tc.json, got)
		}
	}
	auth.Permissions = []byte(`{"models":["public-model"]}`)
	service.catalog = coverageCatalog{Catalog: service.catalog, models: []catalog.CatalogModelDTO{{ModelName: ""}, {ModelName: "other"}, {ModelName: "public-model"}, {ModelName: "public-model"}}}
	models, err := service.Models(context.Background(), auth)
	if err != nil || len(models.Models) != 1 || models.Models[0].ID != "public-model" {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	service.catalog = coverageCatalog{err: coverageFailure}
	if _, err := service.Models(context.Background(), auth); !errors.Is(err, coverageFailure) {
		t.Fatalf("models error=%v", err)
	}
	if cachedTokenCount(nil) != 0 {
		t.Fatal("nil usage has cached tokens")
	}
	service.ConfigureRequest(700*time.Second, 5)
	service.ConfigureQuota(10, time.Second)
	if service.reservationTTL != 760*time.Second || service.maxAttempts != 5 || service.defaultMaxTokens != 10 {
		t.Fatalf("configuration=%+v", service)
	}
}

func TestRateLimitErrorAndRuleBoundaries(t *testing.T) {
	for _, scope := range []string{"user", "api_key", "model", "channel"} {
		for _, metric := range []string{"rpm", "tpm", "concurrency", "unknown"} {
			for _, fail := range []bool{false, true} {
				t.Run(scope+"/"+metric+"/"+map[bool]string{false: "count", true: "error"}[fail], func(t *testing.T) {
					s, _, auth := newProtocolSeamService(t, streamTransport(), coverageAdapter())
					rule := ratelimit.RateLimitRuleDTO{TargetType: scope, TargetValue: "*", Action: "reject", Metric: metric, LimitValue: 3}
					s.ratelimit = coverageRate{RateLimit: s.ratelimit, rules: []ratelimit.RateLimitRuleDTO{rule}, active: 5}
					p := coveragePort{Port: s.store, requests: 5, tokens: 5}
					if fail {
						p.err = coverageFailure
						s.ratelimit = coverageRate{RateLimit: s.ratelimit, rules: []ratelimit.RateLimitRuleDTO{rule}, countErr: coverageFailure}
					}
					s.store = p
					tokens := int64(2)
					var err error
					if scope == "channel" {
						err = s.checkChannelRateLimit(context.Background(), auth, "public-model", 1, tokens)
					} else {
						err = s.checkRateLimit(context.Background(), auth, "public-model", &tokens)
					}
					want := ErrRateLimited
					if fail {
						want = coverageFailure
					}
					if metric == "unknown" {
						want = nil
					}
					if !errors.Is(err, want) {
						t.Fatalf("error=%v want %v", err, want)
					}
				})
			}
		}
	}
	s, _, auth := newProtocolSeamService(t, streamTransport(), coverageAdapter())
	tokens := int64(1)
	auth.RateLimitOverrides = []byte(`{"rpm":1}`)
	s.store = coveragePort{Port: s.store, requests: 1}
	if err := s.checkRateLimit(context.Background(), auth, "public-model", &tokens); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("override=%v", err)
	}
	s.store = coveragePort{Port: s.store, err: coverageFailure}
	if err := s.checkRateLimit(context.Background(), auth, "public-model", &tokens); !errors.Is(err, coverageFailure) {
		t.Fatalf("override store=%v", err)
	}
	auth.RateLimitOverrides = nil
	s.ratelimit = coverageRate{listErr: coverageFailure}
	if err := s.checkRateLimit(context.Background(), auth, "public-model", &tokens); !errors.Is(err, coverageFailure) {
		t.Fatalf("list error=%v", err)
	}
	if err := s.checkChannelRateLimit(context.Background(), auth, "public-model", 1, tokens); !errors.Is(err, coverageFailure) {
		t.Fatalf("channel list error=%v", err)
	}
	if err := s.loadRateLimitSnapshot(context.Background(), 2, &rateLimitSnapshot{loaded: true, ownerUserID: 1}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("snapshot=%v", err)
	}
	s.ratelimit = coverageRate{rules: []ratelimit.RateLimitRuleDTO{{Action: "reject", TargetType: "api_key", TargetValue: "*", Metric: "tpm", LimitValue: 3}}}
	if err := s.checkRateLimit(context.Background(), auth, "public-model", nil); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("missing estimate=%v", err)
	}
	s.ratelimit = coverageRate{rules: []ratelimit.RateLimitRuleDTO{{Action: "log"}, {Action: "reject", TargetType: "user", TargetValue: "999", LimitValue: 3}, {Action: "reject", TargetType: "user", TargetValue: "*", LimitValue: 0}, {Action: "reject", TargetType: "channel", TargetValue: "*", LimitValue: 0}}}
	if err := s.checkRateLimit(context.Background(), auth, "public-model", &tokens); err != nil {
		t.Fatal(err)
	}
	if err := s.checkChannelRateLimit(context.Background(), auth, "public-model", 1, tokens); err != nil {
		t.Fatal(err)
	}
}

type coverageTx struct {
	stage    string
	balance  string
	updated  string
	inserted bool
}

func (t *coverageTx) LockChannel(int, int) error {
	if t.stage == "lock" {
		return coverageFailure
	}
	return nil
}
func (t *coverageTx) GetChannelBalanceText(int, int) (string, error) {
	if t.stage == "balance" {
		return "", coverageFailure
	}
	return t.balance, nil
}
func (t *coverageTx) UpdateChannelBalance(_ int, _ int, b string) (bool, error) {
	t.updated = b
	if t.stage == "update" {
		return false, coverageFailure
	}
	return t.stage != "missing", nil
}
func (t *coverageTx) SettleQuotaReservation(int64, string, int, int, int64, string) error {
	if t.stage == "quota" {
		return coverageFailure
	}
	return nil
}
func (t *coverageTx) InsertUsageLog(usage.UsageLogInput) (int, error) {
	t.inserted = true
	return 1, nil
}

type coverageTxManager struct {
	tx  *coverageTx
	err error
}

func (m coverageTxManager) InTx(ctx context.Context, fn func(settlement.Tx) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if m.err != nil {
		return m.err
	}
	return fn(m.tx)
}

func TestSettlementRejectsInvalidAndFailedWrites(t *testing.T) {
	for _, stage := range []string{"cost", "negative", "quota", "channel", "lock", "balance", "parse", "update", "missing", "empty"} {
		t.Run(stage, func(t *testing.T) {
			s, _, _ := newProtocolSeamService(t, streamTransport(), coverageAdapter())
			tx := &coverageTx{stage: stage, balance: "10.000000"}
			s.settleTx = coverageTxManager{tx: tx}
			id := 1
			in := settlement.Input{UserID: 1, ChannelID: &id, Cost: "1.000000", DebitChannel: true}
			switch stage {
			case "cost":
				in.Cost = "invalid"
			case "negative":
				in.Cost = "-1"
			case "channel":
				in.ChannelID = nil
			case "parse":
				tx.balance = "invalid"
			case "empty":
				tx.balance = ""
			}
			got, err := s.Settle(context.Background(), in)
			if stage == "empty" {
				if err != nil || got != 1 || tx.updated != "-1.000000" {
					t.Fatalf("empty balance settlement=%d %v %+v", got, err, tx)
				}
				return
			}
			want := coverageFailure
			if stage == "cost" || stage == "negative" || stage == "channel" || stage == "parse" {
				want = apperrors.ErrInvalid
			}
			if stage == "missing" {
				want = apperrors.ErrNotFound
			}
			if !errors.Is(err, want) || got != 0 || tx.inserted {
				t.Fatalf("settlement=%d %v %+v", got, err, tx)
			}
		})
	}
}

func TestChatPreflightAndSettlementFailures(t *testing.T) {
	for _, mode := range []string{"model", "permission", "estimate", "rate reserve", "route", "route unavailable", "quota", "estimated pricing", "rewrite absent", "rewrite error", "secret", "URL", "parse absent", "usage absent", "pricing", "settlement"} {
		t.Run(mode, func(t *testing.T) {
			s, st, auth := newProtocolSeamService(t, streamTransport(), coverageAdapter())
			req := ChatRequest{Model: "public-model", Body: []byte(`{}`)}
			want := coverageFailure
			switch mode {
			case "model":
				req.Model = ""
				want = ErrInvalidRequest
			case "permission":
				auth.Permissions = []byte(`{"models":["other"]}`)
				want = ErrForbidden
			case "estimate":
				s.adapter.EstimateUsage = func([]byte, int) (EstimatedUsage, error) { return EstimatedUsage{}, coverageFailure }
				want = ErrRateLimited
			case "rate reserve":
				s.ratelimit = coverageRate{RateLimit: s.ratelimit, reserveErr: coverageFailure}
				want = ErrRateLimited
			case "route":
				s.catalog = coverageCatalog{Catalog: s.catalog, err: coverageFailure}
			case "route unavailable":
				s.catalog = coverageCatalog{Catalog: s.catalog, err: ErrNoHealthyChannel}
				want = ErrNoHealthyChannel
			case "quota":
				s.quota = coverageQuota{err: coverageFailure}
			case "estimated pricing":
				s.catalog = coverageCatalog{Catalog: s.catalog, pricing: &catalog.PricingDTO{}, err: coverageFailure}
			case "rewrite absent":
				s.adapter.RewriteRequest = nil
				want = ErrInvalidRequest
			case "rewrite error":
				s.adapter.RewriteRequest = func([]byte, string) ([]byte, error) { return nil, coverageFailure }
				want = ErrInvalidRequest
			case "secret":
				s.catalog = coverageCatalog{Catalog: s.catalog, secretErr: coverageFailure}
			case "URL":
				s.catalog = coverageCatalog{Catalog: s.catalog, baseURL: "://bad"}
				want = ErrUpstream
			case "parse absent":
				s.adapter.ParseUsage = nil
				want = ErrInvalidRequest
			case "usage absent":
				s.adapter.ParseUsage = func([]byte) *Usage { return nil }
				want = ErrUpstream
			case "pricing":
				base := s.catalog
				s.catalog = &delayedPricingFailure{Catalog: base}
			case "settlement":
				s.settleTx = coverageTxManager{err: coverageFailure}
			}
			_, err := s.ChatCompletions(context.Background(), auth, req, "")
			if !errors.Is(err, want) {
				t.Fatalf("error=%v want %v", err, want)
			}
			active, err := st.CountActiveRateLimitReservations(context.Background(), auth.UserID, &auth.KeyID, "public-model", nil)
			if err != nil || active != 0 {
				t.Fatalf("leaked active=%d error=%v", active, err)
			}
		})
	}
	s, _, auth := newProtocolSeamService(t, streamTransport(), coverageAdapter())
	s.sticky.put(stickyKey{owner: auth.UserID, key: auth.KeyID, model: "public-model"}, 1, s.now())
	s.catalog = coverageCatalog{Catalog: s.catalog, singleErr: coverageFailure}
	if _, err := s.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{}`)}, ""); !errors.Is(err, coverageFailure) {
		t.Fatalf("sticky lookup=%v", err)
	}
}

type delayedPricingFailure struct {
	Catalog
	calls int
}

func (c *delayedPricingFailure) GetPricing(ctx context.Context, id int, m string) (catalog.PricingDTO, error) {
	c.calls++
	if c.calls > 1 {
		return catalog.PricingDTO{}, coverageFailure
	}
	return c.Catalog.GetPricing(ctx, id, m)
}

func TestPricingAndRoutingDefensiveInputs(t *testing.T) {
	s, _, auth := newProtocolSeamService(t, streamTransport(), coverageAdapter())
	s.catalog = coverageCatalog{Catalog: s.catalog, pricing: &catalog.PricingDTO{InputPricePer1M: "invalid", OutputPricePer1M: "0"}}
	if _, _, _, err := s.priceFor(context.Background(), 1, "up", nil); err == nil {
		t.Fatal("invalid price accepted")
	}
	s.catalog = coverageCatalog{Catalog: s.catalog, err: catalog.ErrNotFound}
	if cost, _, _, err := s.priceFor(context.Background(), 1, "up", nil); err != nil || cost != "0.000000" {
		t.Fatalf("free price=%s %v", cost, err)
	}
	s.catalog = coverageCatalog{Catalog: s.catalog, routes: []catalog.RouteCandidate{{ChannelID: 1, Weight: 0, Priority: 1}, {ChannelID: 2, Weight: 0, Priority: 1}}}
	routes, _, err := s.orderedCandidates(context.Background(), auth.UserID, "public-model")
	if err != nil || len(routes) != 2 {
		t.Fatalf("zero-weight routes=%+v %v", routes, err)
	}
	s.catalog = coverageCatalog{Catalog: s.catalog, routes: []catalog.RouteCandidate{{ChannelID: 1, Weight: 0, Priority: 1}, {ChannelID: 2, Weight: 1, Priority: 1}}}
	routes, _, err = s.orderedCandidates(context.Background(), auth.UserID, "public-model")
	if err != nil || routes[0].ChannelID != 2 {
		t.Fatalf("weighted routes=%+v %v", routes, err)
	}
	s.randIntN = func(n int) int { return n }
	routes, _, err = s.orderedCandidates(context.Background(), auth.UserID, "public-model")
	if err != nil || routes[0].ChannelID != 1 {
		t.Fatalf("defensive route=%+v %v", routes, err)
	}
}

func TestStreamFailureSettlementAndCleanup(t *testing.T) {
	for _, mode := range []string{"missing adapter", "close lease", "close quota", "lease forwarding", "cancel tail", "deadline tail", "actual settlement failure", "success settlement failure", "tokenization failure", "done write failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			adapter := streamAdapter(func(_ io.Reader, _ string, emit func(StreamEvent) error) error {
				usage := &Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}
				if mode == "tokenization failure" {
					usage = nil
				}
				if err := emit(StreamEvent{Data: true, Frame: []byte("data"), Text: "text", Usage: usage}); err != nil {
					return err
				}
				switch mode {
				case "cancel tail":
					cancel()
					return nil
				case "deadline tail":
					return nil
				case "actual settlement failure":
					return ErrInvalidStream
				case "tokenization failure":
					return ErrInvalidStream
				}
				return emit(StreamEvent{Done: true})
			})
			adapter.CountTextTokens = func(string, string) (int, error) { return 0, coverageFailure }
			s, st, auth := newProtocolSeamService(t, streamTransport(), adapter)
			if mode == "missing adapter" {
				s.adapter.ParseStream = nil
			}
			policyName, scope, period, scopeID, limit := "stream test", "user", "day", auth.UserID, int64(100)
			_, err := newTestQuota(st, nil).CreateQuotaPolicy(context.Background(), auth.UserID, quota.QuotaPolicyInput{PolicyName: &policyName, ScopeType: &scope, PeriodType: &period, ScopeID: &scopeID, TokenLimit: &limit})
			if err != nil {
				t.Fatal(err)
			}
			response, err := s.ChatCompletions(ctx, auth, ChatRequest{Model: "public-model", Stream: true, Body: []byte(`{}`)}, "")
			if mode == "missing adapter" {
				if !errors.Is(err, ErrInvalidRequest) {
					t.Fatalf("missing adapter=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stream := response.Stream.(*completionStream)
			defer stream.Close()
			if mode == "deadline tail" {
				deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer deadlineCancel()
				stream.ctx = deadlineCtx
			}
			if mode == "actual settlement failure" || mode == "success settlement failure" {
				s.settleTx = coverageTxManager{err: coverageFailure}
			}
			if mode == "close lease" || mode == "lease forwarding" {
				stream.probeLeaseID = "lease"
				rec := &coverageProbeCatalog{Catalog: s.catalog}
				s.catalog = rec
				defer func() {
					if rec.released == 0 {
						t.Error("probe lease not released")
					}
				}()
			}
			if mode == "close lease" || mode == "close quota" {
				stream.Close()
				policies, _ := newTestQuota(st, nil).ListQuotaUsage(context.Background(), quota.QuotaPolicyFilter{OwnerUserID: auth.UserID, Page: 1, PageSize: 10})
				if policies.List[0].ReservedTokens != 0 {
					t.Fatalf("quota not released=%+v", policies)
				}
				return
			}
			err = stream.Forward(func(b []byte) error {
				if mode == "done write failure" && string(b) == "data: [DONE]\n\n" {
					return coverageFailure
				}
				return nil
			})
			if mode == "lease forwarding" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("expected stream failure")
			}
			logs, _ := st.ListUsageLogs(context.Background(), auth.UserID, usage.UsageLogFilter{Page: 1, PageSize: 10})
			if logs.Total != 1 {
				t.Fatalf("logs=%+v", logs)
			}
			if mode == "actual settlement failure" && logs.List[0].ErrorCode != "partial_actual_settlement_failed" {
				t.Fatalf("partial failure log=%+v", logs.List[0])
			}
			if mode == "success settlement failure" && logs.List[0].ErrorCode != "settlement_failed" {
				t.Fatalf("settlement failure log=%+v", logs.List[0])
			}
		})
	}
	s, _, _ := newProtocolSeamService(t, streamTransport(), coverageAdapter())
	s.finalizeRateReservation(context.Background(), 0, nil)
}

type coverageProbeCatalog struct {
	Catalog
	released int
}

func (c *coverageProbeCatalog) ReleaseChannelProbe(ctx context.Context, id int, lease string) (bool, error) {
	c.released++
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return true, nil
}

func TestUpstreamRetryAndRequestContextEdges(t *testing.T) {
	finalReadService, finalReadStore, finalReadAuth := newProtocolSeamService(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(coverageReadFailure{})}, nil
	}), coverageAdapter())
	if _, err := finalReadService.ChatCompletions(context.Background(), finalReadAuth, ChatRequest{Model: "public-model", Body: []byte(`{}`)}, ""); !errors.Is(err, ErrUpstream) {
		t.Fatalf("terminal read error=%v", err)
	}
	logs, _ := finalReadStore.ListUsageLogs(context.Background(), 1, usage.UsageLogFilter{Page: 1, PageSize: 10})
	if logs.Total != 1 || logs.List[0].ErrorCode != "upstream_stream_interrupted" {
		t.Fatalf("terminal read log=%+v", logs)
	}
	for _, mode := range []string{"transport", "read", "fallback cap", "fallback error"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if mode == "read" {
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(coverageReadFailure{})}, nil
					}
					return nil, coverageFailure
				}
				return streamTransport()(nil)
			})
			s, st, auth := newProtocolSeamService(t, transport, coverageAdapter())
			addSeamFallback(t, st, "two", "http://two.test")
			if mode == "fallback cap" {
				addSeamFallback(t, st, "three", "http://three.test")
				s.maxAttempts = 2
			}
			if mode == "fallback cap" || mode == "fallback error" {
				key := stickyKey{owner: auth.UserID, key: auth.KeyID, model: "public-model"}
				s.sticky.put(key, 1, s.now())
			}
			if mode == "fallback error" {
				s.catalog = coverageRouteFailure{Catalog: s.catalog}
			}
			response, err := s.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{}`)}, "")
			if mode == "fallback error" {
				if !errors.Is(err, ErrUpstream) || calls != 1 {
					t.Fatalf("fallback error=%v calls=%d", err, calls)
				}
			} else if err != nil || response.Status != 200 || calls != 2 {
				t.Fatalf("retry result=%+v err=%v calls=%d", response, err, calls)
			}
		})
	}
	s, _, auth := newProtocolSeamService(t, streamTransport(), coverageAdapter())
	candidates, _, err := s.orderedCandidates(context.Background(), auth.UserID, "public-model")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.attemptUpstreams(ctx, upstreamAttemptInput{auth: auth, req: ChatRequest{Model: "public-model"}, candidates: candidates, start: s.now()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled attempt=%v", err)
	}
}

type coverageReadFailure struct{}

func (coverageReadFailure) Read([]byte) (int, error) { return 0, coverageFailure }

type coverageRouteFailure struct{ Catalog }

func (coverageRouteFailure) RouteCandidates(context.Context, int, string) (catalog.ListResponse[catalog.RouteCandidate], error) {
	return catalog.ListResponse[catalog.RouteCandidate]{}, coverageFailure
}

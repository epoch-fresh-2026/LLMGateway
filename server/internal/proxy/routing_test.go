package proxy

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func newRouteTestApp(st *storefake.Store, randIntN func(int) int) *Service {
	return &Service{
		store:     st,
		catalog:   newTestCatalog(st),
		quota:     newTestQuota(st, nil),
		ratelimit: newTestRateLimit(st, nil),
		settleTx:  st.SettlementTx(),
		client:    &http.Client{},
		randIntN:  randIntN,
		now:       time.Now,
	}
}

func seedRoutingStore(t *testing.T) *storefake.Store {
	t.Helper()
	st := storefake.New()
	cat := newTestCatalog(st)
	create := func(name string, priority, weight int, balance string) int {
		var balancePtr *string
		if balance != "" {
			balancePtr = &balance
		}
		created, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: name, BaseURL: "https://" + name + ".test", APIKey: "sk", Status: 1, Priority: priority, Weight: weight, Balance: balancePtr})
		if err != nil {
			t.Fatal(err)
		}
		return created.ID
	}
	low := create("low", 5, 100, "")
	highA := create("highA", 10, 100, "")
	highB := create("highB", 10, 200, "")
	zero := create("zero", 20, 500, "0.000000")
	for _, id := range []int{low, highA, highB, zero} {
		if _, err := cat.CreateChannelModel(context.Background(), 1, id, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestOrderedCandidatesUsesHighestPriorityGroup(t *testing.T) {
	st := seedRoutingStore(t)

	// rand 0 selects the first candidate in the highest priority group, which
	// is ordered by weight desc (highB weight 200 before highA weight 100).
	a := newRouteTestApp(st, func(int) int { return 0 })
	candidates, _, err := a.orderedCandidates(context.Background(), 1, "gpt")
	if err != nil {
		t.Fatalf("orderedCandidates: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected candidates")
	}
	candidate := candidates[0]
	if candidate.ChannelName != "highB" || candidate.Priority != 10 {
		t.Fatalf("candidate = %+v, want highB priority 10", candidate)
	}
	if candidate.Balance != nil {
		t.Fatalf("expected nil balance, got %v", *candidate.Balance)
	}
}

func TestOrderedCandidatesWeightedFallback(t *testing.T) {
	st := seedRoutingStore(t)

	// Total weight in the top group is 300; a pick of 299 lands on highA.
	a := newRouteTestApp(st, func(int) int { return 299 })
	candidates, _, err := a.orderedCandidates(context.Background(), 1, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) == 0 || candidates[0].ChannelName != "highA" {
		t.Fatalf("candidates = %+v, want highA first", candidates)
	}
}

func TestOrderedCandidatesExcludesNonPositiveBalance(t *testing.T) {
	st := seedRoutingStore(t)
	cat := newTestCatalog(st)

	// Only the zero-balance channel serves "only-zero".
	if _, err := cat.CreateChannelModel(context.Background(), 1, 4, domain.ChannelModel{ModelName: "only-zero", UpstreamModel: "up", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	a := newRouteTestApp(st, func(int) int { return 0 })
	candidates, _, err := a.orderedCandidates(context.Background(), 1, "only-zero")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want none", candidates)
	}
}

func TestSelectChannelExcludesBalanceBelowConfiguredReserve(t *testing.T) {
	st := storefake.New()
	cat := newTestCatalog(st)
	lowBalance := "0.999999"
	highBalance := "1.000000"
	for _, input := range []domain.ChannelInput{
		{Name: "below-reserve", BaseURL: "https://below.test", APIKey: "sk", Status: 1, Priority: 10, Weight: 100, Balance: &lowBalance},
		{Name: "at-reserve", BaseURL: "https://at.test", APIKey: "sk", Status: 1, Priority: 10, Weight: 100, Balance: &highBalance},
		{Name: "unlimited", BaseURL: "https://unlimited.test", APIKey: "sk", Status: 1, Priority: 10, Weight: 100},
	} {
		channel, err := cat.CreateChannel(context.Background(), 1, input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cat.CreateChannelModel(context.Background(), 1, channel.ID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "gpt", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}

	a := newRouteTestApp(st, func(int) int { return 0 })
	a.ConfigureMinimumRouteBalance("1.000000")
	candidates, _, err := a.orderedCandidates(context.Background(), 1, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].ChannelName == "below-reserve" || candidates[1].ChannelName == "below-reserve" {
		t.Fatalf("candidates = %+v, want channels at-reserve and unlimited", candidates)
	}
}

func TestOrderedCandidatesScopedToOwner(t *testing.T) {
	st := storefake.New()
	cat := newTestCatalog(st)
	for owner, name := range map[int]string{1: "mine", 2: "other"} {
		channel, err := cat.CreateChannel(context.Background(), owner, domain.ChannelInput{Name: name, BaseURL: "https://" + name + ".test", APIKey: "sk", Status: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cat.CreateChannelModel(context.Background(), owner, channel.ID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}

	a := newRouteTestApp(st, func(int) int { return 0 })
	candidates, _, err := a.orderedCandidates(context.Background(), 1, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ChannelName != "mine" {
		t.Fatalf("owner 1 candidates = %+v, want only its own channel", candidates)
	}
}

func TestOrderedCandidatesNoCandidates(t *testing.T) {
	a := newRouteTestApp(storefake.New(), func(int) int { return 0 })
	candidates, _, err := a.orderedCandidates(context.Background(), 1, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want none", candidates)
	}
}

func TestOrderedCandidatesDeduplicateChannelsAndKeepPriorityFallbacks(t *testing.T) {
	st := storefake.New()
	cat := newTestCatalog(st)
	ids := []int{}
	for _, input := range []domain.ChannelInput{
		{Name: "preferred", BaseURL: "http://preferred", APIKey: "x", Status: 1, Priority: 10, Weight: 10},
		{Name: "fallback", BaseURL: "http://fallback", APIKey: "x", Status: 1, Priority: 5, Weight: 1},
	} {
		created, err := cat.CreateChannel(context.Background(), 1, input)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	for _, channelID := range []int{ids[0], ids[1]} {
		if _, err := cat.CreateChannelModel(context.Background(), 1, channelID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "gpt", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(st, cat, newTestQuota(st, nil), newTestRateLimit(st, nil), nil, func(int) int { return 0 }, time.Now)
	candidates, _, err := service.orderedCandidates(context.Background(), 1, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].ChannelID != ids[0] || candidates[1].ChannelID != ids[1] {
		t.Fatalf("candidates = %+v, want channels 1,2 once", candidates)
	}
}

func TestHalfOpenProbeLeaseIsAcquiredOnlyOnceAndReleased(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	st := storefake.NewWithClock(func() time.Time { return clock })
	cipher, err := crypto.NewCipher([]byte(testEncryptionKey))
	if err != nil {
		t.Fatal(err)
	}
	cat := catalog.New(catalog.Deps{Store: st, Health: st, Tx: st.CatalogTx(), Cipher: cipher, Client: &http.Client{}, Now: func() time.Time { return clock }})
	channel, err := cat.CreateChannel(ctx, 1, domain.ChannelInput{Name: "c", BaseURL: "https://c.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(ctx, 1, channel.ID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := cat.RecordChannelFailure(ctx, channel.ID, domain.FailureUpstream5xx); err != nil {
			t.Fatal(err)
		}
	}
	fallback, err := cat.CreateChannel(ctx, 1, domain.ChannelInput{Name: "fallback", BaseURL: "https://fallback.test", APIKey: "sk", Status: 1, Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(ctx, 1, fallback.ID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.UpdateUserBreakerConfig(ctx, 1, catalog.ChannelBreakerConfigInput{WindowSeconds: 60, MinimumSamples: 10, ErrorRatePercent: 50, TimeoutRatePercent: 50, CooldownSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(30 * time.Second)
	routes, err := cat.RouteCandidates(ctx, 1, "gpt")
	if err != nil || len(routes.List) != 1 || routes.List[0].ChannelID != fallback.ID {
		t.Fatalf("routes before owner cooldown = %+v, err=%v", routes, err)
	}
	clock = clock.Add(30 * time.Second)

	svc := &Service{
		store: st, catalog: cat, quota: newTestQuota(st, nil), ratelimit: newTestRateLimit(st, nil),
		settleTx: st.SettlementTx(), client: &http.Client{}, randIntN: func(int) int { return 0 },
		now: func() time.Time { return clock }, requestTimeout: time.Minute,
	}

	candidates, probes, err := svc.orderedCandidates(ctx, 1, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].HealthState != catalog.HealthHalfOpen || probes[channel.ID] == "" {
		t.Fatalf("candidates=%d probes=%v, want one half-open probe lease", len(candidates), probes)
	}

	// A concurrent request must not get a second probe while the lease is held.
	if secondCandidates, second, err := svc.orderedCandidates(ctx, 1, "gpt"); err != nil || len(second) != 0 || len(secondCandidates) != 1 || secondCandidates[0].ChannelID != fallback.ID {
		t.Fatalf("second orderedCandidates candidates=%v probes=%v err=%v, want fallback only", secondCandidates, second, err)
	}

	// Releasing by owner frees the gate immediately.
	svc.releaseProbes(ctx, probes)
	if _, ok, err := st.AcquireChannelProbe(ctx, channel.ID, time.Minute); err != nil || !ok {
		t.Fatalf("probe lease not released: ok=%v err=%v", ok, err)
	}
}

type routingCountCatalog struct {
	Catalog
	healthCalls int
	probeCalls  int
	probeErr    error
	routeErr    error
	listCalls   int
	singleCalls int
	released    []int
	duplicate   bool
	halfOpen    bool
}

func (c *routingCountCatalog) GetChannelHealth(ctx context.Context, channelID int) (catalog.ChannelHealth, error) {
	c.healthCalls++
	return c.Catalog.GetChannelHealth(ctx, channelID)
}

func (c *routingCountCatalog) AcquireChannelProbe(ctx context.Context, channelID int, lease time.Duration) (string, bool, error) {
	c.probeCalls++
	if c.probeErr != nil {
		return "", false, c.probeErr
	}
	return c.Catalog.AcquireChannelProbe(ctx, channelID, lease)
}

func (c *routingCountCatalog) RouteCandidates(ctx context.Context, ownerUserID int, modelName string) (catalog.ListResponse[catalog.RouteCandidate], error) {
	c.listCalls++
	if c.routeErr != nil {
		return catalog.ListResponse[catalog.RouteCandidate]{}, c.routeErr
	}
	result, err := c.Catalog.RouteCandidates(ctx, ownerUserID, modelName)
	if c.halfOpen {
		for i := range result.List {
			result.List[i].HealthState = catalog.HealthHalfOpen
		}
	}
	if c.duplicate {
		result.List = append(result.List, result.List...)
	}
	return result, err
}

func (c *routingCountCatalog) RouteCandidate(ctx context.Context, owner int, model string, id int) (catalog.RouteCandidate, bool, error) {
	c.singleCalls++
	return c.Catalog.RouteCandidate(ctx, owner, model, id)
}

func (c *routingCountCatalog) ReleaseChannelProbe(ctx context.Context, id int, lease string) (bool, error) {
	c.released = append(c.released, id)
	return c.Catalog.ReleaseChannelProbe(ctx, id, lease)
}

func TestOrderedCandidatesClosedChannelsAvoidHealthQueries(t *testing.T) {
	a := newRouteTestApp(seedRoutingStore(t), func(int) int { return 0 })
	counted := &routingCountCatalog{Catalog: a.catalog}
	a.catalog = counted
	candidates, probes, err := a.orderedCandidates(context.Background(), 1, "gpt")
	if err != nil || len(candidates) != 3 || len(probes) != 0 || counted.healthCalls != 0 || counted.probeCalls != 0 {
		t.Fatalf("candidates=%v probes=%v err=%v healthCalls=%d probeCalls=%d", candidates, probes, err, counted.healthCalls, counted.probeCalls)
	}
	for _, candidate := range candidates {
		if candidate.HealthState != catalog.HealthClosed {
			t.Fatalf("candidate health=%q, want closed", candidate.HealthState)
		}
	}
	counted.routeErr = errors.New("route health query failed")
	if candidates, _, err := a.orderedCandidates(context.Background(), 1, "gpt"); !errors.Is(err, counted.routeErr) || len(candidates) != 0 {
		t.Fatalf("candidates=%v err=%v, want route error", candidates, err)
	}
}

func TestOrderedCandidatesHalfOpenProbeErrorFallsBack(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	st := storefake.NewWithClock(func() time.Time { return clock })
	a := newRouteTestApp(st, func(int) int { return 0 })
	for i, name := range []string{"probe", "fallback"} {
		channel, err := a.catalog.(*catalog.Server).CreateChannel(ctx, 1, domain.ChannelInput{Name: name, BaseURL: "https://" + name + ".test", APIKey: "sk", Status: 1, Priority: 10 - i})
		if err != nil {
			t.Fatal(err)
		}
		cat := a.catalog.(*catalog.Server)
		if _, err := cat.CreateChannelModel(ctx, 1, channel.ID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			for j := 0; j < 5; j++ {
				if _, err := cat.RecordChannelFailure(ctx, channel.ID, catalog.FailureUpstream5xx); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	clock = clock.Add(time.Minute)
	counted := &routingCountCatalog{Catalog: a.catalog, probeErr: errors.New("probe unavailable")}
	a.catalog = counted
	candidates, probes, err := a.orderedCandidates(ctx, 1, "gpt")
	if err != nil || len(candidates) != 1 || candidates[0].ChannelName != "fallback" || len(probes) != 0 || counted.healthCalls != 0 || counted.probeCalls != 1 {
		t.Fatalf("candidates=%v probes=%v err=%v healthCalls=%d probeCalls=%d", candidates, probes, err, counted.healthCalls, counted.probeCalls)
	}
}

func TestStickyCandidateFreshGates(t *testing.T) {
	for _, mode := range []string{"disabled", "mapping", "owner", "balance", "cooldown", "probe", "half_open", "remap"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			service, st, auth := newProtocolSeamService(t, nil, seamAdapter())
			cat := newTestCatalog(st)
			fallback := addSeamFallback(t, st, "fallback", "http://fallback.test")
			key := stickyKey{owner: auth.UserID, key: auth.KeyID, model: "public-model"}
			service.sticky.put(key, 1, service.now())
			switch mode {
			case "disabled":
				if _, err := cat.UpdateChannelStatus(ctx, 1, 1, 0); err != nil {
					t.Fatal(err)
				}
			case "mapping":
				if _, err := cat.UpdateChannelModel(ctx, 1, 1, 1, "public-model", false); err != nil {
					t.Fatal(err)
				}
			case "remap":
				if err := cat.DeleteChannelModel(ctx, 1, 1, 1); err != nil {
					t.Fatal(err)
				}
				if _, err := cat.CreateChannelModel(ctx, 1, 1, domain.ChannelModel{ModelName: "public-model", UpstreamModel: "new-upstream", Enabled: true}); err != nil {
					t.Fatal(err)
				}
			case "owner":
				key.owner = 2
				service.sticky.put(key, 1, service.now())
			case "balance":
				service.ConfigureMinimumRouteBalance("11")
			case "cooldown", "probe", "half_open":
				clock := time.Now()
				healthStore := storefake.NewWithClock(func() time.Time { return clock })
				cipher, err := crypto.NewCipher([]byte(testEncryptionKey))
				if err != nil {
					t.Fatal(err)
				}
				healthCat := catalog.New(catalog.Deps{Store: healthStore, Health: healthStore, Tx: healthStore.CatalogTx(), Cipher: cipher, Now: func() time.Time { return clock }})
				channel, err := healthCat.CreateChannel(ctx, 1, domain.ChannelInput{Name: "probe", BaseURL: "http://probe.test", APIKey: "x", Status: 1})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := healthCat.CreateChannelModel(ctx, 1, channel.ID, domain.ChannelModel{ModelName: key.model, UpstreamModel: "up", Enabled: true}); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 5; i++ {
					if _, err := healthCat.RecordChannelFailure(ctx, 1, catalog.FailureUpstream5xx); err != nil {
						t.Fatal(err)
					}
				}
				if mode != "cooldown" {
					clock = clock.Add(time.Minute)
				}
				if mode == "probe" {
					lease, ok, err := healthCat.AcquireChannelProbe(ctx, 1, time.Minute)
					if err != nil || !ok {
						t.Fatalf("lease=%s ok=%v err=%v", lease, ok, err)
					}
					defer healthCat.ReleaseChannelProbe(ctx, 1, lease)
				}
				service.catalog = healthCat
				fallback = 0
			}
			counted := &routingCountCatalog{Catalog: service.catalog}
			service.catalog = counted
			candidates, probes, _, lazy, err := service.stickyCandidates(ctx, key)
			defer service.releaseProbes(ctx, probes)
			if err != nil || counted.singleCalls != 1 {
				t.Fatalf("candidates=%v err=%v singles=%d", candidates, err, counted.singleCalls)
			}
			valid := mode == "half_open" || mode == "remap"
			if valid {
				if !lazy || counted.listCalls != 0 || len(candidates) != 1 || candidates[0].ChannelID != 1 {
					t.Fatalf("fast path rejected: %v", candidates)
				}
				if mode == "remap" && candidates[0].UpstreamModel != "new-upstream" {
					t.Fatal("stale mapping")
				}
				if mode == "half_open" && probes[1] == "" {
					t.Fatal("probe not acquired")
				}
			} else {
				if lazy || counted.listCalls != 1 || service.sticky.get(key, service.now()).channelID != 0 {
					t.Fatal("ineligible cache survived")
				}
				if mode == "owner" || fallback == 0 {
					if len(candidates) != 0 {
						t.Fatalf("ineligible channel routed: %v", candidates)
					}
				} else if len(candidates) != 1 || candidates[0].ChannelID != fallback {
					t.Fatalf("fallback=%v", candidates)
				}
			}
		})
	}
}

func TestOrderedCandidatesSticksAPIKeyAndModelToSameChannel(t *testing.T) {
	st := seedRoutingStore(t)
	a := newRouteTestApp(st, func(int) int { return 299 })
	first, _, err := a.orderedCandidates(context.Background(), 1, "gpt", 42)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := a.orderedCandidates(context.Background(), 1, "gpt", 42)
	if err != nil {
		t.Fatal(err)
	}
	if first[0].ChannelID != second[0].ChannelID {
		t.Fatalf("sticky first candidates = %d,%d, want same channel", first[0].ChannelID, second[0].ChannelID)
	}
	otherKey, _, err := a.orderedCandidates(context.Background(), 1, "gpt", 43)
	if err != nil {
		t.Fatal(err)
	}
	if otherKey[0].ChannelID == 0 {
		t.Fatalf("other key candidate = %+v", otherKey)
	}
}

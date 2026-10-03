package storefake

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"LLMGateway/server/internal/catalog"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func newHealthTestStore() (*Store, *time.Time) {
	current := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	st := NewWithClock(func() time.Time { return current })
	return st, &current
}

func newHealthCatalog(st *Store, clock *time.Time) *catalog.Server {
	return newHealthCatalogWithBreaker(st, clock, catalog.ChannelBreakerConfig{})
}

func newHealthCatalogWithBreaker(st *Store, clock *time.Time, breaker catalog.ChannelBreakerConfig) *catalog.Server {
	return catalog.New(catalog.Deps{
		Store:   st,
		Health:  st,
		Tx:      st.CatalogTx(),
		Cipher:  testCipher(),
		Client:  &http.Client{},
		Now:     func() time.Time { return *clock },
		Breaker: breaker,
	})
}

func TestChannelHealthLifecycle(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)

	// Missing row is closed.
	health, err := cat.GetChannelHealth(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if health.State != domain.HealthClosed {
		t.Fatalf("initial state = %s, want closed", health.State)
	}

	// Five consecutive failures trip the breaker.
	for i := 0; i < 5; i++ {
		health, err = cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx)
		if err != nil {
			t.Fatal(err)
		}
	}
	if health.State != domain.HealthOpen || health.OpenedAt == nil {
		t.Fatalf("state = %s, want open with opened_at", health.State)
	}
	if health.FailureCount != 5 {
		t.Fatalf("failure_count = %d, want 5", health.FailureCount)
	}

	// Before the cooldown the channel is still open.
	if got, _ := cat.GetChannelHealth(context.Background(), 1); got.State != domain.HealthOpen {
		t.Fatalf("before cooldown state = %s, want open", got.State)
	}

	// After the cooldown it becomes half-open lazily.
	*clock = clock.Add(30 * time.Second)
	if got, _ := cat.GetChannelHealth(context.Background(), 1); got.State != domain.HealthHalfOpen {
		t.Fatalf("after cooldown state = %s, want half-open", got.State)
	}

	// A success closes it again.
	closed, err := cat.RecordChannelSuccess(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != domain.HealthClosed || closed.ConsecutiveFailures != 0 || closed.OpenedAt != nil {
		t.Fatalf("unexpected closed state: %+v", closed)
	}
}

func TestChannelHealthHalfOpenFailureReopens(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	for i := 0; i < 5; i++ {
		if _, err := cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx); err != nil {
			t.Fatal(err)
		}
	}
	*clock = clock.Add(30 * time.Second)

	reopened, err := cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.State != domain.HealthOpen {
		t.Fatalf("state = %s, want open", reopened.State)
	}
}

func TestResetChannelHealth(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	ctx := context.Background()
	for _, id := range []int{1, 2} {
		if _, err := cat.RecordChannelSuccess(ctx, id); err != nil {
			t.Fatal(err)
		}
		if _, err := cat.RecordChannelFailure(ctx, id, domain.FailureUpstream401); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := cat.AcquireChannelProbe(ctx, id, time.Minute); err != nil || !ok {
			t.Fatalf("acquire channel %d: ok=%v err=%v", id, ok, err)
		}
	}
	other := *st.channelHealth[2]
	otherLease := st.probes[2]
	otherBucket := *st.healthBuckets[2][catalog.ChannelHealthBucketStart(*clock).Unix()]
	*clock = clock.Add(time.Second)
	for i := 0; i < 2; i++ {
		if err := cat.ResetChannelHealth(ctx, 1); err != nil {
			t.Fatal(err)
		}
		health, found, err := st.GetChannelHealthRow(ctx, 1)
		if err != nil || !found || health.State != domain.HealthClosed || health.SuccessCount != 1 || health.FailureCount != 1 || health.ConsecutiveFailures != 0 || health.OpenedAt != nil || health.UpdatedAt != clock.Format(time.RFC3339) {
			t.Fatalf("after reset: found=%v health=%+v err=%v", found, health, err)
		}
		if len(st.healthBuckets[1]) != 0 {
			t.Fatal("reset retained channel buckets")
		}
		if _, ok := st.probes[1]; ok {
			t.Fatal("reset retained probe lease")
		}
	}
	if *st.channelHealth[2] != other || st.probes[2] != otherLease || *st.healthBuckets[2][catalog.ChannelHealthBucketStart(*clock).Unix()] != otherBucket {
		t.Fatal("reset changed another channel")
	}
	if _, ok, err := cat.AcquireChannelProbe(ctx, 1, time.Minute); err != nil || !ok {
		t.Fatalf("probe after reset: ok=%v err=%v", ok, err)
	}
	if err := st.CatalogTx().InTx(ctx, func(tx catalog.Tx) error {
		return tx.UpsertChannelHealthBucket(3, catalog.ChannelHealthBucketStart(*clock), 1, 1, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cat.AcquireChannelProbe(ctx, 3, time.Minute); err != nil || !ok {
		t.Fatalf("missing health probe: ok=%v err=%v", ok, err)
	}
	for i := 0; i < 2; i++ {
		if err := cat.ResetChannelHealth(ctx, 3); err != nil {
			t.Fatal(err)
		}
		if _, found, err := st.GetChannelHealthRow(ctx, 3); err != nil || found {
			t.Fatalf("reset created missing health: found=%v err=%v", found, err)
		}
		if len(st.healthBuckets[3]) != 0 {
			t.Fatal("missing health reset retained buckets")
		}
		if _, ok := st.probes[3]; ok {
			t.Fatal("missing health reset retained probe")
		}
	}
}

func TestListChannelHealth(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "channel", BaseURL: "https://channel.test", APIKey: "secret", Status: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx); err != nil {
		t.Fatal(err)
	}
	list, err := cat.ListChannelHealth(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 {
		t.Fatalf("total = %d, want 1", list.Total)
	}
	entry := list.List[0]
	if entry.ChannelID != 1 || entry.State != "closed" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestChannelHealthConcurrentFailures(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)

	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent RecordChannelFailure: %v", err)
	}

	health, err := cat.GetChannelHealth(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if health.ConsecutiveFailures != workers {
		t.Fatalf("consecutive_failures = %d, want %d", health.ConsecutiveFailures, workers)
	}
}

func TestChannelProbeLeaseAllowsOnlyOneConcurrentProbe(t *testing.T) {
	st := NewWithClock(func() time.Time { return time.Unix(100, 0).UTC() })
	first, ok, err := st.AcquireChannelProbe(context.Background(), 1, time.Minute)
	if err != nil || !ok || first == "" {
		t.Fatalf("first probe = %q,%v,%v", first, ok, err)
	}
	if _, ok, err := st.AcquireChannelProbe(context.Background(), 1, time.Minute); err != nil || ok {
		t.Fatalf("second probe = ok:%v err:%v, want denied", ok, err)
	}
	// A release from a different owner must not free the lease.
	if released, _ := st.ReleaseChannelProbe(context.Background(), 1, "other-owner"); released {
		t.Fatal("wrong-owner release freed the lease")
	}
	if released, err := st.ReleaseChannelProbe(context.Background(), 1, first); err != nil || !released {
		t.Fatalf("release = %v,%v, want freed", released, err)
	}
	// Once released, the gate admits a new probe immediately.
	if _, ok, err := st.AcquireChannelProbe(context.Background(), 1, time.Minute); err != nil || !ok {
		t.Fatalf("re-acquire = ok:%v err:%v, want allowed", ok, err)
	}
}

func TestWindowErrorRateOpensDespiteInterleavedSuccesses(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalogWithBreaker(st, clock, catalog.ChannelBreakerConfig{
		FailureThreshold:   99,
		Cooldown:           30 * time.Second,
		WindowSeconds:      60,
		MinimumSamples:     4,
		ErrorRatePercent:   50,
		TimeoutRatePercent: 50,
	})
	// Interleaved successes keep resetting the consecutive counter, but the
	// window error rate crosses the threshold.
	for i := 0; i < 2; i++ {
		if _, err := cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx); err != nil {
			t.Fatal(err)
		}
		if _, err := cat.RecordChannelSuccess(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
	}
	health, err := cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx)
	if err != nil {
		t.Fatal(err)
	}
	if health.State != domain.HealthOpen {
		t.Fatalf("state = %s, want open via window error rate", health.State)
	}
}

func TestUserBreakerConfigAdminLifecycle(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	got, err := cat.GetUserBreakerConfig(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.WindowSeconds != 60 || got.CooldownSeconds != 30 || got.ErrorRatePercent != 50 {
		t.Fatalf("defaults = %+v, want global defaults", got)
	}

	updated, err := cat.UpdateUserBreakerConfig(context.Background(), 1, catalog.ChannelBreakerConfigInput{WindowSeconds: 120, MinimumSamples: 20, ErrorRatePercent: 30, TimeoutRatePercent: 40, CooldownSeconds: 15})
	if err != nil {
		t.Fatal(err)
	}
	if updated.WindowSeconds != 120 || updated.CooldownSeconds != 15 || updated.MinimumSamples != 20 {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := cat.UpdateUserBreakerConfig(context.Background(), 1, catalog.ChannelBreakerConfigInput{WindowSeconds: 0, MinimumSamples: 1, ErrorRatePercent: 50, TimeoutRatePercent: 50, CooldownSeconds: 10}); err == nil {
		t.Fatal("expected validation error for non-positive window")
	}

	if err := cat.DeleteUserBreakerConfig(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	back, _ := cat.GetUserBreakerConfig(context.Background(), 1)
	if back.WindowSeconds != 60 || back.CooldownSeconds != 30 {
		t.Fatalf("after delete = %+v, want global defaults", back)
	}
}

func TestUserBreakerConfigSharedWithIndependentChannels(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	ctx := context.Background()
	ids := make([]int, 3)
	for i, owner := range []int{1, 1, 2} {
		channel, err := cat.CreateChannel(ctx, owner, catalog.ChannelInput{Name: "test", BaseURL: "https://example.test", APIKey: "placeholder", Status: 1})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = channel.ID
	}
	initial, err := cat.GetUserBreakerConfig(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if initial.WindowSeconds != 60 || initial.CooldownSeconds != 30 {
		t.Fatalf("process defaults = %+v", initial)
	}

	if _, err := cat.UpdateUserBreakerConfig(context.Background(), 1, catalog.ChannelBreakerConfigInput{WindowSeconds: 120, MinimumSamples: 20, ErrorRatePercent: 30, TimeoutRatePercent: 40, CooldownSeconds: 15}); err != nil {
		t.Fatal(err)
	}
	inherited, err := cat.GetUserBreakerConfig(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if inherited.WindowSeconds != 120 || inherited.CooldownSeconds != 15 {
		t.Fatalf("inherited = %+v, want user defaults", inherited)
	}

	for _, id := range ids {
		if _, err := cat.RecordChannelFailure(ctx, id, domain.FailureUpstream401); err != nil {
			t.Fatal(err)
		}
	}
	*clock = clock.Add(16 * time.Second)
	for i, id := range ids {
		health, err := cat.GetChannelHealth(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		want := domain.HealthHalfOpen
		if i == 2 {
			want = domain.HealthOpen
		}
		if health.State != want {
			t.Fatalf("channel %d state = %s, want %s", id, health.State, want)
		}
	}
	if _, err := cat.RecordChannelSuccess(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	first, _ := cat.GetChannelHealth(ctx, ids[0])
	second, _ := cat.GetChannelHealth(ctx, ids[1])
	if first.State != domain.HealthClosed || first.SuccessCount != 1 || second.State != domain.HealthHalfOpen || second.SuccessCount != 0 || second.FailureCount != 1 {
		t.Fatalf("channel states are not independent: first=%+v second=%+v", first, second)
	}
	if _, err := cat.UpdateUserBreakerConfig(ctx, 1, catalog.ChannelBreakerConfigInput{WindowSeconds: 60, MinimumSamples: 10, ErrorRatePercent: 50, TimeoutRatePercent: 50, CooldownSeconds: 5}); err != nil {
		t.Fatal(err)
	}
	if err := cat.DeleteUserBreakerConfig(ctx, 1); err != nil {
		t.Fatal(err)
	}
	second, _ = cat.GetChannelHealth(ctx, ids[1])
	if second.State != domain.HealthOpen {
		t.Fatalf("after config deletion state = %s, want open with process cooldown", second.State)
	}
}

func TestReapChannelHealthBuckets(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	if _, err := cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx); err != nil {
		t.Fatal(err)
	}
	// Advance well past the retention window, record again, then reap.
	*clock = clock.Add(20 * time.Minute)
	if _, err := cat.RecordChannelSuccess(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	removed, err := cat.ReapChannelHealthBuckets(context.Background(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 stale bucket", removed)
	}
}

func TestRouteCandidateValidation(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	ctx := context.Background()
	balance := "0"
	created, err := cat.CreateChannel(ctx, 1, domain.ChannelInput{Name: "route", BaseURL: "https://api.test", APIKey: "sk", Status: 1, Priority: 7, Weight: 23, Balance: &balance})
	if err != nil {
		t.Fatal(err)
	}
	model, err := cat.CreateChannelModel(ctx, 1, created.ID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(ctx, 1, created.ID, domain.ChannelModel{ModelName: "other", UpstreamModel: "other-up", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	check := func(owner int, name string, id int, found bool, state catalog.HealthState) {
		t.Helper()
		candidate, ok, err := cat.RouteCandidate(ctx, owner, name, id)
		if err != nil || ok != found {
			t.Fatalf("RouteCandidate(%d, %s, %d): ok=%v err=%v", owner, name, id, ok, err)
		}
		if !found {
			if candidate.ChannelID != 0 {
				t.Fatalf("unexpected candidate: %+v", candidate)
			}
			return
		}
		if candidate.ChannelID != created.ID || candidate.UpstreamModel != "up" || candidate.Priority != 7 || candidate.Weight != 23 || candidate.Balance == nil || *candidate.Balance != "0.000000" || candidate.HealthState != state {
			t.Fatalf("unexpected candidate: %+v", candidate)
		}
		list, err := cat.RouteCandidates(ctx, owner, name)
		if err != nil || list.Total != 1 || list.List[0].HealthState != candidate.HealthState || list.List[0].UpstreamModel != candidate.UpstreamModel {
			t.Fatalf("list mismatch: %+v err=%v", list, err)
		}
	}
	check(1, "gpt", created.ID, true, catalog.HealthClosed)
	check(2, "gpt", created.ID, false, "")
	check(1, "missing", created.ID, false, "")
	check(1, "gpt", created.ID+100, false, "")
	if _, err := cat.UpdateChannelStatus(ctx, 1, created.ID, 0); err != nil {
		t.Fatal(err)
	}
	check(1, "gpt", created.ID, false, "")
	if _, err := cat.UpdateChannelStatus(ctx, 1, created.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.UpdateChannelModel(ctx, 1, created.ID, model.ID, "gpt", false); err != nil {
		t.Fatal(err)
	}
	check(1, "gpt", created.ID, false, "")
	if _, err := cat.UpdateChannelModel(ctx, 1, created.ID, model.ID, "gpt", true); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.UpdateUserBreakerConfig(ctx, 1, catalog.ChannelBreakerConfigInput{WindowSeconds: 60, MinimumSamples: 10, ErrorRatePercent: 50, TimeoutRatePercent: 50, CooldownSeconds: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.RecordChannelFailure(ctx, created.ID, catalog.FailureUpstream401); err != nil {
		t.Fatal(err)
	}
	check(1, "gpt", created.ID, false, "")
	*clock = clock.Add(5 * time.Second)
	check(1, "gpt", created.ID, true, catalog.HealthHalfOpen)
	if err := cat.DeleteChannelModel(ctx, 1, created.ID, model.ID); err != nil {
		t.Fatal(err)
	}
	check(1, "gpt", created.ID, false, "")
}

func TestRouteCandidatesExcludeOpenChannel(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	created, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	channelID := created.ID
	if _, err := cat.CreateChannelModel(context.Background(), 1, channelID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	candidates, err := cat.RouteCandidates(context.Background(), 1, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if candidates.Total != 1 {
		t.Fatalf("candidates total = %d, want 1 before tripping", candidates.Total)
	}

	for i := 0; i < 5; i++ {
		if _, err := cat.RecordChannelFailure(context.Background(), channelID, domain.FailureUpstream5xx); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err = cat.RouteCandidates(context.Background(), 1, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if candidates.Total != 0 {
		t.Fatalf("candidates total = %d, want 0 while open", candidates.Total)
	}
}

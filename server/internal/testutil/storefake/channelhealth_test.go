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
	if _, err := cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream401); err != nil {
		t.Fatal(err)
	}
	if err := cat.ResetChannelHealth(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	health, _ := cat.GetChannelHealth(context.Background(), 1)
	if health.State != domain.HealthClosed || health.FailureCount != 0 {
		t.Fatalf("after reset: %+v", health)
	}
}

func TestListChannelHealth(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	if _, err := cat.CreateChannel(context.Background(), domain.ChannelInput{Name: "channel", BaseURL: "https://channel.test", APIKey: "secret", Status: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx); err != nil {
		t.Fatal(err)
	}
	list, err := cat.ListChannelHealth(context.Background())
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

func TestPerChannelBreakerConfigOverridesCooldown(t *testing.T) {
	st, clock := newHealthTestStore()
	st.breakerConfigs[1] = catalog.ChannelBreakerConfig{Cooldown: 5 * time.Second}
	cat := newHealthCatalog(st, clock)
	for i := 0; i < 5; i++ {
		if _, err := cat.RecordChannelFailure(context.Background(), 1, domain.FailureUpstream5xx); err != nil {
			t.Fatal(err)
		}
	}
	*clock = clock.Add(5 * time.Second)
	health, err := cat.GetChannelHealth(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if health.State != domain.HealthHalfOpen {
		t.Fatalf("state = %s, want half-open after per-channel cooldown", health.State)
	}
}

func TestChannelBreakerConfigAdminLifecycle(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	created, err := cat.CreateChannel(context.Background(), domain.ChannelInput{Name: "c", BaseURL: "https://c.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	id := created.ID

	got, err := cat.GetChannelBreakerConfig(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.WindowSeconds != 60 || got.CooldownSeconds != 30 || got.ErrorRatePercent != 50 {
		t.Fatalf("defaults = %+v, want global defaults", got)
	}

	updated, err := cat.UpdateChannelBreakerConfig(context.Background(), id, catalog.ChannelBreakerConfigInput{WindowSeconds: 120, MinimumSamples: 20, ErrorRatePercent: 30, TimeoutRatePercent: 40, CooldownSeconds: 15})
	if err != nil {
		t.Fatal(err)
	}
	if updated.WindowSeconds != 120 || updated.CooldownSeconds != 15 || updated.MinimumSamples != 20 {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := cat.UpdateChannelBreakerConfig(context.Background(), id, catalog.ChannelBreakerConfigInput{WindowSeconds: 0, MinimumSamples: 1, ErrorRatePercent: 50, TimeoutRatePercent: 50, CooldownSeconds: 10}); err == nil {
		t.Fatal("expected validation error for non-positive window")
	}

	if err := cat.DeleteChannelBreakerConfig(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	back, _ := cat.GetChannelBreakerConfig(context.Background(), id)
	if back.WindowSeconds != 60 || back.CooldownSeconds != 30 {
		t.Fatalf("after delete = %+v, want global defaults", back)
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

func TestRouteCandidatesExcludeOpenChannel(t *testing.T) {
	st, clock := newHealthTestStore()
	cat := newHealthCatalog(st, clock)
	created, err := cat.CreateChannel(context.Background(), domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	channelID := created.ID
	if _, err := cat.CreateChannelModel(context.Background(), channelID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	candidates, err := cat.RouteCandidates(context.Background(), "gpt")
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
	candidates, err = cat.RouteCandidates(context.Background(), "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if candidates.Total != 0 {
		t.Fatalf("candidates total = %d, want 0 while open", candidates.Total)
	}
}

package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"LLMGateway/server/internal/catalog"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func createHealthTestChannel(t *testing.T, owner int, cat *catalog.Server) int {
	t.Helper()
	created, err := cat.CreateChannel(context.Background(), owner, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	channelID := created.ID
	if _, err := cat.CreateChannelModel(context.Background(), owner, channelID, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return channelID
}

func TestPGChannelHealthLifecycle(t *testing.T) {
	st := testStore(t)
	owner := testOwner(t, st)
	cat := testCatalog(t, st)
	ctx := context.Background()
	channelID := createHealthTestChannel(t, owner, cat)

	health, err := cat.GetChannelHealth(context.Background(), channelID)
	if err != nil {
		t.Fatalf("GetChannelHealth: %v", err)
	}
	if health.State != domain.HealthClosed {
		t.Fatalf("initial state = %s, want closed", health.State)
	}

	for i := 0; i < 5; i++ {
		health, err = cat.RecordChannelFailure(context.Background(), channelID, domain.FailureUpstream5xx)
		if err != nil {
			t.Fatalf("RecordChannelFailure: %v", err)
		}
	}
	if health.State != domain.HealthOpen || health.OpenedAt == nil {
		t.Fatalf("state = %s, want open with opened_at", health.State)
	}

	candidates, err := cat.RouteCandidates(context.Background(), owner, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if candidates.Total != 0 {
		t.Fatalf("candidates total = %d, want 0 while open", candidates.Total)
	}

	// Backdate opened_at so the cooldown has elapsed; the channel is allowed again.
	if _, err := st.pool.Exec(ctx, "UPDATE channel_health SET opened_at = now() - interval '1 minute' WHERE channel_id = $1", channelID); err != nil {
		t.Fatal(err)
	}
	candidates, err = cat.RouteCandidates(context.Background(), owner, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if candidates.Total != 1 {
		t.Fatalf("candidates total = %d, want 1 after cooldown", candidates.Total)
	}

	closed, err := cat.RecordChannelSuccess(context.Background(), channelID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != domain.HealthClosed || closed.ConsecutiveFailures != 0 || closed.OpenedAt != nil {
		t.Fatalf("unexpected closed state: %+v", closed)
	}

	if err := cat.ResetChannelHealth(context.Background(), channelID); err != nil {
		t.Fatalf("ResetChannelHealth: %v", err)
	}
	reset, _ := cat.GetChannelHealth(context.Background(), channelID)
	if reset.State != domain.HealthClosed || reset.FailureCount != 0 {
		t.Fatalf("after reset: %+v", reset)
	}
}

func TestPGChannelHealthHalfOpen(t *testing.T) {
	st := testStore(t)
	owner := testOwner(t, st)
	cat := testCatalog(t, st)
	ctx := context.Background()
	channelID := createHealthTestChannel(t, owner, cat)

	for i := 0; i < 5; i++ {
		if _, err := cat.RecordChannelFailure(context.Background(), channelID, domain.FailureUpstream5xx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.pool.Exec(ctx, "UPDATE channel_health SET opened_at = now() - interval '1 minute' WHERE channel_id = $1", channelID); err != nil {
		t.Fatal(err)
	}

	health, err := cat.GetChannelHealth(context.Background(), channelID)
	if err != nil {
		t.Fatal(err)
	}
	if health.State != domain.HealthHalfOpen {
		t.Fatalf("state = %s, want half-open after cooldown", health.State)
	}

	reopened, err := cat.RecordChannelFailure(context.Background(), channelID, domain.FailureUpstream5xx)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.State != domain.HealthOpen {
		t.Fatalf("state = %s, want open after half-open failure", reopened.State)
	}
}

func TestPGChannelHealthConcurrentFailures(t *testing.T) {
	st := testStore(t)
	owner := testOwner(t, st)
	cat := testCatalog(t, st)
	channelID := createHealthTestChannel(t, owner, cat)

	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cat.RecordChannelFailure(context.Background(), channelID, domain.FailureUpstream5xx); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent RecordChannelFailure: %v", err)
	}

	health, err := cat.GetChannelHealth(context.Background(), channelID)
	if err != nil {
		t.Fatal(err)
	}
	if health.ConsecutiveFailures != workers {
		t.Fatalf("consecutive_failures = %d, want %d (lost updates)", health.ConsecutiveFailures, workers)
	}
	if health.FailureCount != int64(workers) {
		t.Fatalf("failure_count = %d, want %d", health.FailureCount, workers)
	}
	if health.State != domain.HealthOpen {
		t.Fatalf("state = %s, want open", health.State)
	}
}

type healthSnapshot struct {
	state             string
	consecutive       int
	successCount      int64
	failureCount      int64
	openedAtSet       bool
	stateAfterCool    string
	deterministicOpen string
}

func runHealthScenario(t *testing.T, owner int, cat *catalog.Server, clock *time.Time) healthSnapshot {
	t.Helper()

	created, err := cat.CreateChannel(context.Background(), owner, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	channelID := created.ID

	var health domain.ChannelHealth
	for i := 0; i < 5; i++ {
		health, err = cat.RecordChannelFailure(context.Background(), channelID, domain.FailureUpstream5xx)
		if err != nil {
			t.Fatal(err)
		}
	}

	snapshot := healthSnapshot{
		state:        string(health.State),
		consecutive:  health.ConsecutiveFailures,
		successCount: health.SuccessCount,
		failureCount: health.FailureCount,
		openedAtSet:  health.OpenedAt != nil,
	}

	*clock = clock.Add(30 * time.Second)
	afterCool, err := cat.GetChannelHealth(context.Background(), channelID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.stateAfterCool = string(afterCool.State)

	// Deterministic failure on a fresh channel opens immediately.
	second, err := cat.CreateChannel(context.Background(), owner, domain.ChannelInput{Name: "Other", BaseURL: "https://other.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	det, err := cat.RecordChannelFailure(context.Background(), second.ID, domain.FailureUpstream401)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.deterministicOpen = string(det.State)

	return snapshot
}

func TestPGChannelHealthWindowAndConfig(t *testing.T) {
	st := testStore(t)
	owner := testOwner(t, st)
	cat := testCatalog(t, st)
	ctx := context.Background()
	channelID := createHealthTestChannel(t, owner, cat)

	override := catalog.ChannelBreakerConfig{Cooldown: 5 * time.Second, WindowSeconds: 60, MinimumSamples: 4, ErrorRatePercent: 50, TimeoutRatePercent: 50}
	if err := st.CatalogTx().InTx(ctx, func(tx catalog.Tx) error {
		return tx.UpsertUserBreakerConfig(owner, override)
	}); err != nil {
		t.Fatalf("UpsertUserBreakerConfig: %v", err)
	}
	row, found, err := st.GetUserBreakerConfigRow(ctx, owner)
	if err != nil || !found {
		t.Fatalf("GetUserBreakerConfigRow found=%v err=%v", found, err)
	}
	if row.Cooldown != override.Cooldown || row.WindowSeconds != override.WindowSeconds || row.ErrorRatePercent != override.ErrorRatePercent {
		t.Fatalf("breaker config = %+v", row)
	}

	bucket := catalog.ChannelHealthBucketStart(st.now().UTC())
	var window catalog.ChannelHealthWindow
	if err := st.CatalogTx().InTx(ctx, func(tx catalog.Tx) error {
		if err := tx.UpsertChannelHealthBucket(channelID, bucket, 1, 1, 0); err != nil {
			return err
		}
		var sumErr error
		window, sumErr = tx.GetChannelHealthWindow(channelID, bucket)
		return sumErr
	}); err != nil {
		t.Fatalf("bucket tx: %v", err)
	}
	if window.Requests != 1 || window.Errors != 1 || window.Timeouts != 0 {
		t.Fatalf("window = %+v, want 1/1/0", window)
	}

	removed, err := st.DeleteStaleChannelHealthBuckets(ctx, st.now().UTC().Add(time.Hour))
	if err != nil || removed != 1 {
		t.Fatalf("DeleteStaleChannelHealthBuckets removed=%d err=%v", removed, err)
	}
}

func TestPGChannelHealthScenario(t *testing.T) {
	fixed := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	pg := testStore(t)
	pgClock := fixed
	pg.now = func() time.Time { return pgClock }
	owner := testOwner(t, pg)
	cat := testCatalog(t, pg)
	got := runHealthScenario(t, owner, cat, &pgClock)

	want := healthSnapshot{
		state: "open", consecutive: 5, successCount: 0, failureCount: 5,
		openedAtSet: true, stateAfterCool: "half-open", deterministicOpen: "open",
	}
	if got != want {
		t.Fatalf("health snapshot = %+v, want %+v", got, want)
	}
}

package storefake

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func strp(value string) *string { return &value }
func intp(value int) *int       { return &value }
func int64p(value int64) *int64 { return &value }
func boolp(value bool) *bool    { return &value }

func TestRateLimitRejectsForeignTarget(t *testing.T) {
	st := New()
	rl := newRateLimit(st, nil)
	acc := newAccounts(st)
	cat := newCatalog(st)
	ctx := context.Background()

	st.SeedUser("one", "hash")
	st.SeedUser("two", "hash")
	key1, err := acc.CreateKey(ctx, 1, domain.KeyInput{KeyName: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	key2, err := acc.CreateKey(ctx, 2, domain.KeyInput{KeyName: "k2"})
	if err != nil {
		t.Fatal(err)
	}
	channel1, err := cat.CreateChannel(ctx, 1, domain.ChannelInput{Name: "one", BaseURL: "https://one.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	channel2, err := cat.CreateChannel(ctx, 2, domain.ChannelInput{Name: "two", BaseURL: "https://two.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(ctx, 1, channel1.ID, domain.ChannelModel{ModelName: "mine", UpstreamModel: "up", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannelModel(ctx, 2, channel2.ID, domain.ChannelModel{ModelName: "theirs", UpstreamModel: "up", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	rule := func(targetType, targetValue string) domain.RateLimitInput {
		return domain.RateLimitInput{RuleName: strp("rule"), TargetType: strp(targetType), TargetValue: strp(targetValue), Metric: strp("rpm"), LimitValue: int64p(1), Action: strp("reject")}
	}

	// Own resources are accepted.
	if _, err := rl.CreateRateLimit(ctx, 1, rule("api_key", strconv.Itoa(key1.ID))); err != nil {
		t.Fatalf("own key target: %v", err)
	}
	if _, err := rl.CreateRateLimit(ctx, 1, rule("channel", strconv.Itoa(channel1.ID))); err != nil {
		t.Fatalf("own channel target: %v", err)
	}
	if _, err := rl.CreateRateLimit(ctx, 1, rule("model", "mine")); err != nil {
		t.Fatalf("own model target: %v", err)
	}

	// Another user's resources are rejected.
	for _, in := range []domain.RateLimitInput{
		rule("api_key", strconv.Itoa(key2.ID)),
		rule("channel", strconv.Itoa(channel2.ID)),
		rule("model", "theirs"),
		rule("user", "2"),
	} {
		if _, err := rl.CreateRateLimit(ctx, 1, in); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("foreign target (%s=%s) err = %v, want ErrInvalid", *in.TargetType, *in.TargetValue, err)
		}
	}
}

func TestRateLimitCRUDAndFilter(t *testing.T) {
	st := New()
	rl := newRateLimit(st, nil)

	created, err := rl.CreateRateLimit(context.Background(), 1, domain.RateLimitInput{
		RuleName:   strp("default user rpm"),
		TargetType: strp("user"),
		Metric:     strp("rpm"),
		LimitValue: int64p(600),
		Action:     strp("reject"),
	})
	if err != nil {
		t.Fatalf("CreateRateLimit: %v", err)
	}
	if created.ID != 1 || created.TargetValue != "*" || created.Priority != 100 || created.Enabled != true {
		t.Fatalf("unexpected rule: %+v", created)
	}
	if created.Extras != nil && string(created.Extras) != "{}" {
		t.Fatalf("extras = %v, want {}", created.Extras)
	}

	if _, err := rl.CreateRateLimit(context.Background(), 1, domain.RateLimitInput{RuleName: strp("bad"), TargetType: strp("nope"), Metric: strp("rpm"), LimitValue: int64p(1), Action: strp("reject")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid target_type err = %v, want ErrInvalid", err)
	}
	if _, err := rl.CreateRateLimit(context.Background(), 1, domain.RateLimitInput{RuleName: strp("bad"), TargetType: strp("user"), Metric: strp("bogus"), LimitValue: int64p(1), Action: strp("reject")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid metric err = %v, want ErrInvalid", err)
	}
	if _, err := rl.CreateRateLimit(context.Background(), 1, domain.RateLimitInput{RuleName: strp("deprecated"), TargetType: strp("user"), Metric: strp("tpd"), LimitValue: int64p(1), Action: strp("reject")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("deprecated tpd metric err = %v, want ErrInvalid", err)
	}
	if _, err := rl.CreateRateLimit(context.Background(), 1, domain.RateLimitInput{RuleName: strp("bad"), TargetType: strp("user"), Metric: strp("rpm"), LimitValue: int64p(0), Action: strp("reject")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("non-positive limit err = %v, want ErrInvalid", err)
	}

	if _, err := rl.CreateRateLimit(context.Background(), 1, domain.RateLimitInput{RuleName: strp("queue"), TargetType: strp("model"), Metric: strp("rpm"), LimitValue: int64p(1000), Action: strp("queue")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("queue action err = %v, want ErrInvalid", err)
	}

	// Partial update: only enabled.
	updated, err := rl.UpdateRateLimit(context.Background(), 1, 1, domain.RateLimitInput{Enabled: boolp(false)})
	if err != nil {
		t.Fatalf("UpdateRateLimit: %v", err)
	}
	if updated.Enabled != false || updated.RuleName != "default user rpm" {
		t.Fatalf("partial update lost fields: %+v", updated)
	}

	enabled := true
	enabledList, err := rl.ListRateLimits(context.Background(), 1, &enabled, 1, 20)
	if err != nil {
		t.Fatalf("ListRateLimits: %v", err)
	}
	if enabledList.Total != 0 {
		t.Fatalf("enabled total = %d, want 0", enabledList.Total)
	}

	all, _ := rl.ListRateLimits(context.Background(), 1, nil, 1, 20)
	if all.Total != 1 {
		t.Fatalf("all total = %d, want 1", all.Total)
	}

	if err := rl.DeleteRateLimit(context.Background(), 1, 1); err != nil {
		t.Fatalf("DeleteRateLimit: %v", err)
	}
	if err := rl.DeleteRateLimit(context.Background(), 1, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
	if _, err := rl.UpdateRateLimit(context.Background(), 1, 404, domain.RateLimitInput{Enabled: boolp(true)}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update missing err = %v, want ErrNotFound", err)
	}
}

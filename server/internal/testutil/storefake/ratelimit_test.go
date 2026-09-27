package storefake

import (
	"context"
	"errors"
	"testing"

	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func strp(value string) *string { return &value }
func intp(value int) *int       { return &value }
func int64p(value int64) *int64 { return &value }
func boolp(value bool) *bool    { return &value }

func TestRateLimitCRUDAndFilter(t *testing.T) {
	st := New()
	rl := newRateLimit(st, nil)

	created, err := rl.CreateRateLimit(context.Background(), domain.RateLimitInput{
		RuleName:      strp("default user rpm"),
		TargetType:    strp("user"),
		Metric:        strp("rpm"),
		LimitValue:    int64p(600),
		WindowSeconds: intp(60),
		Action:        strp("reject"),
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

	if _, err := rl.CreateRateLimit(context.Background(), domain.RateLimitInput{RuleName: strp("bad"), TargetType: strp("nope"), Metric: strp("rpm"), LimitValue: int64p(1), WindowSeconds: intp(1), Action: strp("reject")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid target_type err = %v, want ErrInvalid", err)
	}
	if _, err := rl.CreateRateLimit(context.Background(), domain.RateLimitInput{RuleName: strp("bad"), TargetType: strp("user"), Metric: strp("bogus"), LimitValue: int64p(1), WindowSeconds: intp(1), Action: strp("reject")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid metric err = %v, want ErrInvalid", err)
	}
	if _, err := rl.CreateRateLimit(context.Background(), domain.RateLimitInput{RuleName: strp("deprecated"), TargetType: strp("user"), Metric: strp("tpd"), LimitValue: int64p(1), WindowSeconds: intp(86400), Action: strp("reject")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("deprecated tpd metric err = %v, want ErrInvalid", err)
	}
	if _, err := rl.CreateRateLimit(context.Background(), domain.RateLimitInput{RuleName: strp("bad"), TargetType: strp("user"), Metric: strp("rpm"), LimitValue: int64p(0), WindowSeconds: intp(60), Action: strp("reject")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("non-positive limit err = %v, want ErrInvalid", err)
	}

	if _, err := rl.CreateRateLimit(context.Background(), domain.RateLimitInput{RuleName: strp("queue"), TargetType: strp("model"), Metric: strp("rpm"), LimitValue: int64p(1000), WindowSeconds: intp(60), Action: strp("queue")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("queue action err = %v, want ErrInvalid", err)
	}

	// Partial update: only enabled.
	updated, err := rl.UpdateRateLimit(context.Background(), 1, domain.RateLimitInput{Enabled: boolp(false)})
	if err != nil {
		t.Fatalf("UpdateRateLimit: %v", err)
	}
	if updated.Enabled != false || updated.RuleName != "default user rpm" {
		t.Fatalf("partial update lost fields: %+v", updated)
	}

	enabled := true
	enabledList, err := rl.ListRateLimits(context.Background(), &enabled, 1, 20)
	if err != nil {
		t.Fatalf("ListRateLimits: %v", err)
	}
	if enabledList.Total != 0 {
		t.Fatalf("enabled total = %d, want 0", enabledList.Total)
	}

	all, _ := rl.ListRateLimits(context.Background(), nil, 1, 20)
	if all.Total != 1 {
		t.Fatalf("all total = %d, want 1", all.Total)
	}

	if err := rl.DeleteRateLimit(context.Background(), 1); err != nil {
		t.Fatalf("DeleteRateLimit: %v", err)
	}
	if err := rl.DeleteRateLimit(context.Background(), 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
	if _, err := rl.UpdateRateLimit(context.Background(), 404, domain.RateLimitInput{Enabled: boolp(true)}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update missing err = %v, want ErrNotFound", err)
	}
}

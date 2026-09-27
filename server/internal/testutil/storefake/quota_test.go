package storefake

import (
	"context"
	"errors"
	"testing"
	"time"

	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func TestQuotaPolicyRejectsForeignKeyScope(t *testing.T) {
	st := New()
	q := newQuota(st, nil)
	acc := newAccounts(st)
	ctx := context.Background()
	st.SeedUser("one", "hash")
	st.SeedUser("two", "hash")
	key2, err := acc.CreateKey(ctx, 2, domain.KeyInput{KeyName: "k2"})
	if err != nil {
		t.Fatal(err)
	}

	name, scope, period, limit := "foreign", "api_key", "day", int64(100)
	if _, err := q.CreateQuotaPolicy(ctx, 1, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &key2.ID, PeriodType: &period, TokenLimit: &limit}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign key scope err = %v, want ErrNotFound", err)
	}

	foreignUser, scopeUser := "foreign-user", "user"
	foreignID := 2
	if _, err := q.CreateQuotaPolicy(ctx, 1, domain.QuotaPolicyInput{PolicyName: &foreignUser, ScopeType: &scopeUser, ScopeID: &foreignID, PeriodType: &period, TokenLimit: &limit}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign user scope err = %v, want ErrNotFound", err)
	}
}

func TestQuotaReaperReleasesExpiredReservation(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	st := NewWithClock(func() time.Time { return now })
	q := newQuota(st, func() time.Time { return now })
	acc := newAccounts(st)
	st.SeedUser("quota", "hash")
	key, err := acc.CreateKey(context.Background(), 1, domain.KeyInput{KeyName: "quota"})
	if err != nil {
		t.Fatal(err)
	}
	name, scope, period, scopeID, limit := "daily", "user", "day", 1, int64(100)
	if _, err := q.CreateQuotaPolicy(context.Background(), 1, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &scopeID, PeriodType: &period, TokenLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	reservation, err := q.ReserveQuota(context.Background(), domain.QuotaReserveInput{RequestID: "expired", UserID: 1, APIKeyID: key.ID, EstimatedTokens: 40, EstimatedCost: "0.000000", ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if reservation.ID == 0 {
		t.Fatal("reservation not created")
	}
	now = now.Add(2 * time.Minute)
	count, err := q.ReapExpiredQuotaReservations(context.Background(), 100)
	if err != nil || count != 1 {
		t.Fatalf("reap = %d, %v", count, err)
	}
	usage, _ := q.ListQuotaUsage(context.Background(), domain.QuotaPolicyFilter{OwnerUserID: 1, Page: 1, PageSize: 10})
	if usage.List[0].ReservedTokens != 0 {
		t.Fatalf("usage = %+v", usage)
	}
}

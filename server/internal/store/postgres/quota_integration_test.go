package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/httpcommon"
	"LLMGateway/server/internal/proxy"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func TestPGQuotaReservationRequiresUserAndKeyQuota(t *testing.T) {
	st := testStore(t)
	q := testQuota(t, st)
	_, keyID := createQuotaTestIdentity(t, st)
	createQuotaPolicy(t, q, "user daily", "user", 1, "day", 100, "10.000000")
	createQuotaPolicy(t, q, "key daily", "api_key", keyID, "day", 50, "5.000000")

	reservation, err := q.ReserveQuota(context.Background(), domain.QuotaReserveInput{
		RequestID: "quota-1", UserID: 1, APIKeyID: keyID, Model: "gpt",
		EstimatedTokens: 40, EstimatedCost: "1.000000", ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if reservation.ID == 0 {
		t.Fatal("reservation ID is zero")
	}

	_, err = q.ReserveQuota(context.Background(), domain.QuotaReserveInput{
		RequestID: "quota-2", UserID: 1, APIKeyID: keyID, Model: "gpt",
		EstimatedTokens: 20, EstimatedCost: "1.000000", ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if !errors.Is(err, store.ErrQuotaExceeded) {
		t.Fatalf("second reservation error = %v, want ErrQuotaExceeded", err)
	}

	usage, err := q.ListQuotaUsage(context.Background(), domain.QuotaPolicyFilter{OwnerUserID: 1, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if usage.Total != 2 {
		t.Fatalf("usage total = %d, want 2", usage.Total)
	}
	for _, item := range usage.List {
		if item.ReservedTokens != 40 || item.UsedTokens != 0 {
			t.Fatalf("partial reservation leaked into bucket: %+v", item)
		}
	}
}

func TestPGQuotaReleaseAndSettlementMoveReservedToUsed(t *testing.T) {
	st := testStore(t)
	q := testQuota(t, st)
	_, keyID := createQuotaTestIdentity(t, st)
	createQuotaPolicy(t, q, "user daily", "user", 1, "day", 100, "10.000000")

	released, err := q.ReserveQuota(context.Background(), domain.QuotaReserveInput{RequestID: "release", UserID: 1, APIKeyID: keyID, Model: "gpt", EstimatedTokens: 40, EstimatedCost: "2.000000", ExpiresAt: time.Now().UTC().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.ReleaseQuota(context.Background(), released.ID); err != nil {
		t.Fatal(err)
	}

	settled, err := q.ReserveQuota(context.Background(), domain.QuotaReserveInput{RequestID: "settle", UserID: 1, APIKeyID: keyID, Model: "gpt", EstimatedTokens: 40, EstimatedCost: "2.000000", ExpiresAt: time.Now().UTC().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	usage := successUsageInput("settle", 1, 0)
	usage.APIKeyID = &keyID
	usage.ChannelID = nil
	usage.TotalTokens = 25
	usage.TotalCost = "1.250000"
	service := proxy.NewService(st, testCatalog(t, st), q, testRateLimit(t, st), nil, func(int) int { return 0 }, time.Now)
	if _, err := service.Settle(context.Background(), settlement.Input{ReservationID: settled.ID, UserID: 1, APIKeyID: keyID, Cost: "1.250000", UsageLog: usage}); err != nil {
		t.Fatal(err)
	}

	rows, err := q.ListQuotaUsage(context.Background(), domain.QuotaPolicyFilter{OwnerUserID: 1, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if rows.Total != 1 || rows.List[0].ReservedTokens != 0 || rows.List[0].UsedTokens != 25 || rows.List[0].ReservedCost != "0.000000" || rows.List[0].UsedCost != "1.250000" {
		t.Fatalf("quota usage = %+v", rows)
	}
}

func TestPGQuotaConcurrentReservationsDoNotOversell(t *testing.T) {
	st := testStore(t)
	q := testQuota(t, st)
	_, keyID := createQuotaTestIdentity(t, st)
	createQuotaPolicy(t, q, "user daily", "user", 1, "day", 100, "10.000000")

	var successes atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := q.ReserveQuota(context.Background(), domain.QuotaReserveInput{
				RequestID: fmt.Sprintf("concurrent-%d", i), UserID: 1, APIKeyID: keyID, Model: "gpt",
				EstimatedTokens: 10, EstimatedCost: "0.100000", ExpiresAt: time.Now().UTC().Add(time.Minute),
			})
			if err == nil {
				successes.Add(1)
				return
			}
			if !errors.Is(err, store.ErrQuotaExceeded) {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("unexpected reservation error: %v", err)
	}
	if successes.Load() != 10 {
		t.Fatalf("successful reservations = %d, want 10", successes.Load())
	}
	usage, err := q.ListQuotaUsage(context.Background(), domain.QuotaPolicyFilter{OwnerUserID: 1, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if usage.Total != 1 || usage.List[0].ReservedTokens != 100 {
		t.Fatalf("quota usage = %+v, want reserved_tokens=100", usage)
	}
}

func TestPGQuotaReaperReleasesExpiredReservation(t *testing.T) {
	st := testStore(t)
	q := testQuota(t, st)
	_, keyID := createQuotaTestIdentity(t, st)
	createQuotaPolicy(t, q, "user daily", "user", 1, "day", 100, "10.000000")
	now := time.Now().UTC()
	st.now = func() time.Time { return now }
	reservation, err := q.ReserveQuota(context.Background(), domain.QuotaReserveInput{
		RequestID: "expired-pg", UserID: 1, APIKeyID: keyID, Model: "gpt",
		EstimatedTokens: 40, EstimatedCost: "1.000000", ExpiresAt: now.Add(time.Minute),
	})
	if err != nil || reservation.ID == 0 {
		t.Fatalf("reserve = %+v, %v", reservation, err)
	}
	now = now.Add(2 * time.Minute)
	count, err := q.ReapExpiredQuotaReservations(context.Background(), 100)
	if err != nil || count != 1 {
		t.Fatalf("reap = %d, %v", count, err)
	}
	usage, err := q.ListQuotaUsage(context.Background(), domain.QuotaPolicyFilter{OwnerUserID: 1, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if usage.List[0].ReservedTokens != 0 || usage.List[0].ReservedCost != "0.000000" {
		t.Fatalf("quota usage = %+v", usage)
	}
}

func TestPGQuotaMonthlyCostLimitIsEnforced(t *testing.T) {
	st := testStore(t)
	q := testQuota(t, st)
	_, keyID := createQuotaTestIdentity(t, st)
	createQuotaPolicy(t, q, "key monthly cost", "api_key", keyID, "month", 100000, "1.000000")
	if _, err := q.ReserveQuota(context.Background(), domain.QuotaReserveInput{RequestID: "cost-1", UserID: 1, APIKeyID: keyID, EstimatedTokens: 10, EstimatedCost: "0.750000", ExpiresAt: time.Now().UTC().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	_, err := q.ReserveQuota(context.Background(), domain.QuotaReserveInput{RequestID: "cost-2", UserID: 1, APIKeyID: keyID, EstimatedTokens: 10, EstimatedCost: "0.300000", ExpiresAt: time.Now().UTC().Add(time.Minute)})
	if !errors.Is(err, store.ErrQuotaExceeded) {
		t.Fatalf("error = %v, want ErrQuotaExceeded", err)
	}
}

func TestPGQuotaListsTokenOnlyPolicyWithBucket(t *testing.T) {
	st := testStore(t)
	q := testQuota(t, st)
	_, keyID := createQuotaTestIdentity(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	name, scope, period, owner, tokens := "token only", "user", "day", 1, int64(100)
	policy, err := q.CreateQuotaPolicy(ctx, owner, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &owner, PeriodType: &period, TokenLimit: &tokens})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.ReserveQuota(ctx, domain.QuotaReserveInput{RequestID: "token-only", UserID: owner, APIKeyID: keyID, EstimatedTokens: 10, EstimatedCost: "0.000000", ExpiresAt: time.Now().UTC().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	filter := domain.QuotaPolicyFilter{OwnerUserID: owner, Page: 1, PageSize: 100}
	policies, err := q.ListQuotaPolicies(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if policies.Total != 1 || len(policies.List) != 1 || policies.List[0].ID != policy.ID || policies.List[0].CostLimit != nil {
		t.Fatalf("policies = %+v", policies)
	}
	usage, err := q.ListQuotaUsage(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Total != 1 || len(usage.List) != 1 || usage.List[0].CostLimit != nil || usage.List[0].ReservedTokens != 10 {
		t.Fatalf("usage = %+v", usage)
	}
	mux := http.NewServeMux()
	q.RegisterAdminRoutes(mux)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(httpcommon.WithIdentity(r.Context(), httpcommon.Identity{UserID: owner})))
	}))
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/admin/quota-policies?page=1&page_size=100", "/admin/quota-usage?page=1&page_size=100"} {
		res, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Code int `json:"code"`
			Data struct {
				List  []map[string]any `json:"list"`
				Total int              `json:"total"`
			} `json:"data"`
		}
		err = json.NewDecoder(res.Body).Decode(&envelope)
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK || envelope.Code != 0 || envelope.Data.Total != 1 || len(envelope.Data.List) != 1 {
			t.Fatalf("GET %s: status=%d envelope=%+v err=%v", path, res.StatusCode, envelope, err)
		}
		cost, present := envelope.Data.List[0]["cost_limit"]
		if !present || cost != nil {
			t.Fatalf("GET %s cost_limit = %v, present=%v", path, cost, present)
		}
	}
	for _, boundary := range []string{usage.List[0].PeriodStart, usage.List[0].PeriodEnd} {
		if _, err := time.Parse(time.RFC3339, boundary); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPGQuotaPolicyOwnerIsolation(t *testing.T) {
	st := testStore(t)
	q := testQuota(t, st)
	_, key1 := createQuotaTestIdentity(t, st)
	createQuotaTestIdentity(t, st)
	ctx := context.Background()

	name, scopeUser, period, limit := "mine", "user", "day", int64(100)
	ownerOne := 1
	if _, err := q.CreateQuotaPolicy(ctx, 1, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scopeUser, ScopeID: &ownerOne, PeriodType: &period, TokenLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	other, err := q.ListQuotaPolicies(ctx, domain.QuotaPolicyFilter{OwnerUserID: 2, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if other.Total != 0 {
		t.Fatalf("other owner sees %d policies, want 0", other.Total)
	}

	scopeKey := "api_key"
	if _, err := q.CreateQuotaPolicy(ctx, 2, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scopeKey, ScopeID: &key1, PeriodType: &period, TokenLimit: &limit}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign key scope err = %v, want ErrNotFound", err)
	}
	if _, err := q.CreateQuotaPolicy(ctx, 1, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scopeKey, ScopeID: &key1, PeriodType: &period, TokenLimit: &limit}); err != nil {
		t.Fatalf("own key scope: %v", err)
	}
}

func createQuotaTestIdentity(t *testing.T, st *Store) (string, int) {
	t.Helper()
	owner := testOwner(t, st)
	acc := accounts.New(st, st.AccountsTx())
	key, err := acc.CreateKey(context.Background(), owner, domain.KeyInput{KeyName: "quota-key"})
	if err != nil {
		t.Fatal(err)
	}
	return key.FullKey, key.ID
}

func createQuotaPolicy(t *testing.T, q *quota.Server, name, scope string, scopeID int, period string, tokens int64, cost string) {
	t.Helper()
	if _, err := q.CreateQuotaPolicy(context.Background(), 1, domain.QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &scopeID, PeriodType: &period, TokenLimit: &tokens, CostLimit: &cost}); err != nil {
		t.Fatal(err)
	}
}

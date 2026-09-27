package postgres

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"LLMGateway/server/internal/accounts"

	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func createKeyAndHash(t *testing.T, acc *accounts.Server, userID int) (int, string) {
	t.Helper()
	created, err := acc.CreateKey(context.Background(), userID, domain.KeyInput{KeyName: "default", Prefix: "sk-"})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	return created.ID, crypto.HashKey(created.FullKey)
}

func TestPGProxyStoreCapabilities(t *testing.T) {
	st := testStore(t)
	cat := testCatalog(t, st)
	acc := accounts.New(st, st.AccountsTx())

	if _, err := acc.CreateUser(context.Background(), domain.UserInput{Nickname: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.RechargeUser(context.Background(), 1, domain.RechargeInput{Amount: "25.000000"}); err != nil {
		t.Fatal(err)
	}
	keyID, keyHash := createKeyAndHash(t, acc, 1)

	auth, err := st.AuthenticateKey(context.Background(), keyHash)
	if err != nil {
		t.Fatalf("AuthenticateKey: %v", err)
	}
	if auth.KeyID != keyID || auth.UserID != 1 || !auth.KeyActive || auth.UserStatus != "active" {
		t.Fatalf("unexpected auth context: %+v", auth)
	}
	if auth.AvailableBalance != "25.000000" || auth.FrozenBalance != "0.000000" {
		t.Fatalf("unexpected balances: %+v", auth)
	}
	if string(auth.Permissions) != `{"models":["*"]}` {
		t.Fatalf("permissions = %s", auth.Permissions)
	}
	if _, err := st.AuthenticateKey(context.Background(), crypto.HashKey("sk-unknown")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown key err = %v, want ErrNotFound", err)
	}

	if err := st.UpdateKeyLastUsed(context.Background(), keyID); err != nil {
		t.Fatalf("UpdateKeyLastUsed: %v", err)
	}
	if err := st.UpdateKeyLastUsed(context.Background(), 404); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing key err = %v, want ErrNotFound", err)
	}

	result, err := acc.DebitUserBalance(context.Background(), 1, "3.5", "request")
	if err != nil {
		t.Fatalf("DebitUserBalance: %v", err)
	}
	if result.BalanceAfter != "21.500000" {
		t.Fatalf("balance_after = %v, want 21.500000", result.BalanceAfter)
	}
	if _, err := acc.DebitUserBalance(context.Background(), 1, "100.000000", "too much"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("insufficient err = %v, want ErrInvalid", err)
	}
	if _, err := acc.DebitUserBalance(context.Background(), 404, "1.000000", "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing user err = %v, want ErrNotFound", err)
	}

	// Pricing + route candidates.
	create := func(name string, status, priority, weight int, balance string) int {
		created, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: name, BaseURL: "https://" + name + ".test", APIKey: "sk", Status: status, Priority: priority, Weight: weight, Balance: strPtr(balance)})
		if err != nil {
			t.Fatal(err)
		}
		return created.ID
	}
	channelA := create("A", 1, 10, 100, "5.000000")
	channelB := create("B", 1, 10, 200, "")
	channelC := create("C", 1, 5, 100, "")
	disabledChannel := create("D", 0, 99, 999, "")
	for _, id := range []int{channelA, channelB, channelC, disabledChannel} {
		if _, err := cat.CreateChannelModel(context.Background(), 1, id, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up-gpt", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: channelA, ModelName: "gpt", InputPricePer1M: "0.10000000", OutputPricePer1M: "0.20000000", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	pricing, err := st.GetPricing(context.Background(), channelA, "gpt")
	if err != nil {
		t.Fatalf("GetPricing: %v", err)
	}
	if pricing.InputPricePer1M != "0.10000000" || pricing.UpstreamModel != "up-gpt" {
		t.Fatalf("unexpected pricing: %+v", pricing)
	}
	if _, err := st.GetPricing(context.Background(), channelA, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing pricing err = %v, want ErrNotFound", err)
	}

	candidates, err := cat.RouteCandidates(context.Background(), 1, "gpt")
	if err != nil {
		t.Fatalf("RouteCandidates: %v", err)
	}
	if candidates.Total != 3 {
		t.Fatalf("candidates total = %d, want 3", candidates.Total)
	}
	if candidates.List[0].ChannelID != channelB {
		t.Fatalf("first candidate = %+v, want channel B", candidates.List[0])
	}
	if candidates.List[1].ChannelID != channelA {
		t.Fatalf("second candidate = %+v, want channel A", candidates.List[1])
	}
	if candidates.List[2].ChannelID != channelC {
		t.Fatalf("third candidate = %+v, want channel C", candidates.List[2])
	}

	// Window counting counts all attempts, including failures.
	if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "req-1", UserID: intp(1), APIKeyID: intp(keyID), ChannelID: intp(channelA), Model: "gpt", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "req-2", UserID: intp(1), APIKeyID: intp(keyID), ChannelID: intp(channelA), Model: "gpt", Status: "error"}); err != nil {
		t.Fatal(err)
	}
	count, err := st.CountRequestsSince(context.Background(), domain.UsageCountFilter{UserID: 1, Since: "1970-01-01T00:00:00Z"})
	if err != nil {
		t.Fatalf("CountRequestsSince: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	scoped, err := st.CountRequestsSince(context.Background(), domain.UsageCountFilter{UserID: 1, APIKeyID: intp(keyID), Since: "1970-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if scoped != 2 {
		t.Fatalf("key-scoped count = %d, want 2", scoped)
	}
	if _, err := st.CountRequestsSince(context.Background(), domain.UsageCountFilter{UserID: 1, Since: "abc"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid since err = %v, want ErrInvalid", err)
	}
	if _, err := st.CountRequestsSince(context.Background(), domain.UsageCountFilter{UserID: 1}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("empty since err = %v, want ErrInvalid", err)
	}
}

func TestPGDebitUserBalanceConcurrent(t *testing.T) {
	st := testStore(t)
	acc := accounts.New(st, st.AccountsTx())

	if _, err := acc.CreateUser(context.Background(), domain.UserInput{Nickname: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.RechargeUser(context.Background(), 1, domain.RechargeInput{Amount: "100.000000"}); err != nil {
		t.Fatal(err)
	}

	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := acc.DebitUserBalance(context.Background(), 1, "1.000000", "concurrent"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent debit: %v", err)
	}

	balance, err := st.GetUserBalance(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableBalance != "80.000000" {
		t.Fatalf("balance = %v, want 80.000000", balance.AvailableBalance)
	}
}

// proxySnapshot captures the PostgreSQL proxy-store contract in one scenario.
type proxySnapshot struct {
	authActive       bool
	authUserStatus   string
	authBalance      string
	authPermissions  string
	pricingInput     string
	pricingUpstream  string
	pricingCached    string
	pricingCurrency  string
	routeOrder       []int
	routeUpstream    string
	routeBalance     string
	afterDebit       string
	insufficientErr  string
	missingPricingIs bool
	countAll         int
}

func runProxyScenario(t *testing.T, st *Store) proxySnapshot {
	t.Helper()
	cat := testCatalog(t, st)
	acc := accounts.New(st, st.AccountsTx())

	if _, err := acc.CreateUser(context.Background(), domain.UserInput{Nickname: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.RechargeUser(context.Background(), 1, domain.RechargeInput{Amount: "25.000000"}); err != nil {
		t.Fatal(err)
	}
	_, keyHash := createKeyAndHash(t, acc, 1)

	auth, err := st.AuthenticateKey(context.Background(), keyHash)
	if err != nil {
		t.Fatal(err)
	}

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
	channelA := create("A", 10, 100, "5.000000")
	channelB := create("B", 10, 200, "")
	for _, id := range []int{channelA, channelB} {
		if _, err := cat.CreateChannelModel(context.Background(), 1, id, domain.ChannelModel{ModelName: "gpt", UpstreamModel: "up-gpt", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cat.UpsertPricing(context.Background(), 1, domain.PricingInput{ChannelID: channelA, ModelName: "gpt", InputPricePer1M: "0.10000000", OutputPricePer1M: "0.20000000", CachedInputPricePer1M: "0.05000000", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}

	pricing, err := st.GetPricing(context.Background(), channelA, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	_, missingPricingErr := st.GetPricing(context.Background(), channelA, "missing")

	candidates, err := cat.RouteCandidates(context.Background(), 1, "gpt")
	if err != nil {
		t.Fatal(err)
	}
	routeOrder := []int{}
	routeUpstream := ""
	routeBalance := ""
	for _, item := range candidates.List {
		routeOrder = append(routeOrder, item.ChannelID)
		routeUpstream = item.UpstreamModel
		if item.Balance != nil {
			routeBalance = *item.Balance
		}
	}

	debit, err := acc.DebitUserBalance(context.Background(), 1, "3.5", "request")
	if err != nil {
		t.Fatal(err)
	}
	_, insufficientErr := acc.DebitUserBalance(context.Background(), 1, "100.000000", "too much")

	if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "req-1", UserID: intp(1), Model: "gpt", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	countAll, err := st.CountRequestsSince(context.Background(), domain.UsageCountFilter{UserID: 1, Since: "1970-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}

	return proxySnapshot{
		authActive:       auth.KeyActive,
		authUserStatus:   auth.UserStatus,
		authBalance:      auth.AvailableBalance,
		authPermissions:  string(auth.Permissions),
		pricingInput:     pricing.InputPricePer1M,
		pricingUpstream:  pricing.UpstreamModel,
		pricingCached:    pricing.CachedInputPricePer1M,
		pricingCurrency:  pricing.Currency,
		routeOrder:       routeOrder,
		routeUpstream:    routeUpstream,
		routeBalance:     routeBalance,
		afterDebit:       debit.BalanceAfter,
		insufficientErr:  errorKind(insufficientErr),
		missingPricingIs: errors.Is(missingPricingErr, store.ErrNotFound),
		countAll:         countAll,
	}
}

func errorKind(err error) string {
	switch {
	case err == nil:
		return "nil"
	case errors.Is(err, store.ErrInvalid):
		return "invalid"
	case errors.Is(err, store.ErrNotFound):
		return "notfound"
	default:
		return "other"
	}
}

func TestPGProxyScenario(t *testing.T) {
	got := runProxyScenario(t, testStore(t))
	want := proxySnapshot{
		authActive: true, authUserStatus: "active", authBalance: "25.000000",
		authPermissions: `{"models":["*"]}`, pricingInput: "0.10000000", pricingUpstream: "up-gpt",
		pricingCached: "0.05000000", pricingCurrency: "USD", routeOrder: []int{2, 1},
		routeUpstream: "up-gpt", routeBalance: "5.000000", afterDebit: "21.500000",
		insufficientErr: "invalid", missingPricingIs: true, countAll: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("proxy snapshot = %+v, want %+v", got, want)
	}
}

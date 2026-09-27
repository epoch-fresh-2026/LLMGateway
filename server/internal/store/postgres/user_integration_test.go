package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func userBoolPtr(value bool) *bool {
	return &value
}

func TestPGUserCRUDAndBalance(t *testing.T) {
	st := testStore(t)
	acc := accounts.New(st, st.AccountsTx())
	ctx := context.Background()

	created, err := acc.CreateUser(ctx, domain.UserInput{Nickname: "Alice"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.ID != 1 || created.UserGroup != "default" || created.Status != "active" {
		t.Fatalf("unexpected user: %+v", created)
	}
	if created.Balance.AvailableBalance != "0.000000" {
		t.Fatalf("unexpected balance: %+v", created.Balance)
	}

	if _, err := acc.UpdateUser(ctx, 1, domain.UserInput{Nickname: "Alice2", UserGroup: "vip"}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	updated, err := acc.UpdateUserStatus(ctx, 1, "suspended")
	if err != nil {
		t.Fatalf("UpdateUserStatus: %v", err)
	}
	if updated.Nickname != "Alice2" || updated.UserGroup != "vip" || updated.Status != "suspended" {
		t.Fatalf("unexpected updated user: %+v", updated)
	}
	if _, err := acc.UpdateUserStatus(ctx, 1, "bogus"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid status err = %v, want ErrInvalid", err)
	}

	listed, err := st.ListUsers(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if listed.Total != 1 {
		t.Fatalf("ListUsers total = %d, want 1", listed.Total)
	}

	recharged, err := acc.RechargeUser(ctx, 1, domain.RechargeInput{Amount: "50.5"})
	if err != nil {
		t.Fatalf("RechargeUser: %v", err)
	}
	if recharged.BalanceAfter != "50.500000" {
		t.Fatalf("balance_after = %v, want 50.500000", recharged.BalanceAfter)
	}

	// Idempotent recharge by related_order_id.
	first, err := acc.RechargeUser(ctx, 1, domain.RechargeInput{Amount: "10.000000", RelatedOrderID: "order-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := acc.RechargeUser(ctx, 1, domain.RechargeInput{Amount: "10.000000", RelatedOrderID: "order-1"})
	if err != nil {
		t.Fatal(err)
	}
	if first.BalanceAfter != second.BalanceAfter {
		t.Fatalf("idempotent recharge mismatch: %v vs %v", first.BalanceAfter, second.BalanceAfter)
	}
	balance, _ := st.GetUserBalance(context.Background(), 1)
	if balance.AvailableBalance != "60.500000" {
		t.Fatalf("available_balance = %v, want 60.500000", balance.AvailableBalance)
	}

	txs, err := st.ListBalanceTransactions(context.Background(), 1, 1, 20)
	if err != nil {
		t.Fatalf("ListBalanceTransactions: %v", err)
	}
	if txs.Total != 2 {
		t.Fatalf("transactions total = %d, want 2", txs.Total)
	}

	if _, err := acc.RechargeUser(ctx, 1, domain.RechargeInput{Amount: "abc"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid amount err = %v, want ErrInvalid", err)
	}
	if _, err := acc.UpdateUser(ctx, 404, domain.UserInput{Nickname: "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing user err = %v, want ErrNotFound", err)
	}
}

func TestPGRechargeOrderScopedPerUserAndConcurrent(t *testing.T) {
	st := testStore(t)
	acc := accounts.New(st, st.AccountsTx())
	ctx := context.Background()

	if _, err := acc.CreateUser(ctx, domain.UserInput{Nickname: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.CreateUser(ctx, domain.UserInput{Nickname: "B"}); err != nil {
		t.Fatal(err)
	}

	// The same related_order_id is allowed for different users.
	if _, err := acc.RechargeUser(ctx, 1, domain.RechargeInput{Amount: "1.000000", RelatedOrderID: "order-x"}); err != nil {
		t.Fatalf("user 1 order: %v", err)
	}
	if _, err := acc.RechargeUser(ctx, 2, domain.RechargeInput{Amount: "1.000000", RelatedOrderID: "order-x"}); err != nil {
		t.Fatalf("same order id for another user should be allowed: %v", err)
	}

	// Concurrent repeats of the same order for the same user must be idempotent.
	const workers = 8
	results := make([]domain.BalanceUpdateDTO, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = acc.RechargeUser(ctx, 1, domain.RechargeInput{Amount: "5.000000", RelatedOrderID: "order-concurrent"})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	first := results[0].BalanceAfter
	for i, result := range results {
		if result.BalanceAfter != first {
			t.Fatalf("worker %d balance_after = %v, want %v", i, result.BalanceAfter, first)
		}
	}

	// Only two transactions for user 1: order-x and order-concurrent.
	txs, err := st.ListBalanceTransactions(context.Background(), 1, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if txs.Total != 2 {
		t.Fatalf("user 1 transactions total = %d, want 2", txs.Total)
	}
}

func TestPGKeysLifecycleHidesPlaintext(t *testing.T) {
	st := testStore(t)
	acc := accounts.New(st, st.AccountsTx())
	ctx := context.Background()
	if _, err := acc.CreateUser(ctx, domain.UserInput{Nickname: "Alice"}); err != nil {
		t.Fatal(err)
	}

	created, err := acc.CreateKey(ctx, 1, domain.KeyInput{KeyName: "default", Prefix: "sk-"})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	fullKey := created.FullKey
	if fullKey == "" {
		t.Fatalf("full_key missing: %+v", created)
	}
	keyID := created.ID

	// The database must store the hash, never the plaintext key.
	var storedHash string
	if err := st.pool.QueryRow(context.Background(), "SELECT key_hash FROM client_api_keys WHERE id = $1", keyID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash != crypto.HashKey(fullKey) || storedHash == fullKey {
		t.Fatalf("key_hash not a hash of the plaintext key")
	}

	listed, err := st.ListUserKeys(context.Background(), 1, 1, 20)
	if err != nil {
		t.Fatalf("ListUserKeys: %v", err)
	}
	row := listed.List[0]
	if row.KeyName != "default" || row.Prefix != "sk-" || row.IsActive != true {
		t.Fatalf("unexpected key row: %+v", row)
	}

	global, err := st.ListKeys(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if global.Total != 1 {
		t.Fatalf("global keys total = %d, want 1", global.Total)
	}

	updated, err := acc.UpdateKey(ctx, 1, keyID, domain.KeyUpdateInput{IsActive: userBoolPtr(false)})
	if err != nil {
		t.Fatalf("UpdateKey: %v", err)
	}
	if updated.IsActive != false {
		t.Fatalf("key not deactivated: %+v", updated)
	}
	if _, err := acc.UpdateKey(ctx, 1, keyID, domain.KeyUpdateInput{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("missing is_active err = %v, want ErrInvalid", err)
	}

	reset, err := acc.ResetKey(ctx, 1, keyID)
	if err != nil {
		t.Fatalf("ResetKey: %v", err)
	}
	newKey := reset.FullKey
	if newKey == "" || newKey == fullKey {
		t.Fatalf("reset did not produce a new key")
	}
	var rotatedHash string
	if err := st.pool.QueryRow(context.Background(), "SELECT key_hash FROM client_api_keys WHERE id = $1", keyID).Scan(&rotatedHash); err != nil {
		t.Fatal(err)
	}
	if rotatedHash != crypto.HashKey(newKey) {
		t.Fatal("reset did not rotate the stored hash")
	}

	if err := acc.DeleteKey(ctx, 1, keyID); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if err := acc.DeleteKey(ctx, 1, keyID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second DeleteKey err = %v, want ErrNotFound", err)
	}
	if _, err := acc.CreateKey(ctx, 404, domain.KeyInput{}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CreateKey missing user err = %v, want ErrNotFound", err)
	}
}

func TestPGDeleteUserCascadesAllRelatedRows(t *testing.T) {
	st := testStore(t)
	cat := testCatalog(t, st)
	acc := accounts.New(st, st.AccountsTx())
	ctx := context.Background()

	if _, err := acc.CreateUser(ctx, domain.UserInput{Nickname: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk-secret", Status: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.RechargeUser(ctx, 1, domain.RechargeInput{Amount: "5.000000"}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.CreateKey(ctx, 1, domain.KeyInput{}); err != nil {
		t.Fatal(err)
	}

	// Seed rows that must be removed by the cascade.
	if _, err := st.pool.Exec(ctx, "INSERT INTO usage_logs (request_id, user_id, channel_id, model, status) VALUES ('req-1', 1, 1, 'gpt', 'success')"); err != nil {
		t.Fatalf("seed usage_logs: %v", err)
	}
	if err := acc.DeleteUser(ctx, 1); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"client_api_keys", "SELECT count(*) FROM client_api_keys WHERE user_id = 1"},
		{"user_balances", "SELECT count(*) FROM user_balances WHERE user_id = 1"},
		{"balance_transactions", "SELECT count(*) FROM balance_transactions WHERE user_id = 1"},
		{"usage_logs", "SELECT count(*) FROM usage_logs WHERE user_id = 1"},
	} {
		var count int
		if err := st.pool.QueryRow(ctx, tc.query).Scan(&count); err != nil {
			t.Fatalf("%s count: %v", tc.name, err)
		}
		if count != 0 {
			t.Fatalf("%s still has %d rows for deleted user", tc.name, count)
		}
	}
}

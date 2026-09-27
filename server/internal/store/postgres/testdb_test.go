package postgres

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/db/migrate"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testEncryptionKey is a fixed 32-byte key used only by integration tests.
const testEncryptionKey = "0123456789abcdef0123456789abcdef"

// testStore returns a PostgreSQL-backed Store connected to TEST_DATABASE_URL,
// with migrations applied and channel data truncated. The test is skipped when
// TEST_DATABASE_URL is not set so `go test ./...` works without a database.
//
// The fixture is intentionally reusable by the user/usage store issues.
func testStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	if err := migrate.Run(ctx, pool, filepath.Join("..", "..", "..", "db", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	// CASCADE clears dependent tables (channel_models, model_pricing,
	// client_api_keys, user_balances, balance_transactions, usage_logs) so the
	// fixture is reusable across domains.
	if _, err := pool.Exec(ctx, "TRUNCATE channel_breaker_probes, channel_breaker_configs, channel_health_buckets, channel_health, rate_limit_reservations, rate_limit_counters, quota_reservation_items, quota_reservations, quota_buckets, quota_policies, users, channels, rate_limit_rules RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate business tables: %v", err)
	}

	return New(pool)
}

// testOwner creates a user so channel rows satisfy the owner_user_id foreign
// key. It returns the created user id.
func testOwner(t *testing.T, st *Store) int {
	t.Helper()
	acc := accounts.New(st, st.AccountsTx())
	user, err := acc.CreateUser(context.Background(), accounts.UserInput{Nickname: "owner"})
	if err != nil {
		t.Fatalf("create owner user: %v", err)
	}
	return user.ID
}

func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	cipher, err := crypto.NewCipher([]byte(testEncryptionKey))
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}
	return cipher
}

// testCatalog wires a catalog server over the integration store. It shares the
// store clock so clock-injection tests keep working.
func testCatalog(t *testing.T, st *Store) *catalog.Server {
	t.Helper()
	return catalog.New(catalog.Deps{
		Store:  st,
		Health: st,
		Tx:     st.CatalogTx(),
		Cipher: testCipher(t),
		Client: &http.Client{},
		Now:    func() time.Time { return st.now() },
	})
}

func testQuota(t *testing.T, st *Store) *quota.Server {
	t.Helper()
	return quota.New(st, st.QuotaTx(), func() time.Time { return st.now() })
}

func testRateLimit(t *testing.T, st *Store) *ratelimit.Server {
	t.Helper()
	return ratelimit.New(st, func() time.Time { return st.now() })
}

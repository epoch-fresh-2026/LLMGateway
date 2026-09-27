package storefake

import (
	"net/http"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
)

func newAccounts(st *Store) *accounts.Server {
	return accounts.New(st, st.AccountsTx())
}

func domainHash(key string) string { return crypto.HashKey(key) }

// testEncryptionKey is a fixed 32-byte key used only by tests.
const testEncryptionKey = "0123456789abcdef0123456789abcdef"

func testCipher() *crypto.Cipher {
	cipher, err := crypto.NewCipher([]byte(testEncryptionKey))
	if err != nil {
		panic(err)
	}
	return cipher
}

// newCatalog wires a catalog server over the fake store for tests that exercise
// catalog rules.
func newCatalog(st *Store) *catalog.Server {
	return catalog.New(catalog.Deps{
		Store:  st,
		Health: st,
		Tx:     st.CatalogTx(),
		Cipher: testCipher(),
		Client: &http.Client{},
	})
}

func newQuota(st *Store, now func() time.Time) *quota.Server {
	if now == nil {
		now = time.Now
	}
	return quota.New(st, st.QuotaTx(), now)
}

func newRateLimit(st *Store, now func() time.Time) *ratelimit.Server {
	if now == nil {
		now = time.Now
	}
	return ratelimit.New(st, now)
}

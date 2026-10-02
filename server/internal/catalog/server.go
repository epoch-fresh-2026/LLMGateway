package catalog

import (
	"context"
	"net/http"
	"sync"
	"time"

	"LLMGateway/server/internal/crypto"
)

const breakerConfigCacheTTL = 30 * time.Second

type breakerConfigCacheEntry struct {
	config  ChannelBreakerConfig
	expires time.Time
}

// Deps carries the catalog server's persistence primitives and runtime
// dependencies. Cipher is required for channel key encryption/decryption.
type Deps struct {
	Store   Port
	Health  HealthPort
	Tx      TxManager
	Cipher  *crypto.Cipher
	Client  *http.Client
	Now     func() time.Time
	Breaker ChannelBreakerConfig
}

// Server owns catalog rules and orchestration over injected primitives.
type Server struct {
	store       Port
	health      HealthPort
	tx          TxManager
	cipher      *crypto.Cipher
	client      *http.Client
	now         func() time.Time
	breaker     ChannelBreakerConfig
	testTimeout time.Duration

	breakerMu    sync.RWMutex
	breakerCache map[int]breakerConfigCacheEntry
}

func New(d Deps) *Server {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	breaker := d.Breaker
	if breaker == (ChannelBreakerConfig{}) {
		breaker = DefaultChannelBreakerConfig()
	}
	return &Server{
		store:        d.Store,
		health:       d.Health,
		tx:           d.Tx,
		cipher:       d.Cipher,
		client:       d.Client,
		now:          now,
		breaker:      breaker,
		testTimeout:  channelTestTimeout,
		breakerCache: map[int]breakerConfigCacheEntry{},
	}
}

func (a *Server) baseBreakerFor(ctx context.Context, ownerUserID int) ChannelBreakerConfig {
	now := a.now()
	a.breakerMu.RLock()
	entry, ok := a.breakerCache[ownerUserID]
	a.breakerMu.RUnlock()
	if ok && now.Before(entry.expires) {
		return entry.config
	}

	a.breakerMu.Lock()
	defer a.breakerMu.Unlock()
	if entry, ok := a.breakerCache[ownerUserID]; ok && now.Before(entry.expires) {
		return entry.config
	}
	userCfg, found, err := a.health.GetUserBreakerConfigRow(ctx, ownerUserID)
	if err != nil {
		return a.breaker
	}
	resolved := a.breaker
	if found {
		resolved = ResolveChannelBreakerConfig(a.breaker, &userCfg)
	}
	a.breakerCache[ownerUserID] = breakerConfigCacheEntry{config: resolved, expires: now.Add(breakerConfigCacheTTL)}
	return resolved
}

func (a *Server) breakerFor(ctx context.Context, channelID int) ChannelBreakerConfig {
	ownerUserID, err := a.store.GetChannelOwner(ctx, channelID)
	if err != nil {
		return a.breaker
	}
	return a.baseBreakerFor(ctx, ownerUserID)
}

func (a *Server) invalidateBreakerConfig(ownerUserID int) {
	a.breakerMu.Lock()
	delete(a.breakerCache, ownerUserID)
	a.breakerMu.Unlock()
}

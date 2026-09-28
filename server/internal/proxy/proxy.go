package proxy

import (
	"context"
	"errors"
	"net/http"
	"time"

	"LLMGateway/server/internal/money"
	settlement "LLMGateway/server/internal/proxy/settlement"
)

const (
	// settleTimeout bounds the settlement transaction. Settlement must survive
	// a canceled downstream request, so it runs detached from the request
	// context but still needs an upper bound to avoid hanging on locks.
	settleTimeout = 5 * time.Second
	// bestEffortTimeout bounds detached bookkeeping (usage log, channel health,
	// key last-used) whose failure must not change the response.
	bestEffortTimeout = 5 * time.Second
)

var (
	ErrUnauthorized     = errors.New("unauthorized")
	ErrForbidden        = errors.New("forbidden")
	ErrInvalidRequest   = errors.New("invalid request")
	ErrRateLimited      = errors.New("rate limit exceeded")
	ErrNoHealthyChannel = errors.New("no healthy channel available")
	ErrUpstream         = errors.New("upstream error")
	ErrInvalidStream    = errors.New("invalid upstream stream")
	ErrQuotaExceeded    = errors.New("period quota exceeded")
)

type Service struct {
	store            Port
	catalog          Catalog
	quota            Quota
	ratelimit        RateLimit
	settleTx         settlement.TxManager
	client           *http.Client
	randIntN         func(int) int
	now              func() time.Time
	adapter          ProtocolAdapter
	minRouteBalance  money.Amount
	defaultMaxTokens int
	reservationTTL   time.Duration
	requestTimeout   time.Duration
	maxAttempts      int
}

// Config is the named composition input for a Service. Production assembly uses
// NewServiceWithConfig; NewService keeps the positional form for compact test
// fixtures.
type Config struct {
	Store     Port
	Catalog   Catalog
	Quota     Quota
	RateLimit RateLimit
	Client    *http.Client
	RandIntN  func(int) int
	Now       func() time.Time
	Adapter   ProtocolAdapter
}

func NewService(st Port, catalog Catalog, quota Quota, rateLimit RateLimit, client *http.Client, randIntN func(int) int, now func() time.Time, adapters ...ProtocolAdapter) *Service {
	adapter := ProtocolAdapter{}
	if len(adapters) > 0 {
		adapter = adapters[0]
	}
	return NewServiceWithConfig(Config{Store: st, Catalog: catalog, Quota: quota, RateLimit: rateLimit, Client: client, RandIntN: randIntN, Now: now, Adapter: adapter})
}

func NewServiceWithConfig(cfg Config) *Service {
	return &Service{store: cfg.Store, catalog: cfg.Catalog, quota: cfg.Quota, ratelimit: cfg.RateLimit, settleTx: cfg.Store.SettlementTx(), client: cfg.Client, randIntN: cfg.RandIntN, now: cfg.Now, adapter: cfg.Adapter, defaultMaxTokens: 4096, reservationTTL: 2 * time.Minute, requestTimeout: 60 * time.Second, maxAttempts: 3}
}

func (a *Service) ConfigureRequest(timeout time.Duration, maxAttempts int) {
	if timeout > 0 {
		a.requestTimeout = timeout
	}
	if maxAttempts > 0 {
		a.maxAttempts = maxAttempts
	}
}

// ConfigureMinimumRouteBalance excludes chargeable channels below the global
// reserve amount while leaving channels without a balance limit eligible.
func (a *Service) ConfigureMinimumRouteBalance(balance string) {
	amount, err := money.Parse6(balance)
	if err == nil && amount.Cmp(0) >= 0 {
		a.minRouteBalance = amount
	}
}

func (a *Service) ConfigureQuota(defaultMaxTokens int, reservationTTL time.Duration) {
	if defaultMaxTokens > 0 {
		a.defaultMaxTokens = defaultMaxTokens
	}
	if reservationTTL > 0 {
		a.reservationTTL = reservationTTL
	}
}

// detachedCtx returns a time-limited context that does not inherit the parent's
// cancellation. It is used for settlement and best-effort bookkeeping that must
// complete even when the downstream request is canceled.
func detachedCtx(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}

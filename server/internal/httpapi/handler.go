package httpapi

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/proxy"
	openaiwire "LLMGateway/server/internal/proxy/openai"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/usage"
)

type adminResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type Server struct {
	store     Port
	client    *http.Client
	proxy     *proxy.Service
	catalog   *catalog.Server
	accounts  *accounts.Server
	usage     *usage.Server
	ratelimit *ratelimit.Server
	quota     *quota.Server
	adminMux  *http.ServeMux
}

// Port is the process assembly contract. Each business server receives its
// own narrower module port during construction.
//
// Transaction managers are exposed as per-module factory methods because a
// single store cannot implement several InTx methods that differ only in the
// callback signature.
type Port interface {
	accounts.Port
	catalog.Port
	catalog.HealthPort
	quota.Port
	ratelimit.Port
	usage.Port
	proxy.Port
	AccountsTx() accounts.TxManager
	CatalogTx() catalog.TxManager
	QuotaTx() quota.TxManager
}

type options struct {
	upstreamTimeout       time.Duration
	randIntN              func(int) int
	now                   func() time.Time
	quotaDefaultMaxTokens int
	quotaReservationTTL   time.Duration
	upstreamMaxAttempts   int
	minimumRouteBalance   string
	cipher                *crypto.Cipher
	breaker               catalog.ChannelBreakerConfig

	sessionConfigured   bool
	sessionTTL          time.Duration
	sessionCookieSecure bool
	registrationEnabled bool
	bcryptCost          int
}

// WithSessionConfig injects the auth/session settings resolved by config.Load.
// Without it the accounts module falls back to its built-in defaults, which
// keeps unit tests free of session wiring.
func WithSessionConfig(ttl time.Duration, cookieSecure, registrationEnabled bool, bcryptCost int) Option {
	return func(o *options) {
		o.sessionConfigured = true
		o.sessionTTL = ttl
		o.sessionCookieSecure = cookieSecure
		o.registrationEnabled = registrationEnabled
		o.bcryptCost = bcryptCost
	}
}

func WithQuotaConfig(defaultMaxTokens int, reservationTTL time.Duration) Option {
	return func(o *options) {
		o.quotaDefaultMaxTokens = defaultMaxTokens
		o.quotaReservationTTL = reservationTTL
	}
}

// Option customizes handler assembly.
type Option func(*options)

// WithUpstreamTimeout sets the HTTP client timeout for downstream proxy calls.
// The default is intentionally longer than the admin timeout.
func WithUpstreamTimeout(timeout time.Duration) Option {
	return func(o *options) {
		if timeout > 0 {
			o.upstreamTimeout = timeout
		}
	}
}

func WithUpstreamMaxAttempts(attempts int) Option {
	return func(o *options) {
		if attempts > 0 {
			o.upstreamMaxAttempts = attempts
		}
	}
}

// WithMinimumRouteBalance sets the global reserve below which chargeable
// channels are excluded from routing.
func WithMinimumRouteBalance(balance string) Option {
	return func(o *options) {
		o.minimumRouteBalance = balance
	}
}

// WithClock injects the clock used for key expiry and rate limit windows.
func WithClock(fn func() time.Time) Option {
	return func(o *options) {
		if fn != nil {
			o.now = fn
		}
	}
}

// WithCipher injects the cipher used to encrypt/decrypt upstream channel keys.
func WithCipher(cipher *crypto.Cipher) Option {
	return func(o *options) {
		o.cipher = cipher
	}
}

// WithChannelBreakerConfig sets the global channel breaker defaults. Per-channel
// overrides in channel_breaker_configs take precedence over these values.
func WithChannelBreakerConfig(breaker catalog.ChannelBreakerConfig) Option {
	return func(o *options) {
		o.breaker = breaker
	}
}

// NewServer builds the HTTP entry points around an injected store. Route
// registration is done by cmd/llmgateway/router.go.
func NewServer(st Port, opts ...Option) *Server {
	settings := options{
		upstreamTimeout:       60 * time.Second,
		randIntN:              rand.Intn,
		now:                   time.Now,
		quotaDefaultMaxTokens: 4096,
		quotaReservationTTL:   2 * time.Minute,
		upstreamMaxAttempts:   3,
	}
	for _, opt := range opts {
		opt(&settings)
	}
	client := &http.Client{Timeout: settings.upstreamTimeout}
	catalogServer := catalog.New(catalog.Deps{
		Store:   st,
		Health:  st,
		Tx:      st.CatalogTx(),
		Cipher:  settings.cipher,
		Client:  client,
		Now:     settings.now,
		Breaker: settings.breaker,
	})
	quotaServer := quota.New(st, st.QuotaTx(), settings.now)
	ratelimitServer := ratelimit.New(st, settings.now)
	proxyService := proxy.NewService(st, catalogServer, quotaServer, ratelimitServer, client, settings.randIntN, settings.now, openaiwire.Adapter())
	proxyService.ConfigureQuota(settings.quotaDefaultMaxTokens, settings.quotaReservationTTL)
	proxyService.ConfigureRequest(settings.upstreamTimeout, settings.upstreamMaxAttempts)
	proxyService.ConfigureMinimumRouteBalance(settings.minimumRouteBalance)
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"code": http.StatusNotFound, "message": "not found", "data": map[string]any{}})
	})
	catalogServer.RegisterAdminRoutes(adminMux)
	accountsOpts := []accounts.Option{accounts.WithClock(settings.now)}
	if settings.sessionConfigured {
		accountsOpts = append(accountsOpts, accounts.WithAuthConfig(accounts.AuthConfig{
			SessionTTL:       settings.sessionTTL,
			CookieSecure:     settings.sessionCookieSecure,
			RegistrationOpen: settings.registrationEnabled,
			BcryptCost:       settings.bcryptCost,
		}))
	}
	accountsServer := accounts.New(st, st.AccountsTx(), accountsOpts...)
	accountsServer.RegisterAdminRoutes(adminMux)
	ratelimitServer.RegisterAdminRoutes(adminMux)
	quotaServer.RegisterAdminRoutes(adminMux)
	usageServer := usage.New(st)
	usageServer.RegisterAdminRoutes(adminMux)
	return &Server{
		store:     st,
		client:    client,
		proxy:     proxyService,
		catalog:   catalogServer,
		accounts:  accountsServer,
		usage:     usageServer,
		ratelimit: ratelimitServer,
		quota:     quotaServer,
		adminMux:  adminMux,
	}
}

// Healthz reports service health.
func (a *Server) Healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReapChannelHealthBuckets prunes stale channel attempt buckets using the
// catalog retention policy. It is called from the process background workers.
func (a *Server) ReapChannelHealthBuckets(ctx context.Context, retention time.Duration) (int, error) {
	return a.catalog.ReapChannelHealthBuckets(ctx, retention)
}

// ReapExpiredSessions deletes expired login sessions. It is called from the
// process background workers on a best-effort basis.
func (a *Server) ReapExpiredSessions(ctx context.Context, limit int) (int, error) {
	return a.accounts.ReapExpiredSessions(ctx, limit)
}

// Admin handles the /admin management API.
func (a *Server) Admin(w http.ResponseWriter, r *http.Request) {
	writeCORSHeaders(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	a.adminMux.ServeHTTP(w, r)
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func writeCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

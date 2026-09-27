package accounts

import (
	"time"

	"LLMGateway/server/internal/crypto"
)

// AuthConfig carries the session and registration settings resolved by
// config.Load. When WithAuthConfig is not supplied, New falls back to safe
// defaults so unit tests can exercise the auth flow without wiring config.
type AuthConfig struct {
	SessionTTL       time.Duration
	CookieSecure     bool
	RegistrationOpen bool
	BcryptCost       int
}

// Option customizes the account server.
type Option func(*Server)

// WithAuthConfig injects the runtime auth settings.
func WithAuthConfig(cfg AuthConfig) Option {
	return func(s *Server) {
		s.auth = cfg
		s.authConfigured = true
	}
}

// WithClock injects the clock used for session expiry.
func WithClock(now func() time.Time) Option {
	return func(s *Server) {
		if now != nil {
			s.now = now
		}
	}
}

// Server owns account rules and orchestration over an injected Port and
// TxManager. Persistence primitives stay behind the port.
type Server struct {
	store          Port
	tx             TxManager
	now            func() time.Time
	auth           AuthConfig
	authConfigured bool
}

func New(st Port, tx TxManager, opts ...Option) *Server {
	s := &Server{store: st, tx: tx, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	defaults := defaultAuthConfig()
	if !s.authConfigured {
		s.auth = defaults
		return s
	}
	if s.auth.SessionTTL <= 0 {
		s.auth.SessionTTL = defaults.SessionTTL
	}
	if s.auth.BcryptCost <= 0 {
		s.auth.BcryptCost = defaults.BcryptCost
	}
	return s
}

func defaultAuthConfig() AuthConfig {
	return AuthConfig{
		SessionTTL:       7 * 24 * time.Hour,
		CookieSecure:     true,
		RegistrationOpen: true,
		BcryptCost:       crypto.DefaultPasswordCost,
	}
}

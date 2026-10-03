package config

import (
	"testing"
)

// loadOrFatal returns a Load result, failing the test on a parse error.
func loadOrFatal(t *testing.T) Config {
	t.Helper()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("ADDR", "")
	t.Setenv(EnvDatabaseURL, "")
	t.Setenv("MIGRATIONS_DIR", "")
	t.Setenv(EnvChannelKey, "")
	t.Setenv("UPSTREAM_TIMEOUT_SECONDS", "")
	t.Setenv(EnvUpstreamRequestTimeout, "")
	t.Setenv(EnvUpstreamMaxAttempts, "")
	t.Setenv("QUOTA_DEFAULT_MAX_TOKENS", "")
	t.Setenv("QUOTA_RESERVATION_TTL_SECONDS", "")
	t.Setenv("QUOTA_REAPER_INTERVAL_SECONDS", "")
	t.Setenv("QUOTA_REAPER_BATCH_SIZE", "")
	t.Setenv("CHANNEL_MIN_ROUTE_BALANCE", "")
	t.Setenv(EnvChannelBreakerFailureThreshold, "")
	t.Setenv(EnvChannelBreakerCooldownSeconds, "")
	t.Setenv(EnvChannelBreakerWindowSeconds, "")
	t.Setenv(EnvChannelBreakerMinimumSamples, "")
	t.Setenv(EnvChannelBreakerErrorRatePercent, "")
	t.Setenv(EnvChannelBreakerTimeoutRatePercent, "")
	t.Setenv(EnvChannelBreakerBucketRetentionSeconds, "")
	t.Setenv(EnvSessionTTLSeconds, "")
	t.Setenv(EnvSessionCookieSecure, "")
	t.Setenv(EnvRegistrationEnabled, "")
	t.Setenv(EnvBcryptCost, "")

	cfg := loadOrFatal(t)
	if cfg.Addr != ":8080" {
		t.Fatalf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("DatabaseURL = %q, want empty", cfg.DatabaseURL)
	}
	if cfg.MigrationsDir != "db/migrations" {
		t.Fatalf("MigrationsDir = %q, want db/migrations", cfg.MigrationsDir)
	}
	if cfg.ChannelKeyEncryptionKey != "" {
		t.Fatalf("ChannelKeyEncryptionKey = %q, want empty", cfg.ChannelKeyEncryptionKey)
	}
	if cfg.UpstreamTimeoutSeconds != 600 || cfg.UpstreamMaxAttempts != 3 || cfg.QuotaDefaultMaxTokens != 4096 || cfg.QuotaReservationTTLSeconds != 660 || cfg.QuotaReaperIntervalSeconds != 30 || cfg.QuotaReaperBatchSize != 100 || cfg.ChannelMinRouteBalance != "0.000000" {
		t.Fatalf("quota defaults = %+v", cfg)
	}
	if cfg.ChannelBreakerFailureThreshold != 5 || cfg.ChannelBreakerCooldownSeconds != 30 || cfg.ChannelBreakerWindowSeconds != 60 || cfg.ChannelBreakerMinimumSamples != 10 || cfg.ChannelBreakerErrorRatePercent != 50 || cfg.ChannelBreakerTimeoutRatePercent != 50 || cfg.ChannelBreakerBucketRetentionSeconds != 600 {
		t.Fatalf("breaker defaults = %+v", cfg)
	}
	if cfg.SessionTTLSeconds != 7*24*60*60 || !cfg.SessionCookieSecure || !cfg.RegistrationEnabled || cfg.BcryptCost != 10 {
		t.Fatalf("security defaults = %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("ADDR", ":9999")
	t.Setenv(EnvDatabaseURL, "postgres://user:pass@localhost:5432/db?sslmode=disable")
	t.Setenv("MIGRATIONS_DIR", "/srv/migrations")
	t.Setenv(EnvChannelKey, "0123456789abcdef0123456789abcdef")
	t.Setenv("UPSTREAM_TIMEOUT_SECONDS", "90")
	t.Setenv(EnvUpstreamRequestTimeout, "90")
	t.Setenv(EnvUpstreamMaxAttempts, "5")
	t.Setenv("QUOTA_DEFAULT_MAX_TOKENS", "8192")
	t.Setenv("QUOTA_RESERVATION_TTL_SECONDS", "180")
	t.Setenv("QUOTA_REAPER_INTERVAL_SECONDS", "15")
	t.Setenv("QUOTA_REAPER_BATCH_SIZE", "50")
	t.Setenv("CHANNEL_MIN_ROUTE_BALANCE", "2.5")
	t.Setenv(EnvChannelBreakerFailureThreshold, "9")
	t.Setenv(EnvChannelBreakerCooldownSeconds, "45")
	t.Setenv(EnvChannelBreakerWindowSeconds, "120")
	t.Setenv(EnvChannelBreakerMinimumSamples, "25")
	t.Setenv(EnvChannelBreakerErrorRatePercent, "80")
	t.Setenv(EnvChannelBreakerTimeoutRatePercent, "70")
	t.Setenv(EnvChannelBreakerBucketRetentionSeconds, "900")
	t.Setenv(EnvSessionTTLSeconds, "3600")
	t.Setenv(EnvSessionCookieSecure, "false")
	t.Setenv(EnvRegistrationEnabled, "false")
	t.Setenv(EnvBcryptCost, "12")

	cfg := loadOrFatal(t)
	if cfg.Addr != ":9999" {
		t.Fatalf("Addr = %q, want :9999", cfg.Addr)
	}
	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/db?sslmode=disable" {
		t.Fatalf("DatabaseURL = %q, want override", cfg.DatabaseURL)
	}
	if cfg.MigrationsDir != "/srv/migrations" {
		t.Fatalf("MigrationsDir = %q, want /srv/migrations", cfg.MigrationsDir)
	}
	if cfg.ChannelKeyEncryptionKey != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("ChannelKeyEncryptionKey = %q, want override", cfg.ChannelKeyEncryptionKey)
	}
	if cfg.UpstreamTimeoutSeconds != 90 || cfg.UpstreamMaxAttempts != 5 || cfg.QuotaDefaultMaxTokens != 8192 || cfg.QuotaReservationTTLSeconds != 180 || cfg.QuotaReaperIntervalSeconds != 15 || cfg.QuotaReaperBatchSize != 50 || cfg.ChannelMinRouteBalance != "2.500000" {
		t.Fatalf("quota overrides = %+v", cfg)
	}
	if cfg.ChannelBreakerFailureThreshold != 9 || cfg.ChannelBreakerCooldownSeconds != 45 || cfg.ChannelBreakerWindowSeconds != 120 || cfg.ChannelBreakerMinimumSamples != 25 || cfg.ChannelBreakerErrorRatePercent != 80 || cfg.ChannelBreakerTimeoutRatePercent != 70 || cfg.ChannelBreakerBucketRetentionSeconds != 900 {
		t.Fatalf("breaker overrides = %+v", cfg)
	}
	if cfg.SessionTTLSeconds != 3600 || cfg.SessionCookieSecure || cfg.RegistrationEnabled || cfg.BcryptCost != 12 {
		t.Fatalf("security overrides = %+v", cfg)
	}
}

func TestLoadRejectsInvalidSecurityValues(t *testing.T) {
	cases := map[string]string{
		EnvSessionTTLSeconds:   "0",
		EnvSessionCookieSecure: "maybe",
		EnvRegistrationEnabled: "sometimes",
		EnvBcryptCost:          "99",
	}
	for env, value := range cases {
		t.Run(env+"="+value, func(t *testing.T) {
			t.Setenv(env, value)
			if _, err := Load(); err == nil {
				t.Fatalf("Load should reject %s=%q", env, value)
			}
		})
	}
}

func TestLoadRejectsNonNumericSecurityValues(t *testing.T) {
	t.Setenv(EnvSessionTTLSeconds, "soon")
	if _, err := Load(); err == nil {
		t.Fatal("Load should reject non-numeric SESSION_TTL_SECONDS")
	}

	t.Setenv(EnvSessionTTLSeconds, "")
	t.Setenv(EnvBcryptCost, "cheap")
	if _, err := Load(); err == nil {
		t.Fatal("Load should reject non-numeric BCRYPT_COST")
	}
}

func TestBreakerPercentRejectsOutOfRange(t *testing.T) {
	t.Setenv(EnvChannelBreakerErrorRatePercent, "150")
	t.Setenv(EnvChannelBreakerTimeoutRatePercent, "0")
	if cfg := loadOrFatal(t); cfg.ChannelBreakerErrorRatePercent != 50 || cfg.ChannelBreakerTimeoutRatePercent != 50 {
		t.Fatalf("percent clamps = %d/%d, want 50/50", cfg.ChannelBreakerErrorRatePercent, cfg.ChannelBreakerTimeoutRatePercent)
	}
}

func TestLoadMinimumRouteBalanceRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"-1", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("CHANNEL_MIN_ROUTE_BALANCE", value)
			if got := loadOrFatal(t).ChannelMinRouteBalance; got != "0.000000" {
				t.Fatalf("ChannelMinRouteBalance = %q, want 0.000000", got)
			}
		})
	}
}

func TestQuotaReservationTTLExceedsUpstreamTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		legacy  string
		ttl     string
		want    int
	}{
		{"default timeout with short TTL", "", "", "60", 660},
		{"legacy timeout", "", "90", "60", 150},
		{"custom timeout", "900", "90", "120", 960},
		{"insufficient safety margin", "90", "", "149", 150},
		{"exact safety margin", "90", "", "150", 150},
		{"longer TTL", "90", "", "180", 180},
		{"automatic TTL", "900", "", "", 960},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvUpstreamRequestTimeout, tt.timeout)
			t.Setenv("UPSTREAM_TIMEOUT_SECONDS", tt.legacy)
			t.Setenv("QUOTA_RESERVATION_TTL_SECONDS", tt.ttl)
			cfg := loadOrFatal(t)
			if cfg.QuotaReservationTTLSeconds != tt.want {
				t.Fatalf("QuotaReservationTTLSeconds = %d, want %d", cfg.QuotaReservationTTLSeconds, tt.want)
			}
		})
	}
}

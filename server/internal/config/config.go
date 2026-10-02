package config

import (
	"fmt"
	"os"
	"strconv"

	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/money"
)

const EnvDatabaseURL = "DATABASE_URL"

// EnvChannelKey is the environment variable holding the channel api_key
// encryption key (raw bytes; 16, 24 or 32 bytes for AES-128/192/256). The
// configuration layer owns env parsing; crypto validates the key material.
const EnvChannelKey = "CHANNEL_KEY_ENCRYPTION_KEY"

// Session and registration security settings.
const (
	EnvSessionTTLSeconds   = "SESSION_TTL_SECONDS"
	EnvSessionCookieSecure = "SESSION_COOKIE_SECURE"
	EnvRegistrationEnabled = "REGISTRATION_ENABLED"
	EnvBcryptCost          = "BCRYPT_COST"
)

const (
	EnvUpstreamRequestTimeout = "UPSTREAM_REQUEST_TIMEOUT"
	EnvUpstreamMaxAttempts    = "UPSTREAM_MAX_ATTEMPTS"
)

const (
	EnvChannelBreakerFailureThreshold       = "CHANNEL_BREAKER_FAILURE_THRESHOLD"
	EnvChannelBreakerCooldownSeconds        = "CHANNEL_BREAKER_COOLDOWN_SECONDS"
	EnvChannelBreakerWindowSeconds          = "CHANNEL_BREAKER_WINDOW_SECONDS"
	EnvChannelBreakerMinimumSamples         = "CHANNEL_BREAKER_MINIMUM_SAMPLES"
	EnvChannelBreakerErrorRatePercent       = "CHANNEL_BREAKER_ERROR_RATE_PERCENT"
	EnvChannelBreakerTimeoutRatePercent     = "CHANNEL_BREAKER_TIMEOUT_RATE_PERCENT"
	EnvChannelBreakerBucketRetentionSeconds = "CHANNEL_BREAKER_BUCKET_RETENTION_SECONDS"
)

type Config struct {
	Addr          string
	DatabaseURL   string
	MigrationsDir string
	// ChannelKeyEncryptionKey is the raw AES key used to encrypt upstream
	// channel api keys. It is required for every runtime configuration.
	ChannelKeyEncryptionKey string
	// UpstreamTimeoutSeconds bounds a downstream proxy call to the upstream
	// provider. Chat completions need far more than the default admin timeout.
	UpstreamTimeoutSeconds     int
	UpstreamMaxAttempts        int
	QuotaDefaultMaxTokens      int
	QuotaReservationTTLSeconds int
	QuotaReaperIntervalSeconds int
	QuotaReaperBatchSize       int
	ChannelMinRouteBalance     string

	// Session and account security settings. SessionTTLSeconds bounds how long
	// a login session stays valid; SessionCookieSecure controls the cookie
	// Secure flag (set false only for plain-HTTP local deployments).
	SessionTTLSeconds   int
	SessionCookieSecure bool
	RegistrationEnabled bool
	BcryptCost          int

	ChannelBreakerFailureThreshold       int
	ChannelBreakerCooldownSeconds        int
	ChannelBreakerWindowSeconds          int
	ChannelBreakerMinimumSamples         int
	ChannelBreakerErrorRatePercent       int
	ChannelBreakerTimeoutRatePercent     int
	ChannelBreakerBucketRetentionSeconds int
}

// Load builds the runtime configuration. Legacy settings keep their best-effort
// fallback behavior, but security-critical settings (session/registration) fail
// fast: an unparsable value returns an error instead of silently defaulting.
func Load() (Config, error) {
	cfg := Config{
		Addr:                    os.Getenv("ADDR"),
		DatabaseURL:             os.Getenv(EnvDatabaseURL),
		MigrationsDir:           os.Getenv("MIGRATIONS_DIR"),
		ChannelKeyEncryptionKey: os.Getenv(EnvChannelKey),
	}
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.MigrationsDir == "" {
		cfg.MigrationsDir = "db/migrations"
	}
	cfg.UpstreamTimeoutSeconds = parsePositiveInt(os.Getenv(EnvUpstreamRequestTimeout), parsePositiveInt(os.Getenv("UPSTREAM_TIMEOUT_SECONDS"), 60))
	cfg.UpstreamMaxAttempts = parsePositiveInt(os.Getenv(EnvUpstreamMaxAttempts), 3)
	cfg.QuotaDefaultMaxTokens = parsePositiveInt(os.Getenv("QUOTA_DEFAULT_MAX_TOKENS"), 4096)
	cfg.QuotaReservationTTLSeconds = parsePositiveInt(os.Getenv("QUOTA_RESERVATION_TTL_SECONDS"), cfg.UpstreamTimeoutSeconds+60)
	if cfg.QuotaReservationTTLSeconds <= cfg.UpstreamTimeoutSeconds {
		cfg.QuotaReservationTTLSeconds = cfg.UpstreamTimeoutSeconds + 60
	}
	cfg.QuotaReaperIntervalSeconds = parsePositiveInt(os.Getenv("QUOTA_REAPER_INTERVAL_SECONDS"), 30)
	cfg.QuotaReaperBatchSize = parsePositiveInt(os.Getenv("QUOTA_REAPER_BATCH_SIZE"), 100)
	cfg.ChannelMinRouteBalance = parseNonNegativeAmount(os.Getenv("CHANNEL_MIN_ROUTE_BALANCE"), "0.000000")
	cfg.ChannelBreakerFailureThreshold = parsePositiveInt(os.Getenv(EnvChannelBreakerFailureThreshold), 5)
	cfg.ChannelBreakerCooldownSeconds = parsePositiveInt(os.Getenv(EnvChannelBreakerCooldownSeconds), 30)
	cfg.ChannelBreakerWindowSeconds = parsePositiveInt(os.Getenv(EnvChannelBreakerWindowSeconds), 60)
	cfg.ChannelBreakerMinimumSamples = parsePositiveInt(os.Getenv(EnvChannelBreakerMinimumSamples), 10)
	cfg.ChannelBreakerErrorRatePercent = parsePercent(os.Getenv(EnvChannelBreakerErrorRatePercent), 50)
	cfg.ChannelBreakerTimeoutRatePercent = parsePercent(os.Getenv(EnvChannelBreakerTimeoutRatePercent), 50)
	cfg.ChannelBreakerBucketRetentionSeconds = parsePositiveInt(os.Getenv(EnvChannelBreakerBucketRetentionSeconds), 600)

	var err error
	cfg.SessionTTLSeconds, err = parsePositiveIntStrict(os.Getenv(EnvSessionTTLSeconds), 7*24*60*60)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", EnvSessionTTLSeconds, err)
	}
	cfg.SessionCookieSecure, err = parseBoolStrict(os.Getenv(EnvSessionCookieSecure), true)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", EnvSessionCookieSecure, err)
	}
	cfg.RegistrationEnabled, err = parseBoolStrict(os.Getenv(EnvRegistrationEnabled), true)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", EnvRegistrationEnabled, err)
	}
	cfg.BcryptCost, err = parseBcryptCost(os.Getenv(EnvBcryptCost))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", EnvBcryptCost, err)
	}
	return cfg, nil
}

// parsePositiveIntStrict parses a positive integer, returning fallback for an
// empty value and an error for any other unparsable or non-positive input.
func parsePositiveIntStrict(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("must be a positive integer, got %q", value)
	}
	return parsed, nil
}

// parseBoolStrict parses a boolean, returning fallback for an empty value and
// an error for any other value strconv.ParseBool rejects.
func parseBoolStrict(value string, fallback bool) (bool, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("must be a boolean, got %q", value)
	}
	return parsed, nil
}

// parseBcryptCost parses the optional bcrypt cost, defaulting to the library
// default and rejecting values outside the bcrypt-supported range.
func parseBcryptCost(value string) (int, error) {
	if value == "" {
		return crypto.DefaultPasswordCost, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("must be an integer, got %q", value)
	}
	if parsed < crypto.MinPasswordCost || parsed > crypto.MaxPasswordCost {
		return 0, fmt.Errorf("must be within [%d,%d], got %d", crypto.MinPasswordCost, crypto.MaxPasswordCost, parsed)
	}
	return parsed, nil
}

func parsePercent(value string, fallback int) int {
	parsed := parsePositiveInt(value, fallback)
	if parsed > 100 {
		return fallback
	}
	return parsed
}

func parsePositiveInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parseNonNegativeAmount(value, fallback string) string {
	amount, err := money.Parse6(value)
	if value == "" || err != nil || amount.Cmp(0) < 0 {
		return fallback
	}
	return money.Format6(amount)
}

package catalog

import "time"

// HealthState is the circuit breaker state of an upstream channel.
type HealthState string

const (
	HealthClosed   HealthState = "closed"
	HealthOpen     HealthState = "open"
	HealthHalfOpen HealthState = "half-open"
)

// ChannelHealth is the persisted circuit breaker state for a channel. A missing
// row means "closed".
type ChannelHealth struct {
	ChannelID           int
	State               HealthState
	ConsecutiveFailures int
	SuccessCount        int64
	FailureCount        int64
	OpenedAt            *string
	UpdatedAt           string
}

type ChannelHealthDTO struct {
	ChannelID           int     `json:"channel_id"`
	State               string  `json:"state"`
	ConsecutiveFailures int     `json:"consecutive_failures"`
	SuccessCount        int64   `json:"success_count"`
	FailureCount        int64   `json:"failure_count"`
	OpenedAt            *string `json:"opened_at"`
	UpdatedAt           string  `json:"updated_at"`
}

// ChannelBreakerConfig holds the circuit breaker thresholds shared by the
// persistence implementations and test fakes.
type ChannelBreakerConfig struct {
	FailureThreshold   int
	Cooldown           time.Duration
	WindowSeconds      int
	MinimumSamples     int
	ErrorRatePercent   int
	TimeoutRatePercent int
}

func DefaultChannelBreakerConfig() ChannelBreakerConfig {
	return ChannelBreakerConfig{FailureThreshold: 5, Cooldown: 30 * time.Second, WindowSeconds: 60, MinimumSamples: 10, ErrorRatePercent: 50, TimeoutRatePercent: 50}
}

type ChannelHealthWindow struct{ Requests, Errors, Timeouts int64 }

// channelHealthBucketSeconds is the fixed width of channel_health_buckets rows.
// Fixed buckets let the window query sum whole rows without per-request
// timestamp alignment and keep cleanup cheap.
const channelHealthBucketSeconds = 10

// ChannelHealthBucketStart aligns t to the channel health bucket grid in UTC.
func ChannelHealthBucketStart(t time.Time) time.Time {
	return t.UTC().Truncate(channelHealthBucketSeconds * time.Second)
}

func ShouldOpenChannelBreaker(window ChannelHealthWindow, cfg ChannelBreakerConfig) bool {
	if window.Requests < int64(cfg.MinimumSamples) || window.Requests <= 0 {
		return false
	}
	return (cfg.ErrorRatePercent > 0 && window.Errors*100 >= window.Requests*int64(cfg.ErrorRatePercent)) || (cfg.TimeoutRatePercent > 0 && window.Timeouts*100 >= window.Requests*int64(cfg.TimeoutRatePercent))
}

func ResolveChannelBreakerConfig(base ChannelBreakerConfig, override *ChannelBreakerConfig) ChannelBreakerConfig {
	if override == nil {
		return base
	}
	resolved := base
	if override.FailureThreshold > 0 {
		resolved.FailureThreshold = override.FailureThreshold
	}
	if override.Cooldown > 0 {
		resolved.Cooldown = override.Cooldown
	}
	if override.WindowSeconds > 0 {
		resolved.WindowSeconds = override.WindowSeconds
	}
	if override.MinimumSamples > 0 {
		resolved.MinimumSamples = override.MinimumSamples
	}
	if override.ErrorRatePercent > 0 {
		resolved.ErrorRatePercent = override.ErrorRatePercent
	}
	if override.TimeoutRatePercent > 0 {
		resolved.TimeoutRatePercent = override.TimeoutRatePercent
	}
	return resolved
}

// NewChannelHealth returns the default closed state for a channel.
func NewChannelHealth(channelID int) ChannelHealth {
	return ChannelHealth{ChannelID: channelID, State: HealthClosed}
}

// EvaluateChannelHealth lazily moves an open channel to half-open once the
// cooldown has elapsed. It is a pure function; callers decide whether to persist.
func EvaluateChannelHealth(current ChannelHealth, now time.Time, cfg ChannelBreakerConfig) ChannelHealth {
	if current.State != HealthOpen || current.OpenedAt == nil {
		return current
	}
	openedAt, err := time.Parse(time.RFC3339, *current.OpenedAt)
	if err != nil {
		return current
	}
	if now.Before(openedAt.Add(cfg.Cooldown)) {
		return current
	}
	current.State = HealthHalfOpen
	current.ConsecutiveFailures = 0
	current.OpenedAt = nil
	return current
}

// ApplyChannelSuccess records a successful attempt; any state returns to closed.
func ApplyChannelSuccess(current ChannelHealth, now time.Time) ChannelHealth {
	current.SuccessCount++
	current.ConsecutiveFailures = 0
	current.State = HealthClosed
	current.OpenedAt = nil
	current.UpdatedAt = now.UTC().Format(time.RFC3339)
	return current
}

// ApplyChannelFailure records a failed attempt and may trip the breaker. It
// opens on a deterministic failure, a half-open probe failure, a window
// error/timeout rate above threshold (when the window has enough samples), or
// the consecutive-failure threshold as a low-traffic fallback. The three
// judgements are a union so partial degradation and burst failures are both
// caught.
func ApplyChannelFailure(current ChannelHealth, reason FailureReason, window ChannelHealthWindow, now time.Time, cfg ChannelBreakerConfig) ChannelHealth {
	current.FailureCount++
	current.ConsecutiveFailures++
	current.UpdatedAt = now.UTC().Format(time.RFC3339)

	shouldOpen := reason.IsDeterministic() ||
		current.State == HealthHalfOpen ||
		ShouldOpenChannelBreaker(window, cfg) ||
		current.ConsecutiveFailures >= cfg.FailureThreshold
	if shouldOpen {
		current.State = HealthOpen
		opened := now.UTC().Format(time.RFC3339)
		current.OpenedAt = &opened
	}
	return current
}

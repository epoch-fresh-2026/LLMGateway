package ratelimit

import (
	"encoding/json"
	"strconv"
	"time"
)

// Overrides is the per-Key rate-limit override embedded in a gateway key's
// rate_limit_overrides JSON. Zero values mean "not overridden".
type Overrides struct {
	RPM, TPM, Concurrency int64
}

// ParseOverrides decodes raw gateway-key override JSON. Malformed input yields
// the zero Overrides so callers fall back to the matching rules.
func ParseOverrides(raw json.RawMessage) Overrides {
	var value struct {
		RPM, TPM, Concurrency int64
	}
	_ = json.Unmarshal(raw, &value)
	return Overrides{RPM: value.RPM, TPM: value.TPM, Concurrency: value.Concurrency}
}

// ForRule returns the override value that replaces a matching rule's limit.
// Only api_key rules can be overridden; user, model and channel rules keep
// their configured limit.
func (o Overrides) ForRule(rule RateLimitRuleDTO) (int64, bool) {
	if rule.TargetType != "api_key" {
		return 0, false
	}
	switch rule.Metric {
	case "rpm":
		return o.RPM, o.RPM > 0
	case "tpm":
		return o.TPM, o.TPM > 0
	case "concurrency":
		return o.Concurrency, o.Concurrency > 0
	}
	return 0, false
}

// MatchesTarget reports whether rule applies to the request identified by
// userID, keyID and model. Channel-scoped rules are excluded because the
// channel is not known before routing.
func MatchesTarget(rule RateLimitRuleDTO, userID, keyID int, model string) bool {
	switch rule.TargetType {
	case "user":
		return rule.TargetValue == "*" || rule.TargetValue == strconv.Itoa(userID)
	case "api_key":
		return rule.TargetValue == "*" || rule.TargetValue == strconv.Itoa(keyID)
	case "model":
		return rule.TargetValue == "*" || rule.TargetValue == model
	default:
		return false
	}
}

// SlidingWindowCount approximates a sliding-window count from the current and
// previous fixed buckets, weighting the previous bucket by how recently the
// current bucket started.
func SlidingWindowCount(previous, current, windowSeconds int64, now, bucketStart time.Time) int64 {
	if windowSeconds <= 0 {
		return current
	}
	elapsed := now.Sub(bucketStart).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > float64(windowSeconds) {
		elapsed = float64(windowSeconds)
	}
	return current + (previous*(windowSeconds-int64(elapsed)))/windowSeconds
}

// RequestWindowStart returns the start of the fixed bucket containing now.
func RequestWindowStart(now time.Time, windowSeconds int) time.Time {
	seconds := now.UTC().Unix()
	return time.Unix(seconds-(seconds%int64(windowSeconds)), 0).UTC()
}

// MetricSince returns the UTC lower bound of the trailing lookback window.
func MetricSince(now time.Time, windowSeconds int) time.Time {
	return now.UTC().Add(-time.Duration(windowSeconds) * time.Second)
}

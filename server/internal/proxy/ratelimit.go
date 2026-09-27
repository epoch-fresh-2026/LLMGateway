package proxy

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/usage"
)

type rateLimitOverrides struct {
	RPM, TPM, RPD, Concurrency int64
	RPMWindowSeconds           int
}

func parseRateLimitOverrides(raw json.RawMessage) rateLimitOverrides {
	var value struct {
		RPM, TPM, RPD, Concurrency int64
		RPMWindowSeconds           int `json:"rpm_window_seconds"`
	}
	_ = json.Unmarshal(raw, &value)
	return rateLimitOverrides{RPM: value.RPM, TPM: value.TPM, RPD: value.RPD, Concurrency: value.Concurrency, RPMWindowSeconds: value.RPMWindowSeconds}
}

func applicableOverride(overrides rateLimitOverrides, rule ratelimit.RateLimitRuleDTO) (int64, bool) {
	if rule.TargetType != "api_key" {
		return 0, false
	}
	switch rule.Metric {
	case "rpm":
		return overrides.RPM, overrides.RPM > 0
	case "tpm":
		return overrides.TPM, overrides.TPM > 0
	case "rpd":
		return overrides.RPD, overrides.RPD > 0
	case "concurrency":
		return overrides.Concurrency, overrides.Concurrency > 0
	}
	return 0, false
}

func slidingWindowCount(previous, current, windowSeconds int64, now, bucketStart time.Time) int64 {
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

func requestWindowStart(now time.Time, windowSeconds int) time.Time {
	seconds := now.UTC().Unix()
	return time.Unix(seconds-(seconds%int64(windowSeconds)), 0).UTC()
}

// checkRateLimit enforces enabled rate-limit rules with the reject action.
//
// A key's rate_limit_overrides.rpm takes precedence over matching rules.
func (a *Service) checkRateLimit(ctx context.Context, auth *accounts.AuthContext, model string, estimatedTokens *int64) error {
	apiKeyOverrides := parseRateLimitOverrides(auth.RateLimitOverrides)

	if override := apiKeyOverrides.RPM; override > 0 {
		window := apiKeyOverrides.RPMWindowSeconds
		if window <= 0 {
			window = 60
		}
		since := a.now().Add(-time.Duration(window) * time.Second).UTC().Format(time.RFC3339)
		count, err := a.store.CountRequestsSince(ctx, usage.UsageCountFilter{APIKeyID: &auth.KeyID, Since: since})
		if err != nil {
			return err
		}
		if slidingWindowCount(0, int64(count), int64(window), a.now(), requestWindowStart(a.now(), window)) >= override {
			return ErrRateLimited
		}
	}

	enabled := true
	result, err := a.ratelimit.ListRateLimits(ctx, auth.UserID, &enabled, 1, 1000)
	if err != nil {
		return err
	}
	for _, item := range result.List {
		if item.Action != "reject" {
			continue
		}
		if item.TargetType == "channel" {
			continue
		}
		if !matchesTarget(item, auth, model) {
			continue
		}
		limit := item.LimitValue
		if limit <= 0 {
			continue
		}
		filter := usage.UsageCountFilter{Since: metricSince(a.now(), item.Metric, item.WindowSeconds)}
		switch item.TargetType {
		case "user":
			filter.UserID = auth.UserID
		case "api_key":
			filter.APIKeyID = &auth.KeyID
		case "model":
			filter.UserID = auth.UserID
			filter.APIKeyID = &auth.KeyID
		}
		if item.TargetType == "model" {
			filter.Model = model
		}
		if override, ok := applicableOverride(parseRateLimitOverrides(auth.RateLimitOverrides), item); ok {
			limit = override
		}
		var count int64
		if item.Metric == "rpm" || item.Metric == "rpd" {
			since := metricSince(a.now(), item.Metric, item.WindowSeconds)
			filter.Since = since
			current, countErr := a.store.CountRequestsSince(ctx, filter)
			if countErr != nil {
				return countErr
			}
			count = slidingWindowCount(0, int64(current), int64(item.WindowSeconds), a.now(), requestWindowStart(a.now(), item.WindowSeconds))
		} else if item.Metric == "tpm" {
			if estimatedTokens == nil {
				return ErrRateLimited
			}
			since := metricSince(a.now(), item.Metric, item.WindowSeconds)
			tokenFilter := usage.TokenCountFilter{APIKeyID: filter.APIKeyID, Model: filter.Model, Since: since}
			tokenFilter.UserID = filter.UserID
			tokenCount, countErr := a.store.CountTokensSince(ctx, tokenFilter)
			if countErr != nil {
				return countErr
			}
			count = tokenCount + *estimatedTokens
		} else if item.Metric == "concurrency" {
			current, countErr := a.ratelimit.CountActiveRateLimitReservations(ctx, auth.UserID, &auth.KeyID, filter.Model, nil)
			if countErr != nil {
				return countErr
			}
			count = current + 1
		} else {
			continue
		}
		if count >= limit {
			return ErrRateLimited
		}
	}
	return nil
}

func metricSince(now time.Time, metric string, windowSeconds int) string {
	now = now.UTC()
	if metric == "rpd" {
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	return now.Add(-time.Duration(windowSeconds) * time.Second).Format(time.RFC3339)
}

func (a *Service) checkChannelRateLimit(ctx context.Context, auth *accounts.AuthContext, model string, channelID int, estimatedTokens int64) error {
	enabled := true
	result, err := a.ratelimit.ListRateLimits(ctx, auth.UserID, &enabled, 1, 1000)
	if err != nil {
		return err
	}
	for _, item := range result.List {
		if item.Action != "reject" || item.TargetType != "channel" {
			continue
		}
		if item.TargetValue != "*" && item.TargetValue != strconv.Itoa(channelID) {
			continue
		}
		if item.LimitValue <= 0 {
			continue
		}
		var count int64
		var err error
		if item.Metric == "rpm" || item.Metric == "rpd" {
			current, countErr := a.store.CountRequestsSince(ctx, usage.UsageCountFilter{Since: metricSince(a.now(), item.Metric, item.WindowSeconds), ChannelID: &channelID})
			err = countErr
			count = slidingWindowCount(0, int64(current), int64(item.WindowSeconds), a.now(), requestWindowStart(a.now(), item.WindowSeconds))
		} else if item.Metric == "tpm" {
			current, countErr := a.store.CountTokensSince(ctx, usage.TokenCountFilter{Since: metricSince(a.now(), item.Metric, item.WindowSeconds), Model: model, ChannelID: &channelID})
			err = countErr
			count = current + estimatedTokens
		} else if item.Metric == "concurrency" {
			current, countErr := a.ratelimit.CountActiveRateLimitReservations(ctx, auth.UserID, &auth.KeyID, model, &channelID)
			err = countErr
			count = current + 1
		} else {
			continue
		}
		if err != nil {
			return err
		}
		if count >= item.LimitValue {
			return ErrRateLimited
		}
	}
	return nil
}

// matchesTarget decides whether a rule applies to the current request.
func matchesTarget(rule ratelimit.RateLimitRuleDTO, auth *accounts.AuthContext, model string) bool {
	switch rule.TargetType {
	case "user":
		return rule.TargetValue == "*" || rule.TargetValue == strconv.Itoa(auth.UserID)
	case "api_key":
		return rule.TargetValue == "*" || rule.TargetValue == strconv.Itoa(auth.KeyID)
	case "model":
		return rule.TargetValue == "*" || rule.TargetValue == model
	default:
		// channel scoping is not known before routing and is not enforced here.
		return false
	}
}

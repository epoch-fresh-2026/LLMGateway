package proxy

import (
	"context"
	"strconv"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/usage"
)

// checkRateLimit enforces enabled rate-limit rules with the reject action.
//
// A key's rate_limit_overrides.rpm takes precedence over matching rules.
func (a *Service) checkRateLimit(ctx context.Context, auth *accounts.AuthContext, model string, estimatedTokens *int64) error {
	apiKeyOverrides := ratelimit.ParseOverrides(auth.RateLimitOverrides)

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
		if ratelimit.SlidingWindowCount(0, int64(count), int64(window), a.now(), ratelimit.RequestWindowStart(a.now(), window)) >= override {
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
		if !ratelimit.MatchesTarget(item, auth.UserID, auth.KeyID, model) {
			continue
		}
		limit := item.LimitValue
		if limit <= 0 {
			continue
		}
		filter := usage.UsageCountFilter{Since: ratelimit.MetricSince(a.now(), item.Metric, item.WindowSeconds).Format(time.RFC3339)}
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
		if override, ok := apiKeyOverrides.ForRule(item); ok {
			limit = override
		}
		var count int64
		if item.Metric == "rpm" || item.Metric == "rpd" {
			since := ratelimit.MetricSince(a.now(), item.Metric, item.WindowSeconds).Format(time.RFC3339)
			filter.Since = since
			current, countErr := a.store.CountRequestsSince(ctx, filter)
			if countErr != nil {
				return countErr
			}
			count = ratelimit.SlidingWindowCount(0, int64(current), int64(item.WindowSeconds), a.now(), ratelimit.RequestWindowStart(a.now(), item.WindowSeconds))
		} else if item.Metric == "tpm" {
			if estimatedTokens == nil {
				return ErrRateLimited
			}
			since := ratelimit.MetricSince(a.now(), item.Metric, item.WindowSeconds).Format(time.RFC3339)
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
			current, countErr := a.store.CountRequestsSince(ctx, usage.UsageCountFilter{Since: ratelimit.MetricSince(a.now(), item.Metric, item.WindowSeconds).Format(time.RFC3339), ChannelID: &channelID})
			err = countErr
			count = ratelimit.SlidingWindowCount(0, int64(current), int64(item.WindowSeconds), a.now(), ratelimit.RequestWindowStart(a.now(), item.WindowSeconds))
		} else if item.Metric == "tpm" {
			current, countErr := a.store.CountTokensSince(ctx, usage.TokenCountFilter{Since: ratelimit.MetricSince(a.now(), item.Metric, item.WindowSeconds).Format(time.RFC3339), Model: model, ChannelID: &channelID})
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

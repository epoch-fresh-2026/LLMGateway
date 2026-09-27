package ratelimit

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

import "LLMGateway/server/internal/pagination"

type ListResponse[T any] = pagination.List[T]

type RateLimitRule struct {
	ID            int
	RuleName      string
	TargetType    string
	TargetValue   string
	Metric        string
	LimitValue    int64
	WindowSeconds int
	Action        string
	Priority      int
	Enabled       bool
	Extras        json.RawMessage
}

type RateLimitRuleDTO struct {
	ID            int             `json:"id"`
	RuleName      string          `json:"rule_name"`
	TargetType    string          `json:"target_type"`
	TargetValue   string          `json:"target_value"`
	Metric        string          `json:"metric"`
	LimitValue    int64           `json:"limit_value"`
	WindowSeconds int             `json:"window_seconds"`
	Action        string          `json:"action"`
	Priority      int             `json:"priority"`
	Enabled       bool            `json:"enabled"`
	Extras        json.RawMessage `json:"extras"`
}

var (
	validTargetTypes = map[string]bool{"user": true, "api_key": true, "model": true, "channel": true}
	validMetrics     = map[string]bool{"rpm": true, "tpm": true, "rpd": true, "concurrency": true}
	validActions     = map[string]bool{"reject": true}
)

const MaxRateLimitWindowSeconds = 7 * 24 * 60 * 60

// NormalizeRateLimit merges a partial input over an existing rule (or defaults
// for a new rule) and validates the result. It is shared by persistence code
// and test fakes so both accept and reject the same values.
func NormalizeRateLimit(in RateLimitInput, existing *RateLimitRule) (RateLimitRule, error) {
	rule := RateLimitRule{TargetValue: "*", Priority: 100, Enabled: true, Extras: json.RawMessage(`{}`)}
	if existing != nil {
		rule = *existing
	}

	if in.RuleName != nil {
		rule.RuleName = *in.RuleName
	}
	if in.TargetType != nil {
		rule.TargetType = *in.TargetType
	}
	if in.TargetValue != nil {
		rule.TargetValue = *in.TargetValue
	}
	if in.Metric != nil {
		rule.Metric = *in.Metric
	}
	if in.LimitValue != nil {
		rule.LimitValue = *in.LimitValue
	}
	if in.WindowSeconds != nil {
		rule.WindowSeconds = *in.WindowSeconds
	}
	if in.Action != nil {
		rule.Action = *in.Action
	}
	if in.Priority != nil {
		rule.Priority = *in.Priority
	}
	if in.Enabled != nil {
		rule.Enabled = *in.Enabled
	}
	if len(in.Extras) > 0 {
		rule.Extras = in.Extras
	}

	if rule.TargetValue == "" {
		rule.TargetValue = "*"
	}
	if len(rule.Extras) == 0 {
		rule.Extras = json.RawMessage(`{}`)
	}

	switch {
	case strings.TrimSpace(rule.RuleName) == "":
		return rule, fmt.Errorf("%w: rule_name is required", ErrInvalid)
	case !validTargetTypes[rule.TargetType]:
		return rule, fmt.Errorf("%w: invalid target_type", ErrInvalid)
	case !validMetrics[rule.Metric]:
		return rule, fmt.Errorf("%w: invalid metric", ErrInvalid)
	case !validActions[rule.Action]:
		return rule, fmt.Errorf("%w: invalid action", ErrInvalid)
	case rule.LimitValue <= 0:
		return rule, fmt.Errorf("%w: limit_value must be positive", ErrInvalid)
	case rule.WindowSeconds <= 0:
		return rule, fmt.Errorf("%w: window_seconds must be positive", ErrInvalid)
	case rule.WindowSeconds > MaxRateLimitWindowSeconds:
		return rule, fmt.Errorf("%w: window_seconds exceeds maximum", ErrInvalid)
	}
	return rule, nil
}

// RateLimitInput uses pointers so a partial update (for example only
// {"enabled": false}) can be distinguished from omitted fields.
type RateLimitInput struct {
	RuleName      *string         `json:"rule_name"`
	TargetType    *string         `json:"target_type"`
	TargetValue   *string         `json:"target_value"`
	Metric        *string         `json:"metric"`
	LimitValue    *int64          `json:"limit_value"`
	WindowSeconds *int            `json:"window_seconds"`
	Action        *string         `json:"action"`
	Priority      *int            `json:"priority"`
	Enabled       *bool           `json:"enabled"`
	Extras        json.RawMessage `json:"extras"`
}

type RateLimitReservationInput struct {
	RequestID       string
	UserID          int
	APIKeyID        int
	Model           string
	ChannelID       *int
	EstimatedTokens int64
	ExpiresAt       time.Time
}

type RateLimitReservation struct {
	ID int64
}

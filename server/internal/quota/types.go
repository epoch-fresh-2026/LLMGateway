package quota

import (
	"fmt"
	"strings"
	"time"

	"LLMGateway/server/internal/money"
)

import "LLMGateway/server/internal/pagination"

type ListResponse[T any] = pagination.List[T]

type QuotaScopeType string
type QuotaPeriodType string

const (
	QuotaScopeUser   QuotaScopeType  = "user"
	QuotaScopeAPIKey QuotaScopeType  = "api_key"
	QuotaPeriodDay   QuotaPeriodType = "day"
	QuotaPeriodMonth QuotaPeriodType = "month"
)

type QuotaPolicy struct {
	ID         int
	PolicyName string
	ScopeType  QuotaScopeType
	ScopeID    int
	PeriodType QuotaPeriodType
	TokenLimit *int64
	CostLimit  *string
	Enabled    bool
}

type QuotaPolicyDTO struct {
	ID         int             `json:"id"`
	PolicyName string          `json:"policy_name"`
	ScopeType  QuotaScopeType  `json:"scope_type"`
	ScopeID    int             `json:"scope_id"`
	PeriodType QuotaPeriodType `json:"period_type"`
	TokenLimit *int64          `json:"token_limit"`
	CostLimit  *string         `json:"cost_limit"`
	Enabled    bool            `json:"enabled"`
}

type QuotaPolicyInput struct {
	PolicyName *string `json:"policy_name"`
	ScopeType  *string `json:"scope_type"`
	ScopeID    *int    `json:"scope_id"`
	PeriodType *string `json:"period_type"`
	TokenLimit *int64  `json:"token_limit"`
	CostLimit  *string `json:"cost_limit"`
	Enabled    *bool   `json:"enabled"`
}

type QuotaPolicyFilter struct {
	OwnerUserID int
	ScopeType   string
	ScopeID     int
	Enabled     *bool
	Page        int
	PageSize    int
}

type QuotaReserveInput struct {
	RequestID       string
	UserID          int
	APIKeyID        int
	Model           string
	EstimatedTokens int64
	EstimatedCost   string
	ExpiresAt       time.Time
}

type QuotaReservation struct {
	ID              int64
	RequestID       string
	EstimatedTokens int64
	EstimatedCost   string
}

type QuotaUsageDTO struct {
	PolicyID       int             `json:"policy_id"`
	PolicyName     string          `json:"policy_name"`
	ScopeType      QuotaScopeType  `json:"scope_type"`
	ScopeID        int             `json:"scope_id"`
	PeriodType     QuotaPeriodType `json:"period_type"`
	PeriodStart    string          `json:"period_start"`
	PeriodEnd      string          `json:"period_end"`
	TokenLimit     *int64          `json:"token_limit"`
	UsedTokens     int64           `json:"used_tokens"`
	ReservedTokens int64           `json:"reserved_tokens"`
	CostLimit      *string         `json:"cost_limit"`
	UsedCost       string          `json:"used_cost"`
	ReservedCost   string          `json:"reserved_cost"`
}

func QuotaPeriodBounds(now time.Time, period QuotaPeriodType) (time.Time, time.Time, error) {
	now = now.UTC()
	switch period {
	case QuotaPeriodDay:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 1), nil
	case QuotaPeriodMonth:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0), nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("%w: invalid quota period", ErrInvalid)
	}
}

func NormalizeQuotaPolicy(in QuotaPolicyInput, existing *QuotaPolicy) (QuotaPolicy, error) {
	policy := QuotaPolicy{Enabled: true}
	if existing != nil {
		policy = *existing
	}
	if in.PolicyName != nil {
		policy.PolicyName = strings.TrimSpace(*in.PolicyName)
	}
	if in.ScopeType != nil {
		policy.ScopeType = QuotaScopeType(*in.ScopeType)
	}
	if in.ScopeID != nil {
		policy.ScopeID = *in.ScopeID
	}
	if in.PeriodType != nil {
		policy.PeriodType = QuotaPeriodType(*in.PeriodType)
	}
	if in.TokenLimit != nil {
		value := *in.TokenLimit
		policy.TokenLimit = &value
	}
	if in.CostLimit != nil {
		value := *in.CostLimit
		policy.CostLimit = &value
	}
	if in.Enabled != nil {
		policy.Enabled = *in.Enabled
	}

	switch {
	case policy.PolicyName == "":
		return policy, fmt.Errorf("%w: policy_name is required", ErrInvalid)
	case policy.ScopeType != QuotaScopeUser && policy.ScopeType != QuotaScopeAPIKey:
		return policy, fmt.Errorf("%w: invalid scope_type", ErrInvalid)
	case policy.ScopeID <= 0:
		return policy, fmt.Errorf("%w: scope_id is required", ErrInvalid)
	case policy.PeriodType != QuotaPeriodDay && policy.PeriodType != QuotaPeriodMonth:
		return policy, fmt.Errorf("%w: invalid period_type", ErrInvalid)
	case policy.TokenLimit == nil && policy.CostLimit == nil:
		return policy, fmt.Errorf("%w: token_limit or cost_limit is required", ErrInvalid)
	case policy.TokenLimit != nil && *policy.TokenLimit <= 0:
		return policy, fmt.Errorf("%w: token_limit must be positive", ErrInvalid)
	}
	if policy.CostLimit != nil {
		parsed, err := money.Parse6(*policy.CostLimit)
		if err != nil || parsed.Cmp(0) <= 0 {
			return policy, fmt.Errorf("%w: cost_limit must be positive", ErrInvalid)
		}
		formatted := money.Format6(parsed)
		policy.CostLimit = &formatted
	}
	return policy, nil
}

func QuotaPolicyToDTO(policy QuotaPolicy) QuotaPolicyDTO {
	return QuotaPolicyDTO{ID: policy.ID, PolicyName: policy.PolicyName, ScopeType: policy.ScopeType, ScopeID: policy.ScopeID, PeriodType: policy.PeriodType, TokenLimit: policy.TokenLimit, CostLimit: policy.CostLimit, Enabled: policy.Enabled}
}

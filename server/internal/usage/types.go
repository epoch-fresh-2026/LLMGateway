package usage

import (
	"fmt"
	"time"
)

import "LLMGateway/server/internal/pagination"

type ListResponse[T any] = pagination.List[T]

// ValidateTimeRange checks optional RFC3339 start/end bounds. Invalid values
// return ErrInvalid so persistence code and test fakes behave identically.
func ValidateTimeRange(startTime, endTime string) error {
	if startTime != "" {
		if _, err := time.Parse(time.RFC3339, startTime); err != nil {
			return fmt.Errorf("%w: invalid start_time", ErrInvalid)
		}
	}
	if endTime != "" {
		if _, err := time.Parse(time.RFC3339, endTime); err != nil {
			return fmt.Errorf("%w: invalid end_time", ErrInvalid)
		}
	}
	return nil
}

// ValidateSince requires a non-empty RFC3339 timestamp. Used by window
// counting so persistence code and test fakes cannot diverge on an empty value.
func ValidateSince(since string) error {
	if since == "" {
		return fmt.Errorf("%w: since is required", ErrInvalid)
	}
	return ValidateTimeRange(since, "")
}

// ValidateDateRange checks optional YYYY-MM-DD bounds.
func ValidateDateRange(dateFrom, dateTo string) error {
	if dateFrom != "" {
		if _, err := time.Parse("2006-01-02", dateFrom); err != nil {
			return fmt.Errorf("%w: invalid date_from", ErrInvalid)
		}
	}
	if dateTo != "" {
		if _, err := time.Parse("2006-01-02", dateTo); err != nil {
			return fmt.Errorf("%w: invalid date_to", ErrInvalid)
		}
	}
	return nil
}

type UsageLog struct {
	ID                   int
	RequestID            string
	UserID               *int
	APIKeyID             *int
	ChannelID            *int
	ChannelName          string
	Model                string
	UpstreamModel        string
	InputTokens          int
	OutputTokens         int
	CachedInputTokens    int
	TotalTokens          int
	UnitPriceInputPer1M  string
	UnitPriceOutputPer1M string
	TotalCost            string
	DurationMs           int
	TTFTMs               *int
	Status               string
	ErrorCode            string
	ClientIP             string
	CreatedAt            string
}

type UsageLogDTO struct {
	ID                   int    `json:"id"`
	RequestID            string `json:"request_id"`
	UserID               *int   `json:"user_id"`
	APIKeyID             *int   `json:"api_key_id"`
	ChannelID            *int   `json:"channel_id"`
	ChannelName          string `json:"channel_name"`
	Model                string `json:"model"`
	UpstreamModel        string `json:"upstream_model"`
	InputTokens          int64  `json:"input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	CachedInputTokens    int64  `json:"cached_input_tokens"`
	TotalTokens          int64  `json:"total_tokens"`
	UnitPriceInputPer1M  string `json:"unit_price_input_per_1m"`
	UnitPriceOutputPer1M string `json:"unit_price_output_per_1m"`
	TotalCost            string `json:"total_cost"`
	DurationMs           int64  `json:"duration_ms"`
	TTFTMs               *int   `json:"ttft_ms"`
	Status               string `json:"status"`
	ErrorCode            string `json:"error_code"`
	ClientIP             string `json:"client_ip"`
	CreatedAt            string `json:"created_at"`
}

type StatsOverviewDTO struct {
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	ErrorCount     int64  `json:"error_count"`
	TotalTokens    int64  `json:"total_tokens"`
	TotalCost      string `json:"total_cost"`
	ActiveKeyCount int64  `json:"active_key_count"`
}

// TTFTStatsFilter narrows stream first-token latency samples. Non-streaming
// requests have no TTFT and are deliberately excluded from this metric.
type TTFTStatsFilter struct {
	UserID    *int
	APIKeyID  *int
	ChannelID *int
	Model     string
	StartTime string
	EndTime   string
}

type TTFTStatsDTO struct {
	SampleCount int64 `json:"sample_count"`
	AverageMs   int64 `json:"average_ms"`
	P50Ms       int64 `json:"p50_ms"`
	P95Ms       int64 `json:"p95_ms"`
	P99Ms       int64 `json:"p99_ms"`
}

type StatsDailyDTO struct {
	StatDate     string `json:"stat_date"`
	RequestCount int64  `json:"request_count"`
	SuccessCount int64  `json:"success_count"`
	ErrorCount   int64  `json:"error_count"`
	TotalTokens  int64  `json:"total_tokens"`
	TotalCost    string `json:"total_cost"`
}

type StatsChannelDTO struct {
	ChannelID    int    `json:"channel_id"`
	ChannelName  string `json:"channel_name"`
	RequestCount int64  `json:"request_count"`
	SuccessCount int64  `json:"success_count"`
	ErrorCount   int64  `json:"error_count"`
	TotalTokens  int64  `json:"total_tokens"`
	TotalCost    string `json:"total_cost"`
}

type UsageAggregateFilter struct {
	GroupBy   string
	UserID    *int
	APIKeyID  *int
	ChannelID *int
	Model     string
	Status    string
	StartTime string
	EndTime   string
	Page      int
	PageSize  int
}

type UsageAggregateDTO struct {
	UserID       *int   `json:"user_id"`
	APIKeyID     *int   `json:"api_key_id"`
	ChannelID    *int   `json:"channel_id"`
	Model        string `json:"model"`
	RequestCount int64  `json:"request_count"`
	SuccessCount int64  `json:"success_count"`
	ErrorCount   int64  `json:"error_count"`
	TotalTokens  int64  `json:"total_tokens"`
	TotalCost    string `json:"total_cost"`
	DurationMs   int64  `json:"duration_ms"`
}

type UsageLogFilter struct {
	UserID    *int
	APIKeyID  *int
	ChannelID *int
	Model     string
	Status    string
	StartTime string
	EndTime   string
	Page      int
	PageSize  int
}

type UsageCountFilter struct {
	UserID    int
	APIKeyID  *int
	Model     string
	ChannelID *int
	Since     string
}

type TokenCountFilter struct {
	UserID    int
	APIKeyID  *int
	Model     string
	ChannelID *int
	Since     string
}

// UsageLogInput is the write model reused by the downstream proxy (#6).
type UsageLogInput struct {
	RequestID            string
	UserID               *int
	APIKeyID             *int
	ChannelID            *int
	Model                string
	UpstreamModel        string
	InputTokens          int
	OutputTokens         int
	CachedInputTokens    int
	TotalTokens          int
	UnitPriceInputPer1M  string
	UnitPriceOutputPer1M string
	TotalCost            string
	DurationMs           int
	TTFTMs               *int
	Status               string
	ErrorCode            string
	ClientIP             string
}

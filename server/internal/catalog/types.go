package catalog

import "LLMGateway/server/internal/pagination"

type ListResponse[T any] = pagination.List[T]

type Channel struct {
	ID          int
	OwnerUserID int
	Name        string
	BaseURL     string
	// APIKey is the upstream secret. It is never serialized into responses.
	// Persistence layers must store it encrypted (see internal/crypto) and
	// expose only ciphertext; test fakes keep placeholder values in-process.
	APIKey   string
	AuthType string
	Status   int
	Weight   int
	Priority int
	Balance  *string
}

type ChannelDTO struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	BaseURL    string  `json:"base_url"`
	AuthType   string  `json:"auth_type"`
	Status     int     `json:"status"`
	Weight     int     `json:"weight"`
	Priority   int     `json:"priority"`
	Balance    *string `json:"balance"`
	ModelCount int     `json:"model_count"`
}

type ChannelModel struct {
	ID            int    `json:"id"`
	ModelName     string `json:"model_name"`
	UpstreamModel string `json:"upstream_model"`
	Enabled       bool   `json:"enabled"`
}

type CatalogChannelDTO struct {
	ChannelID     int    `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	UpstreamModel string `json:"upstream_model"`
	Enabled       bool   `json:"enabled"`
}

type CatalogModelDTO struct {
	ModelName string              `json:"model_name"`
	Status    int                 `json:"status"`
	Channels  []CatalogChannelDTO `json:"channels"`
}

type Pricing struct {
	ID                    int
	ChannelID             int
	UpstreamModel         string
	InputPricePer1M       string
	OutputPricePer1M      string
	CachedInputPricePer1M string
	Currency              string
}

type PricingDTO struct {
	ID                    int    `json:"id"`
	ChannelID             int    `json:"channel_id"`
	ChannelName           string `json:"channel_name"`
	UpstreamModel         string `json:"upstream_model"`
	InputPricePer1M       string `json:"input_price_per_1m"`
	OutputPricePer1M      string `json:"output_price_per_1m"`
	CachedInputPricePer1M string `json:"cached_input_price_per_1m"`
	Currency              string `json:"currency"`
}

type RouteCandidate struct {
	ChannelID     int
	ChannelName   string
	UpstreamModel string
	Priority      int
	Weight        int
	Balance       *string
}

type ChannelTestItemDTO struct {
	ModelAlias    string `json:"model_alias"`
	UpstreamModel string `json:"upstream_model"`
	HTTPStatus    int    `json:"http_status"`
	LatencyMs     int    `json:"latency_ms"`
	OK            bool   `json:"ok"`
	Error         string `json:"error"`
}

type ChannelTestResultDTO struct {
	List []ChannelTestItemDTO `json:"list"`
}

type ChannelInput struct {
	Name     string  `json:"name"`
	BaseURL  string  `json:"base_url"`
	APIKey   string  `json:"api_key"`
	AuthType string  `json:"auth_type"`
	Status   int     `json:"status"`
	Weight   int     `json:"weight"`
	Priority int     `json:"priority"`
	Balance  *string `json:"balance"`
}

type PricingInput struct {
	ChannelID             int    `json:"channel_id"`
	UpstreamModel         string `json:"upstream_model"`
	InputPricePer1M       string `json:"input_price_per_1m"`
	OutputPricePer1M      string `json:"output_price_per_1m"`
	CachedInputPricePer1M string `json:"cached_input_price_per_1m"`
	Currency              string `json:"currency"`
}

type DeletePricingInput struct {
	ChannelID     int    `json:"channel_id"`
	UpstreamModel string `json:"upstream_model"`
}

// ChannelBreakerConfigInput is the admin payload for a per-channel breaker
// override. FailureThreshold is not overridable per channel.
type ChannelBreakerConfigInput struct {
	WindowSeconds      int `json:"window_seconds"`
	MinimumSamples     int `json:"minimum_samples"`
	ErrorRatePercent   int `json:"error_rate_percent"`
	TimeoutRatePercent int `json:"timeout_rate_percent"`
	CooldownSeconds    int `json:"cooldown_seconds"`
}

// UserBreakerConfigDTO is the owner-level breaker default returned by
// /admin/breaker-config. It is not channel-scoped, so it carries no channel id.
type UserBreakerConfigDTO struct {
	WindowSeconds      int `json:"window_seconds"`
	MinimumSamples     int `json:"minimum_samples"`
	ErrorRatePercent   int `json:"error_rate_percent"`
	TimeoutRatePercent int `json:"timeout_rate_percent"`
	CooldownSeconds    int `json:"cooldown_seconds"`
}

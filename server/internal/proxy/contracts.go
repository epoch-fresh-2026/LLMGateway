package proxy

import (
	"context"
	"io"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/usage"
)

// Port is the composition boundary assembled by the process and HTTP layers.
// Business modules depend on their own narrower ports instead.
type Port interface {
	accounts.Port
	usage.Port
	SettlementTx() settlement.TxManager
}

// Quota is the quota surface proxy orchestration needs. quota.Server implements
// it, so proxy depends on quota rules rather than raw primitives.
type Quota interface {
	ReserveQuota(ctx context.Context, in quota.QuotaReserveInput) (quota.QuotaReservation, error)
	ReleaseQuota(ctx context.Context, reservationID int64) error
}

// RateLimit is the rate-limit surface proxy orchestration needs.
type RateLimit interface {
	ListRateLimits(ctx context.Context, enabled *bool, page, pageSize int) (ratelimit.ListResponse[ratelimit.RateLimitRuleDTO], error)
	ReserveRateLimit(ctx context.Context, in ratelimit.RateLimitReservationInput) (ratelimit.RateLimitReservation, error)
	FinalizeRateLimit(ctx context.Context, id int64, tokens int64) error
	ReleaseRateLimit(ctx context.Context, id int64) error
	CountActiveRateLimitReservations(ctx context.Context, userID int, apiKeyID *int, model string, channelID *int) (int64, error)
}

// Catalog is the catalog surface proxy orchestration needs. catalog.Server
// implements it, so proxy depends on catalog rules rather than raw primitives.
type Catalog interface {
	ListCatalogModels(ctx context.Context, ownerUserID int, enabledOnly bool) (catalog.ListResponse[catalog.CatalogModelDTO], error)
	RouteCandidates(ctx context.Context, ownerUserID int, modelName string) (catalog.ListResponse[catalog.RouteCandidate], error)
	GetChannelHealth(ctx context.Context, channelID int) (catalog.ChannelHealth, error)
	AcquireChannelProbe(ctx context.Context, channelID int, lease time.Duration) (string, bool, error)
	ReleaseChannelProbe(ctx context.Context, channelID int, leaseID string) (bool, error)
	GetChannelSecret(ctx context.Context, ownerUserID, channelID int) (*catalog.Channel, error)
	RecordChannelAttempt(ctx context.Context, channelID int, success bool, reason catalog.FailureReason) (catalog.ChannelHealth, error)
	GetPricing(ctx context.Context, channelID int, modelName string) (catalog.PricingDTO, error)
}

// ChatRequest is the protocol-neutral input needed by proxy orchestration.
type ChatRequest struct {
	Model  string
	Stream bool
	Body   []byte
}

// Usage contains the token counts needed for settlement and audit logging.
type Usage struct {
	PromptTokens      int
	CompletionTokens  int
	TotalTokens       int
	CachedInputTokens int
}

type EstimatedUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// Model is a public model visible to an authenticated caller.
type Model struct {
	ID      string
	Created int64
	OwnedBy string
}

// ModelList is the protocol-neutral result of model discovery.
type ModelList struct {
	Models []Model
}

// ChatResponse carries an upstream response without protocol-specific DTOs.
type ChatResponse struct {
	Status int
	Body   []byte
	Usage  *Usage
	Stream ChatStream
}

// ChatStream forwards a successful upstream stream while proxy orchestration
// retains ownership of settlement, audit logging and channel health.
type ChatStream interface {
	Forward(func([]byte) error) error
	Close() error
}

// StreamEvent is a protocol-neutral event emitted by a wire adapter.
type StreamEvent struct {
	Frame []byte
	Data  bool
	Done  bool
	Usage *Usage
	// Text is content successfully forwarded to the downstream caller. It is
	// accumulated for local token estimation only when upstream final usage is
	// unavailable.
	Text string
}

// ProtocolAdapter contains the small protocol seam needed by proxy business
// orchestration. The proxy package owns the contract; adapters own wire rules.
type ProtocolAdapter struct {
	RewriteRequest  func([]byte, string) ([]byte, error)
	ParseUsage      func([]byte) *Usage
	RewriteResponse func([]byte, string) []byte
	ParseStream     func(io.Reader, string, func(StreamEvent) error) error
	StreamError     func(string, string) []byte
	EstimateUsage   func([]byte, int) (EstimatedUsage, error)
	CountTextTokens func(string, string) (int, error)
}

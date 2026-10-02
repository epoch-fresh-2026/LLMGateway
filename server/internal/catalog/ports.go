package catalog

import (
	"context"
	"time"
)

// Port is the catalog persistence primitive surface. It exposes only CRUD and
// query primitives: business rules, defaults, encryption and orchestration live
// on Server. Every channel read/write is scoped by ownerUserID.
type Port interface {
	ListChannels(ctx context.Context, ownerUserID int) (ListResponse[ChannelDTO], error)
	GetChannelDTO(ctx context.Context, ownerUserID, id int) (ChannelDTO, error)
	// GetChannelOwner returns the owner of a channel by id; used to resolve the
	// owner-level breaker default on the request path where only the channel is
	// known.
	GetChannelOwner(ctx context.Context, channelID int) (int, error)
	GetChannelRecord(ctx context.Context, ownerUserID, id int) (ChannelRecord, error)
	InsertChannel(ctx context.Context, in ChannelInsert) (int, error)
	UpdateChannelRecord(ctx context.Context, ownerUserID, id int, in ChannelUpdate) (bool, error)
	UpdateChannelStatusRecord(ctx context.Context, ownerUserID, id, status int) (bool, error)
	DeleteChannel(ctx context.Context, ownerUserID, id int) (bool, error)

	ListChannelModels(ctx context.Context, ownerUserID, channelID int) (ListResponse[ChannelModel], error)
	GetChannelModelByID(ctx context.Context, channelID, modelID int) (ChannelModel, bool, error)
	InsertChannelModel(ctx context.Context, channelID int, in ChannelModel) (ChannelModel, error)
	DeleteChannelModel(ctx context.Context, channelID, modelID int) (bool, error)
	// ChannelModelExists reports whether a model mapping exists on a channel.
	ChannelModelExists(ctx context.Context, ownerUserID, channelID int, modelName string) (bool, error)
	// ChannelUpstreamExists reports whether a channel maps any public alias to
	// the given upstream model, so pricing can only target a real model.
	ChannelUpstreamExists(ctx context.Context, ownerUserID, channelID int, upstreamModel string) (bool, error)

	ListCatalogModels(ctx context.Context, ownerUserID int, enabledOnly bool) (ListResponse[CatalogModelDTO], error)

	ListPricing(ctx context.Context, ownerUserID int) (ListResponse[PricingDTO], error)
	UpsertPricingRecord(ctx context.Context, in PricingRecord) (PricingDTO, error)
	DeletePricing(ctx context.Context, in DeletePricingInput) error
	GetPricing(ctx context.Context, channelID int, upstreamModel string) (PricingDTO, error)

	// RouteCandidates returns enabled mappings on enabled, non-open channels for
	// a public model owned by ownerUserID, ordered by priority desc, weight desc,
	// channel id. The caller supplies the breaker cooldown in seconds.
	RouteCandidates(ctx context.Context, ownerUserID int, modelName string, cooldownSeconds int) (ListResponse[RouteCandidate], error)
}

// Tx is the transaction-scoped persistence surface for catalog writes. It
// exposes only primitives; Server owns the state transitions and balance math.
type Tx interface {
	EnsureChannelHealth(channelID int) error
	GetChannelHealthForUpdate(channelID int) (ChannelHealth, error)
	UpdateChannelHealth(health ChannelHealth) (bool, error)
	DeleteChannelHealth(channelID int) error

	// UpsertChannelHealthBucket adds one attempt to the fixed-width bucket that
	// contains bucketStart. GetChannelHealthWindow sums the buckets at or after
	// since, so both must run in the caller's transaction to stay consistent.
	UpsertChannelHealthBucket(channelID int, bucketStart time.Time, requests, errors, timeouts int64) error
	GetChannelHealthWindow(channelID int, since time.Time) (ChannelHealthWindow, error)

	UpsertUserBreakerConfig(ownerUserID int, cfg ChannelBreakerConfig) error
	DeleteUserBreakerConfig(ownerUserID int) error

	// UpdateChannelModelRecord renames a mapping's public model name and toggles
	// it. The upstream model name is upstream-owned and cannot change.
	UpdateChannelModelRecord(channelID, modelID int, modelName string, enabled bool) (ChannelModel, bool, error)

	LockChannel(ownerUserID, channelID int) error
	GetChannelBalanceText(ownerUserID, channelID int) (string, error)
	UpdateChannelBalance(ownerUserID, channelID int, balance string) (bool, error)
}

// TxManager runs fn inside a single database transaction.
type TxManager interface {
	InTx(ctx context.Context, fn func(Tx) error) error
}

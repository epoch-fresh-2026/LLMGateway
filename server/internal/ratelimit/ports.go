package ratelimit

import "context"

// Port is the rate-limit persistence primitive surface. It exposes only CRUD,
// query and ownership primitives: normalization and validation live on Server.
type Port interface {
	ListRateLimits(ctx context.Context, ownerUserID int, enabled *bool, page, pageSize int) (ListResponse[RateLimitRuleDTO], error)
	GetRateLimit(ctx context.Context, ownerUserID, id int) (RateLimitRule, error)
	InsertRateLimit(ctx context.Context, ownerUserID int, rule RateLimitRule) (int, error)
	UpdateRateLimitRecord(ctx context.Context, ownerUserID, id int, rule RateLimitRule) (bool, error)
	DeleteRateLimit(ctx context.Context, ownerUserID, id int) (bool, error)

	// TargetOwnedByUser reports whether a rule target value references a
	// resource owned by ownerUserID. "*" is always owned; user targets must
	// equal the owner; api_key/model/channel targets must belong to the owner.
	TargetOwnedByUser(ctx context.Context, ownerUserID int, targetType, targetValue string) (bool, error)

	InsertRateLimitReservation(ctx context.Context, in RateLimitReservationInput) (int64, error)
	FinalizeRateLimitReservation(ctx context.Context, id int64) (bool, error)
	ReleaseRateLimitReservation(ctx context.Context, id int64) (bool, error)
	ReapRateLimitReservations(ctx context.Context, limit int) (int, error)
	CountActiveRateLimitReservations(ctx context.Context, userID int, apiKeyID *int, model string, channelID *int) (int64, error)
}

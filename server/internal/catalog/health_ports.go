package catalog

import (
	"context"
	"time"
)

// HealthPort persists per-channel circuit breaker state. Reading health lazily
// evaluates the open -> half-open transition; the transition is only persisted
// on the next recorded success/failure. The breaker state machine is
// catalog-owned pure business logic.
type HealthPort interface {
	ListChannelHealthRows(ctx context.Context, ownerUserID int) ([]ChannelHealth, error)
	// GetChannelHealthRow returns the stored row and whether it exists.
	GetChannelHealthRow(ctx context.Context, channelID int) (ChannelHealth, bool, error)
	// AcquireChannelProbe grants a single-flight half-open probe lease and
	// returns its id. A non-nil err means the gate could not be evaluated;
	// ok=false means another probe already holds an unexpired lease.
	AcquireChannelProbe(ctx context.Context, channelID int, lease time.Duration) (leaseID string, ok bool, err error)
	// ReleaseChannelProbe deletes the lease only when it is still owned by
	// leaseID, so a late release cannot clobber a newer probe. It is idempotent.
	ReleaseChannelProbe(ctx context.Context, channelID int, leaseID string) (bool, error)

	// GetUserBreakerConfigRow returns the owner-level breaker default and whether
	// it exists; a missing row means the owner inherits the process default.
	GetUserBreakerConfigRow(ctx context.Context, ownerUserID int) (ChannelBreakerConfig, bool, error)
	// DeleteStaleChannelHealthBuckets drops buckets older than before and
	// returns the number removed. Callers size before from the largest window.
	DeleteStaleChannelHealthBuckets(ctx context.Context, before time.Time) (int, error)
}

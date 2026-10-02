package quota

import (
	"context"
	"time"
)

// Port is the quota persistence primitive surface. It exposes only CRUD, query
// and ownership primitives: normalization, validation and reservation
// orchestration live on Server. Policy reads/writes are scoped to their owner.
type Port interface {
	ListQuotaPolicies(ctx context.Context, filter QuotaPolicyFilter) (ListResponse[QuotaPolicyDTO], error)
	GetQuotaPolicy(ctx context.Context, ownerUserID, id int) (QuotaPolicy, error)
	InsertQuotaPolicy(ctx context.Context, policy QuotaPolicy) (int, error)
	DeleteQuotaPolicy(ctx context.Context, ownerUserID, id int) (bool, error)
	ListQuotaUsage(ctx context.Context, filter QuotaPolicyFilter) (ListResponse[QuotaUsageDTO], error)

	// KeyBelongsToUser reports whether a gateway key belongs to ownerUserID, so
	// api_key-scoped policies cannot target another user's key.
	KeyBelongsToUser(ctx context.Context, ownerUserID, keyID int) (bool, error)
}

// Tx is the transaction-scoped persistence surface for quota reservations.
type Tx interface {
	// ApplicablePolicies locks and returns enabled policies for the identity.
	ApplicablePolicies(userID, keyID int) ([]QuotaPolicy, error)
	ReapExpired(now time.Time, limit, userID, keyID int) (int, error)
	InsertReservation(in QuotaReservationInsert) (int64, error)
	UpsertBucket(policyID int, start, end time.Time) error
	LockBucket(policyID int, start time.Time) error
	// ReserveBucket atomically adds the reservation to the bucket when the
	// policy limits allow it, returning false when the limit would be exceeded.
	ReserveBucket(policyID int, start time.Time, tokens int64, cost string) (bool, error)
	InsertReservationItem(reservationID int64, policyID int, start time.Time, tokens int64, cost string) error
	ReleaseReservation(reservationID int64, status string, now time.Time) error
}

// TxManager runs fn inside a single database transaction.
type TxManager interface {
	InTx(ctx context.Context, fn func(Tx) error) error
}

// QuotaReservationInsert describes a reservation row to persist.
type QuotaReservationInsert struct {
	RequestID       string
	UserID          int
	APIKeyID        int
	Model           string
	EstimatedTokens int64
	EstimatedCost   string
	ExpiresAt       time.Time
}

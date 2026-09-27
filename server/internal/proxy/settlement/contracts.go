// Package settlement owns the proxy settlement transaction contract. It lives in
// its own package so persistence implementations (store/postgres, storefake)
// can implement it without importing the proxy orchestration package.
package settlement

import (
	"context"

	"LLMGateway/server/internal/usage"
)

// Input describes a successful chat completion to settle. Users are no longer
// billed; settlement only applies the approximate channel cost, the quota
// reservation and the usage log.
type Input struct {
	ReservationID int64
	UserID        int
	APIKeyID      int
	ChannelID     *int
	Cost          string
	DebitChannel  bool
	UsageLog      usage.UsageLogInput
}

// Tx is the transaction-scoped persistence surface for settlement. It exposes
// only CRUD/locking primitives; the atomic order belongs to the proxy
// orchestration.
type Tx interface {
	LockChannel(userID, channelID int) error
	GetChannelBalanceText(userID, channelID int) (string, error)
	UpdateChannelBalance(userID, channelID int, balance string) (bool, error)

	SettleQuotaReservation(reservationID int64, requestID string, userID, keyID int, actualTokens int64, actualCost string) error
	InsertUsageLog(in usage.UsageLogInput) (int, error)
}

// TxManager runs fn inside a single database transaction.
type TxManager interface {
	InTx(ctx context.Context, fn func(Tx) error) error
}

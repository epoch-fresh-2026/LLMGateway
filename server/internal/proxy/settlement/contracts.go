// Package settlement owns the proxy settlement transaction contract. It lives in
// its own package so persistence implementations (store/postgres, storefake)
// can implement it without importing the proxy orchestration package.
package settlement

import (
	"context"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/usage"
)

// Input describes a successful chat completion to settle.
type Input struct {
	ReservationID int64
	UserID        int
	APIKeyID      int
	ChannelID     *int
	Cost          string
	DebitChannel  bool
	Description   string
	UsageLog      usage.UsageLogInput
}

// Tx is the transaction-scoped persistence surface for settlement. It exposes
// only CRUD/locking primitives; the atomic order and the insufficient-balance
// decision belong to the proxy orchestration.
type Tx interface {
	LockUserBalance(userID int) error
	GetUserBalanceText(userID int) (string, error)
	UpdateUserBalance(userID int, available string) (bool, error)
	InsertBalanceTransaction(in accounts.BalanceTransactionInput) error

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

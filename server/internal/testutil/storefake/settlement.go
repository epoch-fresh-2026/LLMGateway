package storefake

import (
	"context"

	"LLMGateway/server/internal/accounts"
	catalog "LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/money"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/store"
	usage "LLMGateway/server/internal/usage"
)

func (s *Store) SettlementTx() settlement.TxManager { return settlementRunner{store: s} }

// settlementRunner runs the settlement callback under the store mutex and
// restores the mutated state when it fails, emulating transaction rollback.
type settlementRunner struct {
	store *Store
}

func (r settlementRunner) InTx(_ context.Context, fn func(settlement.Tx) error) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	snapshot := r.store.snapshotSettlement()
	if err := fn(&settlementTx{s: r.store}); err != nil {
		r.store.restoreSettlement(snapshot)
		return err
	}
	return nil
}

type settlementTx struct {
	s *Store
}

func (t *settlementTx) LockUserBalance(userID int) error {
	if _, ok := t.s.users[userID]; !ok {
		return store.ErrNotFound
	}
	return nil
}

func (t *settlementTx) GetUserBalanceText(userID int) (string, error) {
	user, ok := t.s.users[userID]
	if !ok {
		return "", store.ErrNotFound
	}
	return user.AvailableBalance, nil
}

func (t *settlementTx) UpdateUserBalance(userID int, available string) (bool, error) {
	user, ok := t.s.users[userID]
	if !ok {
		return false, nil
	}
	user.AvailableBalance = available
	return true, nil
}

func (t *settlementTx) InsertBalanceTransaction(in accounts.BalanceTransactionInput) error {
	tx := balanceTransaction{
		ID:           t.s.nextTxID,
		TxType:       in.TxType,
		Amount:       in.Amount,
		BalanceAfter: in.BalanceAfter,
		Description:  in.Description,
		CreatedAt:    nowRFC3339(),
	}
	t.s.nextTxID++
	t.s.transactions[in.UserID] = append(t.s.transactions[in.UserID], tx)
	return nil
}

func (t *settlementTx) LockChannel(userID, channelID int) error {
	channel, ok := t.s.channels[channelID]
	if !ok || channel.OwnerUserID != userID {
		return store.ErrNotFound
	}
	return nil
}

func (t *settlementTx) GetChannelBalanceText(userID, channelID int) (string, error) {
	channel, ok := t.s.channels[channelID]
	if !ok || channel.OwnerUserID != userID {
		return "", store.ErrNotFound
	}
	if channel.Balance == nil {
		return "", nil
	}
	return *channel.Balance, nil
}

func (t *settlementTx) UpdateChannelBalance(userID, channelID int, balance string) (bool, error) {
	channel, ok := t.s.channels[channelID]
	if !ok || channel.OwnerUserID != userID {
		return false, nil
	}
	value := balance
	channel.Balance = &value
	return true, nil
}

func (t *settlementTx) SettleQuotaReservation(reservationID int64, requestID string, userID, keyID int, actualTokens int64, actualCost string) error {
	cost, err := money.Parse6(actualCost)
	if err != nil {
		return store.ErrInvalid
	}
	return t.s.settleQuotaLocked(reservationID, requestID, userID, keyID, actualTokens, cost)
}

func (t *settlementTx) InsertUsageLog(in usage.UsageLogInput) (int, error) {
	return t.s.insertUsageLogLocked(in)
}

// settlementSnapshot captures every field settlement can mutate so a failed
// callback can be rolled back.
type settlementSnapshot struct {
	users             map[int]*accounts.User
	transactions      map[int][]balanceTransaction
	orders            map[string]balanceTransaction
	channels          map[int]*catalog.Channel
	usageLogs         []usage.UsageLog
	quotaBuckets      map[string]*fakeQuotaBucket
	quotaReservations map[int64]*fakeQuotaReservation
	nextTxID          int
	nextUsageLogID    int
}

func (s *Store) snapshotSettlement() settlementSnapshot {
	snapshot := settlementSnapshot{
		users:             make(map[int]*accounts.User, len(s.users)),
		transactions:      make(map[int][]balanceTransaction, len(s.transactions)),
		orders:            make(map[string]balanceTransaction, len(s.orders)),
		channels:          make(map[int]*catalog.Channel, len(s.channels)),
		usageLogs:         append([]usage.UsageLog(nil), s.usageLogs...),
		quotaBuckets:      make(map[string]*fakeQuotaBucket, len(s.quotaBuckets)),
		quotaReservations: make(map[int64]*fakeQuotaReservation, len(s.quotaReservations)),
		nextTxID:          s.nextTxID,
		nextUsageLogID:    s.nextUsageLogID,
	}
	for id, user := range s.users {
		copied := *user
		snapshot.users[id] = &copied
	}
	for id, txs := range s.transactions {
		snapshot.transactions[id] = append([]balanceTransaction(nil), txs...)
	}
	for key, tx := range s.orders {
		snapshot.orders[key] = tx
	}
	for id, channel := range s.channels {
		copied := *channel
		snapshot.channels[id] = &copied
	}
	for key, bucket := range s.quotaBuckets {
		copied := *bucket
		snapshot.quotaBuckets[key] = &copied
	}
	for id, reservation := range s.quotaReservations {
		copied := *reservation
		copied.items = append([]string(nil), reservation.items...)
		snapshot.quotaReservations[id] = &copied
	}
	return snapshot
}

func (s *Store) restoreSettlement(snapshot settlementSnapshot) {
	s.users = snapshot.users
	s.transactions = snapshot.transactions
	s.orders = snapshot.orders
	s.channels = snapshot.channels
	s.usageLogs = snapshot.usageLogs
	s.quotaBuckets = snapshot.quotaBuckets
	s.quotaReservations = snapshot.quotaReservations
	s.nextTxID = snapshot.nextTxID
	s.nextUsageLogID = snapshot.nextUsageLogID
}

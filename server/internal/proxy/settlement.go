package proxy

import (
	"context"
	"fmt"

	"LLMGateway/server/internal/accounts"
	apperrors "LLMGateway/server/internal/errors"
	"LLMGateway/server/internal/money"
	settlement "LLMGateway/server/internal/proxy/settlement"
)

// Settle atomically settles a successful chat completion: quota reservation,
// user debit, optional channel debit and the success usage log. It returns the
// created usage log id.
//
// The transaction runs detached from the request context so a canceled
// downstream request cannot abort a charge for work the upstream already
// performed. It is bounded by settleTimeout so it cannot hang on locks.
func (a *Service) Settle(ctx context.Context, in settlement.Input) (int, error) {
	cost, err := money.Parse6(in.Cost)
	if err != nil || cost.Cmp(0) < 0 {
		return 0, fmt.Errorf("%w: invalid cost", apperrors.ErrInvalid)
	}

	settleCtx, cancel := detachedCtx(ctx, settleTimeout)
	defer cancel()

	var usageID int
	err = a.settleTx.InTx(settleCtx, func(tx settlement.Tx) error {
		if err := tx.SettleQuotaReservation(in.ReservationID, in.UsageLog.RequestID, in.UserID, in.APIKeyID, int64(in.UsageLog.TotalTokens), money.Format6(cost)); err != nil {
			return err
		}

		if err := tx.LockUserBalance(in.UserID); err != nil {
			return err
		}
		currentText, err := tx.GetUserBalanceText(in.UserID)
		if err != nil {
			return err
		}
		current, err := money.Parse6(currentText)
		if err != nil {
			return fmt.Errorf("%w: invalid balance", apperrors.ErrInvalid)
		}
		if cost.Cmp(0) > 0 && current.Cmp(cost) < 0 {
			return fmt.Errorf("%w: insufficient balance", apperrors.ErrInvalid)
		}

		if cost.Cmp(0) > 0 {
			next := money.Format6(current.Sub(cost))
			ok, err := tx.UpdateUserBalance(in.UserID, next)
			if err != nil {
				return err
			}
			if !ok {
				return apperrors.ErrNotFound
			}
			if err := tx.InsertBalanceTransaction(accounts.BalanceTransactionInput{
				UserID:       in.UserID,
				TxType:       "consume",
				Amount:       money.Format6(cost),
				BalanceAfter: next,
				Description:  in.Description,
			}); err != nil {
				return err
			}
		}

		if in.DebitChannel && cost.Cmp(0) > 0 {
			if in.ChannelID == nil {
				return fmt.Errorf("%w: channel_id is required", apperrors.ErrInvalid)
			}
			if err := tx.LockChannel(in.UserID, *in.ChannelID); err != nil {
				return err
			}
			baseText, err := tx.GetChannelBalanceText(in.UserID, *in.ChannelID)
			if err != nil {
				return err
			}
			base := money.Amount(0)
			if baseText != "" {
				parsed, err := money.Parse6(baseText)
				if err != nil {
					return fmt.Errorf("%w: invalid channel balance", apperrors.ErrInvalid)
				}
				base = parsed
			}
			ok, err := tx.UpdateChannelBalance(in.UserID, *in.ChannelID, money.Format6(base.Sub(cost)))
			if err != nil {
				return err
			}
			if !ok {
				return apperrors.ErrNotFound
			}
		}

		id, err := tx.InsertUsageLog(in.UsageLog)
		if err != nil {
			return err
		}
		usageID = id
		return nil
	})
	if err != nil {
		return 0, err
	}
	return usageID, nil
}

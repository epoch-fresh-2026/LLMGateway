package proxy

import (
	"context"
	"fmt"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	apperrors "LLMGateway/server/internal/errors"
	"LLMGateway/server/internal/money"
	settlement "LLMGateway/server/internal/proxy/settlement"
	usagecontracts "LLMGateway/server/internal/usage"
)

// Settle atomically settles a successful chat completion: quota reservation,
// optional approximate channel debit and the success usage log. It returns the
// created usage log id. Users are not billed.
//
// The transaction runs detached from the request context so a canceled
// downstream request cannot abort bookkeeping for work the upstream already
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

// settleUsageInput captures the fields needed to record and settle one priced
// completion attempt. The caller owns pricing (priceFor) and channel-health
// recording; settleUsage owns the atomic quota/balance/usage-log write plus the
// best-effort rate-reservation finalization and key last-used update.
type settleUsageInput struct {
	requestID         string
	auth              *accounts.AuthContext
	candidate         catalog.RouteCandidate
	publicModel       string
	clientIP          string
	durationMs        int
	status            string
	errorCode         string
	ttft              *int
	reservationID     int64
	rateReservationID int64
	cost              string
	inputPrice        string
	outputPrice       string
	usage             *Usage
}

// settleUsage records the usage log and settles the quota reservation and
// channel debit in one transaction, then best-effort finalizes the rate
// reservation and the key last-used timestamp. It returns the constructed usage
// log even on failure so callers can persist a distinct fallback entry.
func (a *Service) settleUsage(ctx context.Context, in settleUsageInput) (usagecontracts.UsageLogInput, error) {
	log := a.usageLogInput(in.requestID, in.auth, &in.candidate.ChannelID, in.candidate.UpstreamModel, in.publicModel, in.usage, in.cost, in.inputPrice, in.outputPrice, in.durationMs, in.clientIP, in.status, in.errorCode)
	log.TTFTMs = in.ttft
	if _, err := a.Settle(ctx, settlement.Input{
		ReservationID: in.reservationID,
		UserID:        in.auth.UserID,
		APIKeyID:      in.auth.KeyID,
		ChannelID:     &in.candidate.ChannelID,
		Cost:          in.cost,
		DebitChannel:  in.candidate.Balance != nil,
		UsageLog:      log,
	}); err != nil {
		return log, err
	}
	a.finalizeRateReservation(ctx, in.rateReservationID, in.usage)
	a.touchKeyLastUsed(ctx, in.auth.KeyID)
	return log, nil
}

// finalizeRateReservation is best-effort: the request already succeeded and was
// charged, so a finalization failure must not turn it into an error response.
func (a *Service) finalizeRateReservation(ctx context.Context, id int64, usage *Usage) {
	if id == 0 || usage == nil {
		return
	}
	finalizeCtx, cancel := detachedCtx(ctx, bestEffortTimeout)
	defer cancel()
	_ = a.ratelimit.FinalizeRateLimit(finalizeCtx, id, int64(usage.TotalTokens))
}

// touchKeyLastUsed is best-effort and must never change the response.
func (a *Service) touchKeyLastUsed(ctx context.Context, keyID int) {
	lastUsedCtx, cancel := detachedCtx(ctx, bestEffortTimeout)
	defer cancel()
	_ = a.store.UpdateKeyLastUsed(lastUsedCtx, keyID)
}

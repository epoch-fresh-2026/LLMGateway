package quota

import (
	"context"
	"fmt"

	"LLMGateway/server/internal/money"
)

func (a *Server) ListQuotaPolicies(ctx context.Context, filter QuotaPolicyFilter) (ListResponse[QuotaPolicyDTO], error) {
	return a.store.ListQuotaPolicies(ctx, filter)
}

// CreateQuotaPolicy normalizes the input, checks the policy targets a resource
// owned by ownerUserID, and persists it.
func (a *Server) CreateQuotaPolicy(ctx context.Context, ownerUserID int, in QuotaPolicyInput) (QuotaPolicyDTO, error) {
	policy, err := NormalizeQuotaPolicy(in, nil)
	if err != nil {
		return QuotaPolicyDTO{}, err
	}
	if err := a.validateScope(ctx, ownerUserID, policy); err != nil {
		return QuotaPolicyDTO{}, err
	}
	id, err := a.store.InsertQuotaPolicy(ctx, policy)
	if err != nil {
		return QuotaPolicyDTO{}, err
	}
	stored, err := a.store.GetQuotaPolicy(ctx, ownerUserID, id)
	if err != nil {
		return QuotaPolicyDTO{}, err
	}
	return QuotaPolicyToDTO(stored), nil
}

// UpdateQuotaPolicy normalizes the input over the stored policy and rejects
// changes to the scope or period.
func (a *Server) UpdateQuotaPolicy(ctx context.Context, ownerUserID, id int, in QuotaPolicyInput) (QuotaPolicyDTO, error) {
	existing, err := a.store.GetQuotaPolicy(ctx, ownerUserID, id)
	if err != nil {
		return QuotaPolicyDTO{}, err
	}
	policy, err := NormalizeQuotaPolicy(in, &existing)
	if err != nil {
		return QuotaPolicyDTO{}, err
	}
	if in.ScopeType != nil || in.ScopeID != nil || in.PeriodType != nil {
		if policy.ScopeType != existing.ScopeType || policy.ScopeID != existing.ScopeID || policy.PeriodType != existing.PeriodType {
			return QuotaPolicyDTO{}, fmt.Errorf("%w: quota scope and period cannot be changed", ErrInvalid)
		}
	}
	ok, err := a.store.UpdateQuotaPolicyRecord(ctx, ownerUserID, id, policy)
	if err != nil {
		return QuotaPolicyDTO{}, err
	}
	if !ok {
		return QuotaPolicyDTO{}, ErrNotFound
	}
	stored, err := a.store.GetQuotaPolicy(ctx, ownerUserID, id)
	if err != nil {
		return QuotaPolicyDTO{}, err
	}
	return QuotaPolicyToDTO(stored), nil
}

func (a *Server) DeleteQuotaPolicy(ctx context.Context, ownerUserID, id int) error {
	ok, err := a.store.DeleteQuotaPolicy(ctx, ownerUserID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// validateScope ensures the policy targets the owner: user-scoped policies must
// name the owner, api_key-scoped policies must reference the owner's key. A
// mismatch returns ErrNotFound so it does not reveal other users' resources.
func (a *Server) validateScope(ctx context.Context, ownerUserID int, policy QuotaPolicy) error {
	if policy.ScopeType == QuotaScopeUser {
		if policy.ScopeID != ownerUserID {
			return ErrNotFound
		}
		return nil
	}
	owned, err := a.store.KeyBelongsToUser(ctx, ownerUserID, policy.ScopeID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrNotFound
	}
	return nil
}

func (a *Server) ListQuotaUsage(ctx context.Context, filter QuotaPolicyFilter) (ListResponse[QuotaUsageDTO], error) {
	return a.store.ListQuotaUsage(ctx, filter)
}

// ReserveQuota validates the request, parses the estimated cost and reserves
// tokens/cost against every applicable policy in one transaction.
func (a *Server) ReserveQuota(ctx context.Context, in QuotaReserveInput) (QuotaReservation, error) {
	if in.RequestID == "" || in.UserID <= 0 || in.APIKeyID <= 0 || in.EstimatedTokens < 0 || !in.ExpiresAt.After(a.now()) {
		return QuotaReservation{}, fmt.Errorf("%w: invalid quota reservation", ErrInvalid)
	}
	cost, err := money.Parse6(in.EstimatedCost)
	if err != nil || cost.Cmp(0) < 0 {
		return QuotaReservation{}, fmt.Errorf("%w: invalid estimated cost", ErrInvalid)
	}
	formattedCost := money.Format6(cost)

	var result QuotaReservation
	err = a.tx.InTx(ctx, func(tx Tx) error {
		policies, err := tx.ApplicablePolicies(in.UserID, in.APIKeyID)
		if err != nil {
			return err
		}
		// Policy locks precede reservation/bucket locks everywhere identity
		// deletion and admission can overlap, preventing reverse lock-order
		// deadlocks.
		if _, err := tx.ReapExpired(a.now(), 16, in.UserID, in.APIKeyID); err != nil {
			return err
		}
		if len(policies) == 0 {
			return nil
		}
		reservationID, err := tx.InsertReservation(QuotaReservationInsert{
			RequestID:       in.RequestID,
			UserID:          in.UserID,
			APIKeyID:        in.APIKeyID,
			Model:           in.Model,
			EstimatedTokens: in.EstimatedTokens,
			EstimatedCost:   formattedCost,
			ExpiresAt:       in.ExpiresAt,
		})
		if err != nil {
			return err
		}
		now := a.now().UTC()
		for _, policy := range policies {
			start, end, err := QuotaPeriodBounds(now, policy.PeriodType)
			if err != nil {
				return err
			}
			if err := tx.UpsertBucket(policy.ID, start, end); err != nil {
				return err
			}
			if err := tx.LockBucket(policy.ID, start); err != nil {
				return err
			}
			ok, err := tx.ReserveBucket(policy.ID, start, in.EstimatedTokens, formattedCost)
			if err != nil {
				return err
			}
			if !ok {
				return ErrQuotaExceeded
			}
			if err := tx.InsertReservationItem(reservationID, policy.ID, start, in.EstimatedTokens, formattedCost); err != nil {
				return err
			}
		}
		result = QuotaReservation{ID: reservationID, RequestID: in.RequestID, EstimatedTokens: in.EstimatedTokens, EstimatedCost: formattedCost}
		return nil
	})
	if err != nil {
		return QuotaReservation{}, err
	}
	return result, nil
}

func (a *Server) ReleaseQuota(ctx context.Context, reservationID int64) error {
	if reservationID == 0 {
		return nil
	}
	return a.tx.InTx(ctx, func(tx Tx) error {
		return tx.ReleaseReservation(reservationID, "released", a.now())
	})
}

func (a *Server) ReapExpiredQuotaReservations(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	var count int
	err := a.tx.InTx(ctx, func(tx Tx) error {
		n, err := tx.ReapExpired(a.now(), limit, 0, 0)
		if err != nil {
			return err
		}
		count = n
		return nil
	})
	return count, err
}

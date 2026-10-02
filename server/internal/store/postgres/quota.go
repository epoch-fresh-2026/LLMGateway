package postgres

import (
	"context"
	"fmt"
	"time"

	"LLMGateway/server/internal/db/sqlc"
	domain "LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/store"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type quotaPolicyRow struct {
	id         int64
	name       string
	scopeType  domain.QuotaScopeType
	scopeID    int
	periodType domain.QuotaPeriodType
	tokenLimit *int64
	costLimit  *string
	enabled    bool
}

type quotaItemRow struct {
	policyID      int64
	periodStart   time.Time
	reservedToken int64
	reservedCost  string
}

func (s *Store) GetQuotaPolicy(ctx context.Context, ownerUserID, id int) (domain.QuotaPolicy, error) {
	row, err := s.queries.GetQuotaPolicy(ctx, sqlc.GetQuotaPolicyParams{ID: int64(id), OwnerUserID: pgtype.Int8{Int64: int64(ownerUserID), Valid: true}})
	if err != nil {
		return domain.QuotaPolicy{}, mapError(err)
	}
	scopeID := optionalInt(row.UserID)
	if row.ScopeType == string(domain.QuotaScopeAPIKey) {
		scopeID = optionalInt(row.ApiKeyID)
	}
	if scopeID == nil {
		return domain.QuotaPolicy{}, fmt.Errorf("%w: quota scope missing", store.ErrInvalid)
	}
	var tokenLimit *int64
	if row.TokenLimit.Valid {
		value := row.TokenLimit.Int64
		tokenLimit = &value
	}
	return domain.QuotaPolicy{
		ID:         int(row.ID),
		PolicyName: row.PolicyName,
		ScopeType:  domain.QuotaScopeType(row.ScopeType),
		ScopeID:    *scopeID,
		PeriodType: domain.QuotaPeriodType(row.PeriodType),
		TokenLimit: tokenLimit,
		CostLimit:  optionalString(textValue(row.CostLimit)),
		Enabled:    row.Enabled,
	}, nil
}

func (s *Store) InsertQuotaPolicy(ctx context.Context, policy domain.QuotaPolicy) (int, error) {
	params := sqlc.CreateQuotaPolicyParams{
		PolicyName: policy.PolicyName,
		ScopeType:  string(policy.ScopeType),
		PeriodType: string(policy.PeriodType),
		Enabled:    policy.Enabled,
	}
	if policy.ScopeType == domain.QuotaScopeUser {
		params.UserID = pgtype.Int8{Int64: int64(policy.ScopeID), Valid: true}
	} else {
		params.ApiKeyID = pgtype.Int8{Int64: int64(policy.ScopeID), Valid: true}
	}
	if policy.TokenLimit != nil {
		params.TokenLimit = pgtype.Int8{Int64: *policy.TokenLimit, Valid: true}
	}
	params.CostLimit = numericValue(policy.CostLimit)

	id, err := s.queries.CreateQuotaPolicy(ctx, params)
	if err != nil {
		return 0, mapError(err)
	}
	return int(id), nil
}

func (s *Store) DeleteQuotaPolicy(ctx context.Context, ownerUserID, id int) (bool, error) {
	affected, err := s.queries.DeleteQuotaPolicy(ctx, sqlc.DeleteQuotaPolicyParams{ID: int64(id), OwnerUserID: pgtype.Int8{Int64: int64(ownerUserID), Valid: true}})
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (s *Store) KeyBelongsToUser(ctx context.Context, ownerUserID, keyID int) (bool, error) {
	exists, err := s.queries.KeyBelongsToUser(ctx, sqlc.KeyBelongsToUserParams{KeyID: int64(keyID), OwnerUserID: int64(ownerUserID)})
	return exists, mapError(err)
}

func (s *Store) ListQuotaPolicies(ctx context.Context, filter domain.QuotaPolicyFilter) (domain.ListResponse[domain.QuotaPolicyDTO], error) {
	limit, offset := limitOffset(filter.Page, filter.PageSize)
	var enabled pgtype.Bool
	if filter.Enabled != nil {
		enabled = pgtype.Bool{Bool: *filter.Enabled, Valid: true}
	}
	rows, err := s.queries.ListQuotaPolicies(ctx, sqlc.ListQuotaPoliciesParams{
		OwnerUserID: int64(filter.OwnerUserID), ScopeType: string(filter.ScopeType), ScopeID: int64(filter.ScopeID),
		Enabled: enabled, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return domain.ListResponse[domain.QuotaPolicyDTO]{}, mapError(err)
	}
	list := []domain.QuotaPolicyDTO{}
	for _, row := range rows {
		scopeID := optionalInt(row.UserID)
		if row.ScopeType == string(domain.QuotaScopeAPIKey) {
			scopeID = optionalInt(row.ApiKeyID)
		}
		if scopeID == nil {
			return domain.ListResponse[domain.QuotaPolicyDTO]{}, fmt.Errorf("%w: quota scope missing", store.ErrInvalid)
		}
		var tokenLimit *int64
		if row.TokenLimit.Valid {
			value := row.TokenLimit.Int64
			tokenLimit = &value
		}
		list = append(list, domain.QuotaPolicyToDTO(domain.QuotaPolicy{
			ID: int(row.ID), PolicyName: row.PolicyName, ScopeType: domain.QuotaScopeType(row.ScopeType),
			ScopeID: *scopeID, PeriodType: domain.QuotaPeriodType(row.PeriodType), TokenLimit: tokenLimit,
			CostLimit: optionalString(row.CostLimit), Enabled: row.Enabled,
		}))
	}
	total, err := s.queries.CountQuotaPolicies(ctx, sqlc.CountQuotaPoliciesParams{
		OwnerUserID: int64(filter.OwnerUserID), ScopeType: string(filter.ScopeType), ScopeID: int64(filter.ScopeID), Enabled: enabled,
	})
	if err != nil {
		return domain.ListResponse[domain.QuotaPolicyDTO]{}, mapError(err)
	}
	return domain.ListResponse[domain.QuotaPolicyDTO]{List: list, Total: int(total)}, nil
}

func (s *Store) ListQuotaUsage(ctx context.Context, filter domain.QuotaPolicyFilter) (domain.ListResponse[domain.QuotaUsageDTO], error) {
	limit, offset := limitOffset(filter.Page, filter.PageSize)
	rows, err := s.queries.ListQuotaUsage(ctx, sqlc.ListQuotaUsageParams{
		OwnerUserID: int64(filter.OwnerUserID), ScopeType: string(filter.ScopeType), ScopeID: int64(filter.ScopeID),
		PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return domain.ListResponse[domain.QuotaUsageDTO]{}, mapError(err)
	}
	list := []domain.QuotaUsageDTO{}
	for _, row := range rows {
		scopeID := optionalInt(row.UserID)
		if row.ScopeType == string(domain.QuotaScopeAPIKey) {
			scopeID = optionalInt(row.ApiKeyID)
		}
		if scopeID == nil {
			return domain.ListResponse[domain.QuotaUsageDTO]{}, fmt.Errorf("%w: quota scope missing", store.ErrInvalid)
		}
		item := domain.QuotaUsageDTO{
			PolicyID: int(row.PolicyID), PolicyName: row.PolicyName, ScopeType: domain.QuotaScopeType(row.ScopeType),
			ScopeID: *scopeID, PeriodType: domain.QuotaPeriodType(row.PeriodType),
			PeriodStart: row.PeriodStart.Time.UTC().Format(time.RFC3339), PeriodEnd: row.PeriodEnd.Time.UTC().Format(time.RFC3339),
			UsedTokens: row.UsedTokens, ReservedTokens: row.ReservedTokens,
			CostLimit: optionalString(row.CostLimit), UsedCost: row.UsedCost, ReservedCost: row.ReservedCost,
		}
		if row.TokenLimit.Valid {
			value := row.TokenLimit.Int64
			item.TokenLimit = &value
		}
		list = append(list, item)
	}
	total, err := s.queries.CountQuotaUsage(ctx, sqlc.CountQuotaUsageParams{
		OwnerUserID: int64(filter.OwnerUserID), ScopeType: string(filter.ScopeType), ScopeID: int64(filter.ScopeID),
	})
	if err != nil {
		return domain.ListResponse[domain.QuotaUsageDTO]{}, mapError(err)
	}
	return domain.ListResponse[domain.QuotaUsageDTO]{List: list, Total: int(total)}, nil
}

// --- Tx primitives ---

func (t *Tx) ApplicablePolicies(userID, keyID int) ([]domain.QuotaPolicy, error) {
	rows, err := applicableQuotaPolicies(t.ctx, t.tx, userID, keyID)
	if err != nil {
		return nil, err
	}
	policies := make([]domain.QuotaPolicy, 0, len(rows))
	for _, row := range rows {
		policies = append(policies, domain.QuotaPolicy{
			ID:         int(row.id),
			PolicyName: row.name,
			ScopeType:  row.scopeType,
			ScopeID:    row.scopeID,
			PeriodType: row.periodType,
			TokenLimit: row.tokenLimit,
			CostLimit:  row.costLimit,
			Enabled:    row.enabled,
		})
	}
	return policies, nil
}

func (t *Tx) ReapExpired(now time.Time, limit, userID, keyID int) (int, error) {
	return reapExpiredQuotaTx(t.ctx, t.tx, now, limit, userID, keyID)
}

func (t *Tx) InsertReservation(in domain.QuotaReservationInsert) (int64, error) {
	reservationID, err := sqlc.New(t.tx).InsertQuotaReservation(t.ctx, sqlc.InsertQuotaReservationParams{
		RequestID: in.RequestID, UserID: int64(in.UserID), ApiKeyID: int64(in.APIKeyID), Model: in.Model,
		EstimatedTokens: in.EstimatedTokens, EstimatedCost: in.EstimatedCost,
		ExpiresAt: pgtype.Timestamptz{Time: in.ExpiresAt.UTC(), Valid: true},
	})
	if err != nil {
		return 0, mapError(err)
	}
	return reservationID, nil
}

func (t *Tx) UpsertBucket(policyID int, start, end time.Time) error {
	err := sqlc.New(t.tx).UpsertQuotaBucket(t.ctx, sqlc.UpsertQuotaBucketParams{
		PolicyID: int64(policyID), PeriodStart: pgtype.Timestamptz{Time: start, Valid: true},
		PeriodEnd: pgtype.Timestamptz{Time: end, Valid: true},
	})
	return mapError(err)
}

func (t *Tx) LockBucket(policyID int, start time.Time) error {
	_, err := sqlc.New(t.tx).LockQuotaBucket(t.ctx, sqlc.LockQuotaBucketParams{
		PolicyID: int64(policyID), PeriodStart: pgtype.Timestamptz{Time: start, Valid: true},
	})
	return mapError(err)
}

func (t *Tx) ReserveBucket(policyID int, start time.Time, tokens int64, cost string) (bool, error) {
	result, err := sqlc.New(t.tx).ReserveQuotaBucket(t.ctx, sqlc.ReserveQuotaBucketParams{
		PolicyID: int64(policyID), PeriodStart: pgtype.Timestamptz{Time: start, Valid: true},
		Tokens: tokens, Cost: cost,
	})
	if err != nil {
		return false, mapError(err)
	}
	return result == 1, nil
}

func (t *Tx) InsertReservationItem(reservationID int64, policyID int, start time.Time, tokens int64, cost string) error {
	err := sqlc.New(t.tx).InsertQuotaReservationItem(t.ctx, sqlc.InsertQuotaReservationItemParams{
		ReservationID: reservationID, PolicyID: int64(policyID), PeriodStart: pgtype.Timestamptz{Time: start, Valid: true},
		Tokens: tokens, Cost: cost,
	})
	return mapError(err)
}

func (t *Tx) ReleaseReservation(reservationID int64, status string, now time.Time) error {
	return releaseQuotaTx(t.ctx, t.tx, reservationID, status, now)
}

// --- helpers ---

func applicableQuotaPolicies(ctx context.Context, tx pgx.Tx, userID, keyID int) ([]quotaPolicyRow, error) {
	rows, err := sqlc.New(tx).ApplicableQuotaPolicies(ctx, sqlc.ApplicableQuotaPoliciesParams{UserID: int64(userID), KeyID: int64(keyID)})
	if err != nil {
		return nil, mapError(err)
	}
	var result []quotaPolicyRow
	for _, item := range rows {
		row := quotaPolicyRow{
			id: item.ID, name: item.PolicyName, scopeType: domain.QuotaScopeType(item.ScopeType), scopeID: int(item.ScopeID),
			periodType: domain.QuotaPeriodType(item.PeriodType), costLimit: optionalString(item.CostLimit), enabled: item.Enabled,
		}
		if item.TokenLimit.Valid {
			value := item.TokenLimit.Int64
			row.tokenLimit = &value
		}
		result = append(result, row)
	}
	return result, nil
}

func releaseQuotaTx(ctx context.Context, tx pgx.Tx, reservationID int64, status string, now time.Time) error {
	queries := sqlc.New(tx)
	current, err := queries.LockQuotaReservationStatus(ctx, reservationID)
	if err != nil {
		return mapError(err)
	}
	if current != "pending" {
		return nil
	}
	items, err := quotaReservationItems(ctx, tx, reservationID)
	if err != nil {
		return err
	}
	for _, item := range items {
		result, err := queries.ReleaseQuotaBucket(ctx, sqlc.ReleaseQuotaBucketParams{
			PolicyID: item.policyID, PeriodStart: pgtype.Timestamptz{Time: item.periodStart, Valid: true},
			Tokens: item.reservedToken, Cost: item.reservedCost,
		})
		if err != nil {
			return mapError(err)
		}
		if result != 1 {
			return fmt.Errorf("%w: invalid quota bucket reservation", store.ErrInvalid)
		}
	}
	err = queries.ReleaseQuotaReservation(ctx, sqlc.ReleaseQuotaReservationParams{
		ID: reservationID, Status: status, ReleasedAt: pgtype.Timestamptz{Time: now.UTC(), Valid: true},
	})
	return mapError(err)
}

func reapExpiredQuotaTx(ctx context.Context, tx pgx.Tx, now time.Time, limit, userID, keyID int) (int, error) {
	ids, err := sqlc.New(tx).LockExpiredQuotaReservations(ctx, sqlc.LockExpiredQuotaReservationsParams{
		ExpiresAt: pgtype.Timestamptz{Time: now.UTC(), Valid: true}, UserID: int64(userID), KeyID: int64(keyID), RowLimit: int64(limit),
	})
	if err != nil {
		return 0, mapError(err)
	}
	for _, id := range ids {
		if err := releaseQuotaTx(ctx, tx, id, "expired", now); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func quotaReservationItems(ctx context.Context, tx pgx.Tx, reservationID int64) ([]quotaItemRow, error) {
	rows, err := sqlc.New(tx).LockQuotaReservationItems(ctx, reservationID)
	if err != nil {
		return nil, mapError(err)
	}
	var items []quotaItemRow
	for _, row := range rows {
		items = append(items, quotaItemRow{
			policyID: row.PolicyID, periodStart: row.PeriodStart.Time, reservedToken: row.ReservedTokens, reservedCost: row.ReservedCost,
		})
	}
	return items, nil
}

func settleQuotaTx(ctx context.Context, tx pgx.Tx, reservationID int64, requestID string, userID, keyID int, actualTokens int64, actualCost string, now time.Time) error {
	if reservationID == 0 {
		return nil
	}
	queries := sqlc.New(tx)
	reservation, err := queries.LockQuotaReservationIdentity(ctx, reservationID)
	if err != nil {
		return mapError(err)
	}
	status := reservation.Status
	if status == "settled" {
		return fmt.Errorf("%w: quota reservation already settled", store.ErrInvalid)
	}
	if status != "pending" {
		return fmt.Errorf("%w: quota reservation is %s", store.ErrInvalid, status)
	}
	if reservation.RequestID != requestID || reservation.UserID != int64(userID) || reservation.ApiKeyID != int64(keyID) {
		return fmt.Errorf("%w: quota reservation identity mismatch", store.ErrInvalid)
	}
	items, err := quotaReservationItems(ctx, tx, reservationID)
	if err != nil {
		return err
	}
	for _, item := range items {
		result, err := queries.SettleQuotaBucket(ctx, sqlc.SettleQuotaBucketParams{
			PolicyID: item.policyID, PeriodStart: pgtype.Timestamptz{Time: item.periodStart, Valid: true},
			ReservedTokens: item.reservedToken, ReservedCost: item.reservedCost, ActualTokens: actualTokens, ActualCost: actualCost,
		})
		if err != nil {
			return mapError(err)
		}
		if result != 1 {
			return fmt.Errorf("%w: invalid quota bucket settlement", store.ErrInvalid)
		}
		if err := queries.SettleQuotaReservationItem(ctx, sqlc.SettleQuotaReservationItemParams{
			ReservationID: reservationID, PolicyID: item.policyID, ActualTokens: pgtype.Int8{Int64: actualTokens, Valid: true}, ActualCost: actualCost,
		}); err != nil {
			return mapError(err)
		}
	}
	err = queries.SettleQuotaReservation(ctx, sqlc.SettleQuotaReservationParams{
		ID: reservationID, ActualTokens: pgtype.Int8{Int64: actualTokens, Valid: true}, ActualCost: actualCost,
		SettledAt: pgtype.Timestamptz{Time: now.UTC(), Valid: true},
	})
	return mapError(err)
}

func cleanupQuotaReservationsTx(ctx context.Context, tx pgx.Tx, userID, keyID int) error {
	queries := sqlc.New(tx)
	if err := queries.LockQuotaPoliciesForCleanup(ctx, sqlc.LockQuotaPoliciesForCleanupParams{UserID: int64(userID), KeyID: int64(keyID)}); err != nil {
		return mapError(err)
	}
	ids, err := queries.LockQuotaReservationsForCleanup(ctx, sqlc.LockQuotaReservationsForCleanupParams{UserID: int64(userID), KeyID: int64(keyID)})
	if err != nil {
		return mapError(err)
	}
	for _, id := range ids {
		if err := releaseQuotaTx(ctx, tx, id, "released", time.Now()); err != nil {
			return err
		}
	}
	if keyID > 0 {
		err = queries.DeleteKeyQuotaReservations(ctx, sqlc.DeleteKeyQuotaReservationsParams{KeyID: int64(keyID), UserID: int64(userID)})
	} else {
		err = queries.DeleteUserQuotaReservations(ctx, int64(userID))
	}
	return mapError(err)
}

func numericValue(value *string) pgtype.Numeric {
	if value == nil || *value == "" {
		return pgtype.Numeric{}
	}
	var numeric pgtype.Numeric
	_ = numeric.Scan(*value)
	return numeric
}

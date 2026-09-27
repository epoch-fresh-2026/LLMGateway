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
		CostLimit:  optionalString(row.CostLimit),
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

func (s *Store) UpdateQuotaPolicyRecord(ctx context.Context, ownerUserID, id int, policy domain.QuotaPolicy) (bool, error) {
	params := sqlc.UpdateQuotaPolicyParams{
		ID:          int64(id),
		OwnerUserID: pgtype.Int8{Int64: int64(ownerUserID), Valid: true},
		PolicyName:  policy.PolicyName,
		Enabled:     policy.Enabled,
		CostLimit:   numericValue(policy.CostLimit),
	}
	if policy.TokenLimit != nil {
		params.TokenLimit = pgtype.Int8{Int64: *policy.TokenLimit, Valid: true}
	}
	affected, err := s.queries.UpdateQuotaPolicy(ctx, params)
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (s *Store) DeleteQuotaPolicy(ctx context.Context, ownerUserID, id int) (bool, error) {
	affected, err := s.queries.DeleteQuotaPolicy(ctx, sqlc.DeleteQuotaPolicyParams{ID: int64(id), OwnerUserID: pgtype.Int8{Int64: int64(ownerUserID), Valid: true}})
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (s *Store) KeyBelongsToUser(ctx context.Context, ownerUserID, keyID int) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM client_api_keys WHERE id = $1 AND user_id = $2)`, keyID, ownerUserID).Scan(&exists); err != nil {
		return false, mapError(err)
	}
	return exists, nil
}

func (s *Store) ListQuotaPolicies(ctx context.Context, filter domain.QuotaPolicyFilter) (domain.ListResponse[domain.QuotaPolicyDTO], error) {
	limit, offset := limitOffset(filter.Page, filter.PageSize)
	rows, err := s.pool.Query(ctx, `
SELECT id, policy_name, scope_type, COALESCE(user_id, api_key_id), period_type,
       token_limit, cost_limit::text, enabled
FROM quota_policies
WHERE deleted_at IS NULL
  AND ((quota_policies.scope_type = 'user' AND quota_policies.user_id = $1)
       OR (quota_policies.scope_type = 'api_key' AND quota_policies.api_key_id IN (
           SELECT k.id FROM client_api_keys k WHERE k.user_id = $1)))
  AND ($2 = '' OR scope_type = $2)
  AND ($3 = 0 OR COALESCE(user_id, api_key_id) = $3)
  AND ($4::boolean IS NULL OR enabled = $4)
ORDER BY id
LIMIT $5 OFFSET $6`, filter.OwnerUserID, filter.ScopeType, filter.ScopeID, filter.Enabled, limit, offset)
	if err != nil {
		return domain.ListResponse[domain.QuotaPolicyDTO]{}, mapError(err)
	}
	defer rows.Close()
	list := []domain.QuotaPolicyDTO{}
	for rows.Next() {
		var row quotaPolicyRow
		var scopeType, periodType string
		var token pgtype.Int8
		var cost pgtype.Text
		if err := rows.Scan(&row.id, &row.name, &scopeType, &row.scopeID, &periodType, &token, &cost, &row.enabled); err != nil {
			return domain.ListResponse[domain.QuotaPolicyDTO]{}, mapError(err)
		}
		row.scopeType, row.periodType = domain.QuotaScopeType(scopeType), domain.QuotaPeriodType(periodType)
		if token.Valid {
			value := token.Int64
			row.tokenLimit = &value
		}
		row.costLimit = optionalString(textOrEmpty(cost))
		list = append(list, domain.QuotaPolicyToDTO(domain.QuotaPolicy{ID: int(row.id), PolicyName: row.name, ScopeType: row.scopeType, ScopeID: row.scopeID, PeriodType: row.periodType, TokenLimit: row.tokenLimit, CostLimit: row.costLimit, Enabled: row.enabled}))
	}
	if err := rows.Err(); err != nil {
		return domain.ListResponse[domain.QuotaPolicyDTO]{}, mapError(err)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM quota_policies WHERE deleted_at IS NULL AND ((quota_policies.scope_type = 'user' AND quota_policies.user_id = $1) OR (quota_policies.scope_type = 'api_key' AND quota_policies.api_key_id IN (SELECT k.id FROM client_api_keys k WHERE k.user_id = $1))) AND ($2 = '' OR scope_type = $2) AND ($3 = 0 OR COALESCE(user_id, api_key_id) = $3) AND ($4::boolean IS NULL OR enabled = $4)`, filter.OwnerUserID, filter.ScopeType, filter.ScopeID, filter.Enabled).Scan(&total); err != nil {
		return domain.ListResponse[domain.QuotaPolicyDTO]{}, mapError(err)
	}
	return domain.ListResponse[domain.QuotaPolicyDTO]{List: list, Total: total}, nil
}

func (s *Store) ListQuotaUsage(ctx context.Context, filter domain.QuotaPolicyFilter) (domain.ListResponse[domain.QuotaUsageDTO], error) {
	limit, offset := limitOffset(filter.Page, filter.PageSize)
	rows, err := s.pool.Query(ctx, `
SELECT p.id, p.policy_name, p.scope_type, COALESCE(p.user_id, p.api_key_id), p.period_type,
       b.period_start, b.period_end, p.token_limit, b.used_tokens, b.reserved_tokens,
       p.cost_limit::text, b.used_cost::text, b.reserved_cost::text
FROM quota_policies p JOIN quota_buckets b ON b.policy_id = p.id
WHERE p.deleted_at IS NULL
  AND ((p.scope_type = 'user' AND p.user_id = $1)
       OR (p.scope_type = 'api_key' AND p.api_key_id IN (SELECT k.id FROM client_api_keys k WHERE k.user_id = $1)))
  AND ($2 = '' OR p.scope_type = $2) AND ($3 = 0 OR COALESCE(p.user_id, p.api_key_id) = $3)
  AND b.period_start <= now() AND b.period_end > now()
ORDER BY p.id
LIMIT $4 OFFSET $5`, filter.OwnerUserID, filter.ScopeType, filter.ScopeID, limit, offset)
	if err != nil {
		return domain.ListResponse[domain.QuotaUsageDTO]{}, mapError(err)
	}
	defer rows.Close()
	list := []domain.QuotaUsageDTO{}
	for rows.Next() {
		var item domain.QuotaUsageDTO
		var scopeType, periodType string
		var start, end time.Time
		var token pgtype.Int8
		var cost pgtype.Text
		if err := rows.Scan(&item.PolicyID, &item.PolicyName, &scopeType, &item.ScopeID, &periodType, &start, &end, &token, &item.UsedTokens, &item.ReservedTokens, &cost, &item.UsedCost, &item.ReservedCost); err != nil {
			return domain.ListResponse[domain.QuotaUsageDTO]{}, mapError(err)
		}
		item.ScopeType, item.PeriodType = domain.QuotaScopeType(scopeType), domain.QuotaPeriodType(periodType)
		item.PeriodStart, item.PeriodEnd = start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339)
		if token.Valid {
			value := token.Int64
			item.TokenLimit = &value
		}
		item.CostLimit = optionalString(textOrEmpty(cost))
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return domain.ListResponse[domain.QuotaUsageDTO]{}, mapError(err)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM quota_policies p JOIN quota_buckets b ON b.policy_id=p.id WHERE p.deleted_at IS NULL AND ((p.scope_type='user' AND p.user_id=$1) OR (p.scope_type='api_key' AND p.api_key_id IN (SELECT k.id FROM client_api_keys k WHERE k.user_id=$1))) AND ($2='' OR p.scope_type=$2) AND ($3=0 OR COALESCE(p.user_id,p.api_key_id)=$3) AND b.period_start <= now() AND b.period_end > now()`, filter.OwnerUserID, filter.ScopeType, filter.ScopeID).Scan(&total); err != nil {
		return domain.ListResponse[domain.QuotaUsageDTO]{}, mapError(err)
	}
	return domain.ListResponse[domain.QuotaUsageDTO]{List: list, Total: total}, nil
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
	var reservationID int64
	err := t.tx.QueryRow(t.ctx, `INSERT INTO quota_reservations (request_id, user_id, api_key_id, model, estimated_tokens, estimated_cost, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, in.RequestID, in.UserID, in.APIKeyID, in.Model, in.EstimatedTokens, in.EstimatedCost, in.ExpiresAt.UTC()).Scan(&reservationID)
	if err != nil {
		return 0, mapError(err)
	}
	return reservationID, nil
}

func (t *Tx) UpsertBucket(policyID int, start, end time.Time) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO quota_buckets (policy_id, period_start, period_end) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, policyID, start, end)
	return mapError(err)
}

func (t *Tx) LockBucket(policyID int, start time.Time) error {
	var lockedPolicyID int64
	return mapError(t.tx.QueryRow(t.ctx, `SELECT policy_id FROM quota_buckets WHERE policy_id=$1 AND period_start=$2 FOR UPDATE`, policyID, start).Scan(&lockedPolicyID))
}

func (t *Tx) ReserveBucket(policyID int, start time.Time, tokens int64, cost string) (bool, error) {
	result, err := t.tx.Exec(t.ctx, `
UPDATE quota_buckets b
SET reserved_tokens = b.reserved_tokens + $3,
    reserved_cost = b.reserved_cost + $4,
    updated_at = now()
FROM quota_policies p
WHERE b.policy_id = $1 AND b.period_start = $2 AND p.id = b.policy_id
  AND p.enabled = true AND p.deleted_at IS NULL
  AND (p.token_limit IS NULL OR b.used_tokens + b.reserved_tokens + $3 <= p.token_limit)
  AND (p.cost_limit IS NULL OR b.used_cost + b.reserved_cost + $4 <= p.cost_limit)`, policyID, start, tokens, cost)
	if err != nil {
		return false, mapError(err)
	}
	return result.RowsAffected() == 1, nil
}

func (t *Tx) InsertReservationItem(reservationID int64, policyID int, start time.Time, tokens int64, cost string) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO quota_reservation_items (reservation_id, policy_id, period_start, reserved_tokens, reserved_cost) VALUES ($1,$2,$3,$4,$5)`, reservationID, policyID, start, tokens, cost)
	return mapError(err)
}

func (t *Tx) ReleaseReservation(reservationID int64, status string, now time.Time) error {
	return releaseQuotaTx(t.ctx, t.tx, reservationID, status, now)
}

// --- helpers ---

func applicableQuotaPolicies(ctx context.Context, tx pgx.Tx, userID, keyID int) ([]quotaPolicyRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, policy_name, scope_type, COALESCE(user_id, api_key_id), period_type, token_limit, cost_limit::text, enabled FROM quota_policies WHERE deleted_at IS NULL AND enabled=true AND ((scope_type='user' AND user_id=$1) OR (scope_type='api_key' AND api_key_id=$2)) ORDER BY id FOR SHARE`, userID, keyID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []quotaPolicyRow
	for rows.Next() {
		var row quotaPolicyRow
		var scopeType, periodType string
		var token pgtype.Int8
		var cost pgtype.Text
		if err := rows.Scan(&row.id, &row.name, &scopeType, &row.scopeID, &periodType, &token, &cost, &row.enabled); err != nil {
			return nil, mapError(err)
		}
		row.scopeType, row.periodType = domain.QuotaScopeType(scopeType), domain.QuotaPeriodType(periodType)
		if token.Valid {
			value := token.Int64
			row.tokenLimit = &value
		}
		row.costLimit = optionalString(textOrEmpty(cost))
		result = append(result, row)
	}
	return result, mapError(rows.Err())
}

func releaseQuotaTx(ctx context.Context, tx pgx.Tx, reservationID int64, status string, now time.Time) error {
	var current string
	if err := tx.QueryRow(ctx, `SELECT status FROM quota_reservations WHERE id=$1 FOR UPDATE`, reservationID).Scan(&current); err != nil {
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
		result, err := tx.Exec(ctx, `UPDATE quota_buckets SET reserved_tokens=reserved_tokens-$3, reserved_cost=reserved_cost-$4, updated_at=now() WHERE policy_id=$1 AND period_start=$2 AND reserved_tokens >= $3 AND reserved_cost >= $4`, item.policyID, item.periodStart, item.reservedToken, item.reservedCost)
		if err != nil {
			return mapError(err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("%w: invalid quota bucket reservation", store.ErrInvalid)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE quota_reservations SET status=$2, released_at=$3, updated_at=now() WHERE id=$1`, reservationID, status, now.UTC())
	return mapError(err)
}

func reapExpiredQuotaTx(ctx context.Context, tx pgx.Tx, now time.Time, limit, userID, keyID int) (int, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM quota_reservations WHERE status='pending' AND expires_at <= $1 AND ($2=0 OR user_id=$2 OR api_key_id=$3) ORDER BY expires_at,id FOR UPDATE SKIP LOCKED LIMIT $4`, now.UTC(), userID, keyID, limit)
	if err != nil {
		return 0, mapError(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, mapError(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := releaseQuotaTx(ctx, tx, id, "expired", now); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func quotaReservationItems(ctx context.Context, tx pgx.Tx, reservationID int64) ([]quotaItemRow, error) {
	rows, err := tx.Query(ctx, `SELECT policy_id, period_start, reserved_tokens, reserved_cost::text FROM quota_reservation_items WHERE reservation_id=$1 ORDER BY policy_id FOR UPDATE`, reservationID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var items []quotaItemRow
	for rows.Next() {
		var item quotaItemRow
		if err := rows.Scan(&item.policyID, &item.periodStart, &item.reservedToken, &item.reservedCost); err != nil {
			return nil, mapError(err)
		}
		items = append(items, item)
	}
	return items, mapError(rows.Err())
}

func settleQuotaTx(ctx context.Context, tx pgx.Tx, reservationID int64, requestID string, userID, keyID int, actualTokens int64, actualCost string, now time.Time) error {
	if reservationID == 0 {
		return nil
	}
	var status, storedRequestID string
	var storedUserID, storedKeyID int
	if err := tx.QueryRow(ctx, `SELECT status, request_id, user_id, api_key_id FROM quota_reservations WHERE id=$1 FOR UPDATE`, reservationID).Scan(&status, &storedRequestID, &storedUserID, &storedKeyID); err != nil {
		return mapError(err)
	}
	if status == "settled" {
		return fmt.Errorf("%w: quota reservation already settled", store.ErrInvalid)
	}
	if status != "pending" {
		return fmt.Errorf("%w: quota reservation is %s", store.ErrInvalid, status)
	}
	if storedRequestID != requestID || storedUserID != userID || storedKeyID != keyID {
		return fmt.Errorf("%w: quota reservation identity mismatch", store.ErrInvalid)
	}
	items, err := quotaReservationItems(ctx, tx, reservationID)
	if err != nil {
		return err
	}
	for _, item := range items {
		result, err := tx.Exec(ctx, `UPDATE quota_buckets SET reserved_tokens=reserved_tokens-$3, reserved_cost=reserved_cost-$4, used_tokens=used_tokens+$5, used_cost=used_cost+$6, updated_at=now() WHERE policy_id=$1 AND period_start=$2 AND reserved_tokens >= $3 AND reserved_cost >= $4 AND $5 <= $3 AND $6 <= $4`, item.policyID, item.periodStart, item.reservedToken, item.reservedCost, actualTokens, actualCost)
		if err != nil {
			return mapError(err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("%w: invalid quota bucket settlement", store.ErrInvalid)
		}
		if _, err := tx.Exec(ctx, `UPDATE quota_reservation_items SET actual_tokens=$3, actual_cost=$4 WHERE reservation_id=$1 AND policy_id=$2`, reservationID, item.policyID, actualTokens, actualCost); err != nil {
			return mapError(err)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE quota_reservations SET status='settled', actual_tokens=$2, actual_cost=$3, settled_at=$4, updated_at=now() WHERE id=$1`, reservationID, actualTokens, actualCost, now.UTC())
	return mapError(err)
}

func cleanupQuotaReservationsTx(ctx context.Context, tx pgx.Tx, userID, keyID int) error {
	if _, err := tx.Exec(ctx, `SELECT id FROM quota_policies WHERE deleted_at IS NULL AND (($1 > 0 AND scope_type='user' AND user_id=$1) OR ($2 > 0 AND scope_type='api_key' AND api_key_id=$2) OR ($1 > 0 AND scope_type='api_key' AND api_key_id IN (SELECT id FROM client_api_keys WHERE user_id=$1))) ORDER BY id FOR UPDATE`, userID, keyID); err != nil {
		return mapError(err)
	}
	rows, err := tx.Query(ctx, `SELECT id FROM quota_reservations WHERE ($1=0 OR user_id=$1) AND ($2=0 OR api_key_id=$2) ORDER BY id FOR UPDATE`, userID, keyID)
	if err != nil {
		return mapError(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return mapError(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := releaseQuotaTx(ctx, tx, id, "released", time.Now()); err != nil {
			return err
		}
	}
	if keyID > 0 {
		_, err = tx.Exec(ctx, `DELETE FROM quota_reservations WHERE api_key_id=$1 AND ($2=0 OR user_id=$2)`, keyID, userID)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM quota_reservations WHERE user_id=$1`, userID)
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

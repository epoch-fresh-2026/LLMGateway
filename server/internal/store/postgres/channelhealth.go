package postgres

import (
	"context"
	"errors"
	"time"

	domain "LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/db/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) GetChannelHealthRow(ctx context.Context, channelID int) (domain.ChannelHealth, bool, error) {
	row, err := s.queries.GetChannelHealth(ctx, int64(channelID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ChannelHealth{}, false, nil
		}
		return domain.ChannelHealth{}, false, mapError(err)
	}
	return channelHealthFromRow(row), true, nil
}

func (s *Store) ListChannelHealthRows(ctx context.Context, ownerUserID int) ([]domain.ChannelHealth, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id, COALESCE(h.state,'closed'), COALESCE(h.consecutive_failures,0), COALESCE(h.success_count,0), COALESCE(h.failure_count,0), h.opened_at, COALESCE(h.updated_at,c.updated_at) FROM channels c LEFT JOIN channel_health h ON h.channel_id=c.id WHERE c.owner_user_id=$1 ORDER BY c.id`, int64(ownerUserID))
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	list := []domain.ChannelHealth{}
	for rows.Next() {
		var h domain.ChannelHealth
		var opened, updated pgtype.Timestamptz
		if err := rows.Scan(&h.ChannelID, &h.State, &h.ConsecutiveFailures, &h.SuccessCount, &h.FailureCount, &opened, &updated); err != nil {
			return nil, mapError(err)
		}
		h.OpenedAt = optionalTimestamp(opened)
		h.UpdatedAt = updated.Time.UTC().Format(time.RFC3339)
		list = append(list, h)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return list, nil
}

func (s *Store) AcquireChannelProbe(ctx context.Context, channelID int, lease time.Duration) (string, bool, error) {
	var leaseID string
	err := s.pool.QueryRow(ctx, `INSERT INTO channel_breaker_probes(channel_id, lease_id, leased_until) VALUES($1, gen_random_uuid(), now()+$2::int*interval '1 second') ON CONFLICT(channel_id) DO UPDATE SET lease_id=gen_random_uuid(), leased_until=EXCLUDED.leased_until WHERE channel_breaker_probes.leased_until <= now() RETURNING lease_id`, channelID, int(lease.Seconds())).Scan(&leaseID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, mapError(err)
	}
	return leaseID, true, nil
}

func (s *Store) ReleaseChannelProbe(ctx context.Context, channelID int, leaseID string) (bool, error) {
	result, err := s.pool.Exec(ctx, `DELETE FROM channel_breaker_probes WHERE channel_id=$1 AND lease_id=$2`, channelID, leaseID)
	if err != nil {
		return false, mapError(err)
	}
	return result.RowsAffected() > 0, nil
}

func (s *Store) GetChannelBreakerConfigRow(ctx context.Context, channelID int) (domain.ChannelBreakerConfig, bool, error) {
	row, err := s.queries.GetChannelBreakerConfig(ctx, int64(channelID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ChannelBreakerConfig{}, false, nil
		}
		return domain.ChannelBreakerConfig{}, false, mapError(err)
	}
	return channelBreakerConfig(row.WindowSeconds, row.MinimumSamples, row.ErrorRatePercent, row.TimeoutRatePercent, row.CooldownSeconds), true, nil
}

func (s *Store) GetUserBreakerConfigRow(ctx context.Context, ownerUserID int) (domain.ChannelBreakerConfig, bool, error) {
	row, err := s.queries.GetUserBreakerConfig(ctx, int64(ownerUserID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ChannelBreakerConfig{}, false, nil
		}
		return domain.ChannelBreakerConfig{}, false, mapError(err)
	}
	return channelBreakerConfig(row.WindowSeconds, row.MinimumSamples, row.ErrorRatePercent, row.TimeoutRatePercent, row.CooldownSeconds), true, nil
}

func (s *Store) ListChannelBreakerConfigRows(ctx context.Context, ownerUserID int) (map[int]domain.ChannelBreakerConfig, error) {
	rows, err := s.queries.ListChannelBreakerConfigs(ctx, int64(ownerUserID))
	if err != nil {
		return nil, mapError(err)
	}
	result := make(map[int]domain.ChannelBreakerConfig, len(rows))
	for _, row := range rows {
		result[int(row.ChannelID)] = channelBreakerConfig(row.WindowSeconds, row.MinimumSamples, row.ErrorRatePercent, row.TimeoutRatePercent, row.CooldownSeconds)
	}
	return result, nil
}

func (s *Store) DeleteStaleChannelHealthBuckets(ctx context.Context, before time.Time) (int, error) {
	affected, err := s.queries.DeleteStaleChannelHealthBuckets(ctx, pgtype.Timestamptz{Time: before.UTC(), Valid: true})
	if err != nil {
		return 0, mapError(err)
	}
	return int(affected), nil
}

func (t *Tx) UpsertChannelHealthBucket(channelID int, bucketStart time.Time, requests, errors, timeouts int64) error {
	return mapError(t.queries.UpsertChannelHealthBucket(t.ctx, sqlc.UpsertChannelHealthBucketParams{
		ChannelID:   int64(channelID),
		BucketStart: pgtype.Timestamptz{Time: bucketStart.UTC(), Valid: true},
		Requests:    requests,
		Errors:      errors,
		Timeouts:    timeouts,
	}))
}

func (t *Tx) GetChannelHealthWindow(channelID int, since time.Time) (domain.ChannelHealthWindow, error) {
	row, err := t.queries.SumChannelHealthWindow(t.ctx, sqlc.SumChannelHealthWindowParams{
		ChannelID: int64(channelID),
		Since:     pgtype.Timestamptz{Time: since.UTC(), Valid: true},
	})
	if err != nil {
		return domain.ChannelHealthWindow{}, mapError(err)
	}
	return domain.ChannelHealthWindow{Requests: row.Requests, Errors: row.Errors, Timeouts: row.Timeouts}, nil
}

func (t *Tx) UpsertChannelBreakerConfig(channelID int, cfg domain.ChannelBreakerConfig) error {
	return mapError(t.queries.UpsertChannelBreakerConfig(t.ctx, sqlc.UpsertChannelBreakerConfigParams{
		ChannelID:          int64(channelID),
		WindowSeconds:      int32(cfg.WindowSeconds),
		MinimumSamples:     int32(cfg.MinimumSamples),
		ErrorRatePercent:   int32(cfg.ErrorRatePercent),
		TimeoutRatePercent: int32(cfg.TimeoutRatePercent),
		CooldownSeconds:    int32(cfg.Cooldown.Seconds()),
	}))
}

func (t *Tx) DeleteChannelBreakerConfig(channelID int) error {
	_, err := t.queries.DeleteChannelBreakerConfig(t.ctx, int64(channelID))
	return mapError(err)
}

func (t *Tx) UpsertUserBreakerConfig(ownerUserID int, cfg domain.ChannelBreakerConfig) error {
	return mapError(t.queries.UpsertUserBreakerConfig(t.ctx, sqlc.UpsertUserBreakerConfigParams{
		OwnerUserID:        int64(ownerUserID),
		WindowSeconds:      int32(cfg.WindowSeconds),
		MinimumSamples:     int32(cfg.MinimumSamples),
		ErrorRatePercent:   int32(cfg.ErrorRatePercent),
		TimeoutRatePercent: int32(cfg.TimeoutRatePercent),
		CooldownSeconds:    int32(cfg.Cooldown.Seconds()),
	}))
}

func (t *Tx) DeleteUserBreakerConfig(ownerUserID int) error {
	_, err := t.queries.DeleteUserBreakerConfig(t.ctx, int64(ownerUserID))
	return mapError(err)
}

func (t *Tx) EnsureChannelHealth(channelID int) error {
	return mapError(t.queries.EnsureChannelHealth(t.ctx, int64(channelID)))
}

func (t *Tx) GetChannelHealthForUpdate(channelID int) (domain.ChannelHealth, error) {
	row, err := t.queries.GetChannelHealthForUpdate(t.ctx, int64(channelID))
	if err != nil {
		return domain.ChannelHealth{}, mapError(err)
	}
	return channelHealthFromRow(row), nil
}

func (t *Tx) UpdateChannelHealth(health domain.ChannelHealth) (bool, error) {
	var openedAt pgtype.Timestamptz
	if health.OpenedAt != nil {
		if parsed, err := time.Parse(time.RFC3339, *health.OpenedAt); err == nil {
			openedAt = pgtype.Timestamptz{Time: parsed, Valid: true}
		}
	}
	affected, err := t.queries.UpdateChannelHealth(t.ctx, sqlc.UpdateChannelHealthParams{
		State:               string(health.State),
		ConsecutiveFailures: int32(health.ConsecutiveFailures),
		SuccessCount:        health.SuccessCount,
		FailureCount:        health.FailureCount,
		OpenedAt:            openedAt,
		ChannelID:           int64(health.ChannelID),
	})
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (t *Tx) DeleteChannelHealth(channelID int) error {
	ctx := t.ctx
	for _, table := range []string{"channel_breaker_probes", "channel_breaker_configs", "channel_health_buckets", "channel_health"} {
		if _, err := t.tx.Exec(ctx, "DELETE FROM "+table+" WHERE channel_id=$1", channelID); err != nil {
			return mapError(err)
		}
	}
	return nil
}

// channelBreakerConfig maps a per-channel override row. FailureThreshold has no
// column, so it stays zero and the catalog layer inherits the global default.
func channelBreakerConfig(windowSeconds, minimumSamples, errorRatePercent, timeoutRatePercent, cooldownSeconds int32) domain.ChannelBreakerConfig {
	return domain.ChannelBreakerConfig{
		Cooldown:           time.Duration(cooldownSeconds) * time.Second,
		WindowSeconds:      int(windowSeconds),
		MinimumSamples:     int(minimumSamples),
		ErrorRatePercent:   int(errorRatePercent),
		TimeoutRatePercent: int(timeoutRatePercent),
	}
}

func channelHealthFromRow(row sqlc.ChannelHealth) domain.ChannelHealth {
	updatedAt := ""
	if row.UpdatedAt.Valid {
		updatedAt = row.UpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	return domain.ChannelHealth{
		ChannelID:           int(row.ChannelID),
		State:               domain.HealthState(row.State),
		ConsecutiveFailures: int(row.ConsecutiveFailures),
		SuccessCount:        row.SuccessCount,
		FailureCount:        row.FailureCount,
		OpenedAt:            optionalTimestamp(row.OpenedAt),
		UpdatedAt:           updatedAt,
	}
}

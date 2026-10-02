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
	rows, err := s.queries.ListChannelHealth(ctx, int64(ownerUserID))
	if err != nil {
		return nil, mapError(err)
	}

	list := []domain.ChannelHealth{}
	for _, row := range rows {
		list = append(list, domain.ChannelHealth{
			ChannelID:           int(row.ChannelID),
			State:               domain.HealthState(row.State),
			ConsecutiveFailures: int(row.ConsecutiveFailures),
			SuccessCount:        row.SuccessCount,
			FailureCount:        row.FailureCount,
			OpenedAt:            optionalTimestamp(row.OpenedAt),
			UpdatedAt:           row.UpdatedAt.Time.UTC().Format(time.RFC3339),
		})
	}
	return list, nil
}

func (s *Store) AcquireChannelProbe(ctx context.Context, channelID int, lease time.Duration) (string, bool, error) {
	leaseID, err := s.queries.AcquireChannelProbe(ctx, sqlc.AcquireChannelProbeParams{
		ChannelID:    int64(channelID),
		LeaseSeconds: int32(lease.Seconds()),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, mapError(err)
	}
	return leaseID, true, nil
}

func (s *Store) ReleaseChannelProbe(ctx context.Context, channelID int, leaseID string) (bool, error) {
	affected, err := s.queries.ReleaseChannelProbe(ctx, sqlc.ReleaseChannelProbeParams{
		ChannelID: int64(channelID),
		LeaseID:   leaseID,
	})
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
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

func (t *Tx) DeleteChannelProbe(channelID int) error {
	return mapError(t.queries.DeleteChannelProbe(t.ctx, int64(channelID)))
}

func (t *Tx) DeleteChannelHealthBuckets(channelID int) error {
	return mapError(t.queries.DeleteChannelHealthBuckets(t.ctx, int64(channelID)))
}

func (t *Tx) ResetChannelHealthState(channelID int) error {
	return mapError(t.queries.ResetChannelHealthState(t.ctx, int64(channelID)))
}

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

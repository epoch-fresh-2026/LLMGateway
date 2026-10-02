package catalog

import (
	"context"
	"fmt"
	"time"
)

// channelHealthBucketRetention bounds how long attempt buckets are kept. It must
// exceed the largest configurable window so a scan never misses live samples.
const channelHealthBucketRetention = 10 * time.Minute

func (a *Server) GetChannelHealth(ctx context.Context, channelID int) (ChannelHealth, error) {
	row, found, err := a.health.GetChannelHealthRow(ctx, channelID)
	if err != nil {
		return ChannelHealth{}, err
	}
	if !found {
		return NewChannelHealth(channelID), nil
	}
	return EvaluateChannelHealth(row, a.now(), a.breakerFor(ctx, channelID)), nil
}

func (a *Server) RecordChannelSuccess(ctx context.Context, channelID int) (ChannelHealth, error) {
	return a.recordChannelHealth(ctx, channelID, true, "")
}

func (a *Server) RecordChannelFailure(ctx context.Context, channelID int, reason FailureReason) (ChannelHealth, error) {
	if !reason.CountsAsChannelFailure() {
		return a.GetChannelHealth(ctx, channelID)
	}
	return a.recordChannelHealth(ctx, channelID, false, reason)
}

func (a *Server) RecordChannelAttempt(ctx context.Context, channelID int, success bool, reason FailureReason) (ChannelHealth, error) {
	if success {
		return a.RecordChannelSuccess(ctx, channelID)
	}
	return a.RecordChannelFailure(ctx, channelID, reason)
}

func (a *Server) ResetChannelHealth(ctx context.Context, channelID int) error {
	return a.tx.InTx(ctx, func(tx Tx) error {
		return tx.DeleteChannelHealth(channelID)
	})
}

func (a *Server) ListChannelHealth(ctx context.Context, ownerUserID int) (ListResponse[ChannelHealthDTO], error) {
	rows, err := a.health.ListChannelHealthRows(ctx, ownerUserID)
	if err != nil {
		return ListResponse[ChannelHealthDTO]{}, err
	}
	cfg := a.baseBreakerFor(ctx, ownerUserID)
	list := []ChannelHealthDTO{}
	for i := range rows {
		health := EvaluateChannelHealth(rows[i], a.now(), cfg)
		list = append(list, channelHealthDTO(&health))
	}
	return ListResponse[ChannelHealthDTO]{List: list, Total: len(list)}, nil
}

func (a *Server) AcquireChannelProbe(ctx context.Context, channelID int, lease time.Duration) (string, bool, error) {
	return a.health.AcquireChannelProbe(ctx, channelID, lease)
}

func (a *Server) ReleaseChannelProbe(ctx context.Context, channelID int, leaseID string) (bool, error) {
	return a.health.ReleaseChannelProbe(ctx, channelID, leaseID)
}

// ReapChannelHealthBuckets removes attempt buckets older than the retention
// window. A non-positive retention falls back to the default.
func (a *Server) ReapChannelHealthBuckets(ctx context.Context, retention time.Duration) (int, error) {
	if retention <= 0 {
		retention = channelHealthBucketRetention
	}
	return a.health.DeleteStaleChannelHealthBuckets(ctx, a.now().UTC().Add(-retention))
}

// GetUserBreakerConfig returns the owner-level breaker default, falling back to
// the process default when the owner has not set one.
func (a *Server) GetUserBreakerConfig(ctx context.Context, ownerUserID int) (UserBreakerConfigDTO, error) {
	return userBreakerConfigDTO(a.baseBreakerFor(ctx, ownerUserID)), nil
}

// UpdateUserBreakerConfig stores the owner-level default and clears the breaker
// cache so every channel of the owner re-resolves on the next attempt.
func (a *Server) UpdateUserBreakerConfig(ctx context.Context, ownerUserID int, in ChannelBreakerConfigInput) (UserBreakerConfigDTO, error) {
	cfg, err := validateBreakerConfig(in)
	if err != nil {
		return UserBreakerConfigDTO{}, err
	}
	if err := a.tx.InTx(ctx, func(tx Tx) error {
		return tx.UpsertUserBreakerConfig(ownerUserID, cfg)
	}); err != nil {
		return UserBreakerConfigDTO{}, err
	}
	a.invalidateBreakerConfig(ownerUserID)
	return userBreakerConfigDTO(cfg), nil
}

// DeleteUserBreakerConfig removes the owner-level default so the owner inherits
// the process default again.
func (a *Server) DeleteUserBreakerConfig(ctx context.Context, ownerUserID int) error {
	if err := a.tx.InTx(ctx, func(tx Tx) error {
		return tx.DeleteUserBreakerConfig(ownerUserID)
	}); err != nil {
		return err
	}
	a.invalidateBreakerConfig(ownerUserID)
	return nil
}

func userBreakerConfigDTO(cfg ChannelBreakerConfig) UserBreakerConfigDTO {
	return UserBreakerConfigDTO{
		WindowSeconds:      cfg.WindowSeconds,
		MinimumSamples:     cfg.MinimumSamples,
		ErrorRatePercent:   cfg.ErrorRatePercent,
		TimeoutRatePercent: cfg.TimeoutRatePercent,
		CooldownSeconds:    int(cfg.Cooldown.Seconds()),
	}
}

func validateBreakerConfig(in ChannelBreakerConfigInput) (ChannelBreakerConfig, error) {
	if in.WindowSeconds <= 0 || in.MinimumSamples <= 0 || in.CooldownSeconds <= 0 {
		return ChannelBreakerConfig{}, fmt.Errorf("%w: window_seconds, minimum_samples and cooldown_seconds must be positive", ErrInvalid)
	}
	if in.ErrorRatePercent < 1 || in.ErrorRatePercent > 100 || in.TimeoutRatePercent < 1 || in.TimeoutRatePercent > 100 {
		return ChannelBreakerConfig{}, fmt.Errorf("%w: rate percentages must be between 1 and 100", ErrInvalid)
	}
	return ChannelBreakerConfig{
		Cooldown:           time.Duration(in.CooldownSeconds) * time.Second,
		WindowSeconds:      in.WindowSeconds,
		MinimumSamples:     in.MinimumSamples,
		ErrorRatePercent:   in.ErrorRatePercent,
		TimeoutRatePercent: in.TimeoutRatePercent,
	}, nil
}

// recordChannelHealth applies a state transition inside a transaction, holding
// the row lock so concurrent recordings cannot lose updates, and updates the
// fixed-width window bucket used by the rate-based breaker. The bucket write
// and the window sum share the transaction so the current attempt is counted.
func (a *Server) recordChannelHealth(ctx context.Context, channelID int, success bool, reason FailureReason) (ChannelHealth, error) {
	cfg := a.breakerFor(ctx, channelID)
	now := a.now()
	bucket := ChannelHealthBucketStart(now)

	var next ChannelHealth
	err := a.tx.InTx(ctx, func(tx Tx) error {
		if err := tx.EnsureChannelHealth(channelID); err != nil {
			return err
		}
		row, err := tx.GetChannelHealthForUpdate(channelID)
		if err != nil {
			return err
		}
		if success {
			if err := tx.UpsertChannelHealthBucket(channelID, bucket, 1, 0, 0); err != nil {
				return err
			}
			next = ApplyChannelSuccess(row, now)
		} else {
			timeouts := int64(0)
			if reason == FailureUpstreamTimeout {
				timeouts = 1
			}
			if err := tx.UpsertChannelHealthBucket(channelID, bucket, 1, 1, timeouts); err != nil {
				return err
			}
			since := now.UTC().Add(-time.Duration(cfg.WindowSeconds) * time.Second)
			window, err := tx.GetChannelHealthWindow(channelID, since)
			if err != nil {
				return err
			}
			next = ApplyChannelFailure(EvaluateChannelHealth(row, now, cfg), reason, window, now, cfg)
		}
		ok, err := tx.UpdateChannelHealth(next)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return ChannelHealth{}, err
	}
	return next, nil
}

func channelHealthDTO(health *ChannelHealth) ChannelHealthDTO {
	return ChannelHealthDTO{
		ChannelID:           health.ChannelID,
		State:               string(health.State),
		ConsecutiveFailures: health.ConsecutiveFailures,
		SuccessCount:        health.SuccessCount,
		FailureCount:        health.FailureCount,
		OpenedAt:            health.OpenedAt,
		UpdatedAt:           health.UpdatedAt,
	}
}

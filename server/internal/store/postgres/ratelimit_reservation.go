package postgres

import (
	"context"

	"LLMGateway/server/internal/db/sqlc"
	domain "LLMGateway/server/internal/ratelimit"

	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) InsertRateLimitReservation(ctx context.Context, in domain.RateLimitReservationInput) (int64, error) {
	id, err := s.queries.InsertRateLimitReservation(ctx, sqlc.InsertRateLimitReservationParams{
		RequestID:       in.RequestID,
		UserID:          int64(in.UserID),
		ApiKeyID:        int64(in.APIKeyID),
		Model:           in.Model,
		ChannelID:       int8Value(in.ChannelID),
		EstimatedTokens: in.EstimatedTokens,
		ExpiresAt:       pgtype.Timestamptz{Time: in.ExpiresAt.UTC(), Valid: true},
	})
	if err != nil {
		return 0, mapError(err)
	}
	return id, nil
}

func (s *Store) FinalizeRateLimitReservation(ctx context.Context, id int64) (bool, error) {
	affected, err := s.queries.FinalizeRateLimitReservation(ctx, id)
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (s *Store) ReleaseRateLimitReservation(ctx context.Context, id int64) (bool, error) {
	affected, err := s.queries.ReleaseRateLimitReservation(ctx, id)
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (s *Store) ReapRateLimitReservations(ctx context.Context, limit int) (int, error) {
	affected, err := s.queries.ReapRateLimitReservations(ctx, int32(limit))
	if err != nil {
		return 0, mapError(err)
	}
	return int(affected), nil
}

func (s *Store) CountActiveRateLimitReservations(ctx context.Context, userID int, apiKeyID *int, model string, channelID *int) (int64, error) {
	count, err := s.queries.CountActiveRateLimitReservations(ctx, sqlc.CountActiveRateLimitReservationsParams{
		UserID:    int64(userID),
		ApiKeyID:  int64(optionalID(apiKeyID)),
		Model:     model,
		ChannelID: int64(optionalID(channelID)),
	})
	if err != nil {
		return 0, mapError(err)
	}
	return count, nil
}

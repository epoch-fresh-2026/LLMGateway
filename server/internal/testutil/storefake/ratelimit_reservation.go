package storefake

import (
	"context"

	domain "LLMGateway/server/internal/ratelimit"
)

func (s *Store) InsertRateLimitReservation(_ context.Context, in domain.RateLimitReservationInput) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextRateLimitReservationID
	s.nextRateLimitReservationID++
	s.rateLimitReservations[id] = in
	return id, nil
}

func (s *Store) FinalizeRateLimitReservation(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rateLimitReservations[id]; !ok {
		return false, nil
	}
	delete(s.rateLimitReservations, id)
	return true, nil
}

func (s *Store) ReleaseRateLimitReservation(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rateLimitReservations[id]; !ok {
		return false, nil
	}
	delete(s.rateLimitReservations, id)
	return true, nil
}

func (s *Store) ReapRateLimitReservations(_ context.Context, limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	now := s.now()
	for id, in := range s.rateLimitReservations {
		if n >= limit {
			break
		}
		if !in.ExpiresAt.After(now) {
			delete(s.rateLimitReservations, id)
			n++
		}
	}
	return n, nil
}

func (s *Store) CountActiveRateLimitReservations(_ context.Context, userID int, apiKeyID *int, model string, channelID *int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var count int64
	for _, in := range s.rateLimitReservations {
		if !in.ExpiresAt.After(now) {
			continue
		}
		if in.UserID != userID || (apiKeyID != nil && in.APIKeyID != *apiKeyID) || (model != "" && in.Model != model) || (channelID != nil && (in.ChannelID == nil || *in.ChannelID != *channelID)) {
			continue
		}
		count++
	}
	return count, nil
}

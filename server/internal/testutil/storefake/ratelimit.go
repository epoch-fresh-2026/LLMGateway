package storefake

import (
	"context"
	"sort"
	"strconv"

	domain "LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/store"
)

func (s *Store) ListRateLimits(_ context.Context, ownerUserID int, enabled *bool, page, pageSize int) (domain.ListResponse[domain.RateLimitRuleDTO], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rules := []*domain.RateLimitRule{}
	for id, rule := range s.rateLimits {
		if s.rateLimitOwners[id] != ownerUserID {
			continue
		}
		if enabled != nil && rule.Enabled != *enabled {
			continue
		}
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority < rules[j].Priority
		}
		return rules[i].ID < rules[j].ID
	})

	start, end := pageBounds(len(rules), page, pageSize)
	list := []domain.RateLimitRuleDTO{}
	for _, rule := range rules[start:end] {
		list = append(list, domain.RateLimitRuleToDTO(*rule))
	}
	return domain.ListResponse[domain.RateLimitRuleDTO]{List: list, Total: len(rules)}, nil
}

func (s *Store) GetRateLimit(_ context.Context, ownerUserID, id int) (domain.RateLimitRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rule, ok := s.rateLimits[id]
	if !ok || s.rateLimitOwners[id] != ownerUserID {
		return domain.RateLimitRule{}, store.ErrNotFound
	}
	return *rule, nil
}

func (s *Store) InsertRateLimit(_ context.Context, ownerUserID int, rule domain.RateLimitRule) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rule.ID = s.nextRateLimitID
	s.nextRateLimitID++
	stored := rule
	s.rateLimits[stored.ID] = &stored
	s.rateLimitOwners[stored.ID] = ownerUserID
	return stored.ID, nil
}

func (s *Store) UpdateRateLimitEnabled(_ context.Context, ownerUserID, id int, enabled bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rateLimits[id]; !ok || s.rateLimitOwners[id] != ownerUserID {
		return false, nil
	}
	s.rateLimits[id].Enabled = enabled
	return true, nil
}

func (s *Store) DeleteRateLimit(_ context.Context, ownerUserID, id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rateLimits[id]; !ok || s.rateLimitOwners[id] != ownerUserID {
		return false, nil
	}
	delete(s.rateLimits, id)
	delete(s.rateLimitOwners, id)
	return true, nil
}

func (s *Store) TargetOwnedByUser(_ context.Context, ownerUserID int, targetType, targetValue string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if targetValue == "*" {
		return true, nil
	}
	switch targetType {
	case "user":
		return targetValue == strconv.Itoa(ownerUserID), nil
	case "api_key":
		id, err := strconv.Atoi(targetValue)
		if err != nil {
			return false, nil
		}
		key, ok := s.keys[id]
		return ok && key.userID == ownerUserID, nil
	case "channel":
		id, err := strconv.Atoi(targetValue)
		if err != nil {
			return false, nil
		}
		channel, ok := s.channels[id]
		return ok && channel.OwnerUserID == ownerUserID, nil
	case "model":
		for channelID, models := range s.models {
			channel := s.channels[channelID]
			if channel == nil || channel.OwnerUserID != ownerUserID {
				continue
			}
			for _, model := range models {
				if model.ModelName == targetValue {
					return true, nil
				}
			}
		}
		return false, nil
	default:
		return false, nil
	}
}

package storefake

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	domain "LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/store"
)

func (s *Store) ListChannels(_ context.Context, ownerUserID int) (domain.ListResponse[domain.ChannelDTO], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []domain.ChannelDTO{}
	for _, ch := range s.channels {
		if ch.OwnerUserID != ownerUserID {
			continue
		}
		list = append(list, s.channelDTO(ch))
	}
	return domain.ListResponse[domain.ChannelDTO]{List: list, Total: len(list)}, nil
}

func (s *Store) GetChannelOwner(_ context.Context, channelID int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.channels[channelID]
	if !ok {
		return 0, store.ErrNotFound
	}
	return ch.OwnerUserID, nil
}

func (s *Store) GetChannelDTO(_ context.Context, ownerUserID, id int) (domain.ChannelDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.channels[id]
	if !ok || ch.OwnerUserID != ownerUserID {
		return domain.ChannelDTO{}, store.ErrNotFound
	}
	return s.channelDTO(ch), nil
}

func (s *Store) GetChannelRecord(_ context.Context, ownerUserID, id int) (domain.ChannelRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.channels[id]
	if !ok || ch.OwnerUserID != ownerUserID {
		return domain.ChannelRecord{}, store.ErrNotFound
	}
	return domain.ChannelRecord{
		ID:               ch.ID,
		Name:             ch.Name,
		BaseURL:          ch.BaseURL,
		APIKeyCiphertext: ch.APIKey,
		AuthType:         ch.AuthType,
		Status:           ch.Status,
		Weight:           ch.Weight,
		Priority:         ch.Priority,
		Balance:          ch.Balance,
	}, nil
}

func (s *Store) InsertChannel(_ context.Context, in domain.ChannelInsert) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := &domain.Channel{
		ID:          s.nextChannelID,
		OwnerUserID: in.OwnerUserID,
		Name:        in.Name,
		BaseURL:     in.BaseURL,
		APIKey:      in.APIKeyCiphertext,
		AuthType:    in.AuthType,
		Status:      in.Status,
		Weight:      in.Weight,
		Priority:    in.Priority,
		Balance:     in.Balance,
	}
	s.nextChannelID++
	s.channels[ch.ID] = ch
	return ch.ID, nil
}

func (s *Store) UpdateChannelRecord(_ context.Context, ownerUserID, id int, in domain.ChannelUpdate) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.channels[id]
	if !ok || ch.OwnerUserID != ownerUserID {
		return false, nil
	}
	ch.Name, ch.BaseURL, ch.AuthType, ch.Status, ch.Weight, ch.Priority = in.Name, in.BaseURL, in.AuthType, in.Status, in.Weight, in.Priority
	if strings.TrimSpace(in.APIKeyCiphertext) != "" {
		ch.APIKey = in.APIKeyCiphertext
	}
	ch.Balance = in.Balance
	return true, nil
}

func (s *Store) UpdateChannelStatusRecord(_ context.Context, ownerUserID, id, status int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.channels[id]
	if !ok || ch.OwnerUserID != ownerUserID {
		return false, nil
	}
	ch.Status = status
	return true, nil
}

func (s *Store) DeleteChannel(_ context.Context, ownerUserID, id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.channels[id]
	if !ok || ch.OwnerUserID != ownerUserID {
		return false, nil
	}
	delete(s.channels, id)
	delete(s.models, id)
	for key, p := range s.pricing {
		if p.ChannelID == id {
			delete(s.pricing, key)
		}
	}
	return true, nil
}

func (s *Store) ListChannelModels(_ context.Context, ownerUserID, channelID int) (domain.ListResponse[domain.ChannelModel], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	channel, ok := s.channels[channelID]
	if !ok || channel.OwnerUserID != ownerUserID {
		return domain.ListResponse[domain.ChannelModel]{}, store.ErrNotFound
	}
	list := []domain.ChannelModel{}
	for _, m := range s.models[channelID] {
		list = append(list, *m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return domain.ListResponse[domain.ChannelModel]{List: list, Total: len(list)}, nil
}

func (s *Store) InsertChannelModel(_ context.Context, channelID int, in domain.ChannelModel) (domain.ChannelModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.channels[channelID]; !ok {
		return domain.ChannelModel{}, store.ErrNotFound
	}
	if s.hasChannelModelLocked(channelID, in.ModelName) {
		return domain.ChannelModel{}, fmt.Errorf("%w: model mapping already exists", store.ErrInvalid)
	}
	in.ID = s.nextModelID
	s.nextModelID++
	if s.models[channelID] == nil {
		s.models[channelID] = map[int]*domain.ChannelModel{}
	}
	m := in
	s.models[channelID][m.ID] = &m
	return m, nil
}

func (s *Store) GetChannelModelByID(_ context.Context, channelID, modelID int) (domain.ChannelModel, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.models[channelID][modelID]
	if !ok {
		return domain.ChannelModel{}, false, nil
	}
	return *m, true, nil
}

func (t *catalogTx) UpdateChannelModelRecord(channelID, modelID int, modelName string, enabled bool) (domain.ChannelModel, bool, error) {
	m, ok := t.s.models[channelID][modelID]
	if !ok {
		return domain.ChannelModel{}, false, nil
	}
	if modelName != "" && modelName != m.ModelName {
		if t.s.hasChannelModelLocked(channelID, modelName) {
			return domain.ChannelModel{}, false, fmt.Errorf("%w: model mapping already exists", store.ErrInvalid)
		}
		m.ModelName = modelName
	}
	m.Enabled = enabled
	return *m, true, nil
}

func (s *Store) DeleteChannelModel(_ context.Context, channelID, modelID int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.models[channelID][modelID]; !ok {
		return false, nil
	}
	delete(s.models[channelID], modelID)
	return true, nil
}

func (s *Store) ChannelModelExists(_ context.Context, ownerUserID, channelID int, modelName string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	channel, ok := s.channels[channelID]
	if !ok || channel.OwnerUserID != ownerUserID {
		return false, nil
	}
	return s.hasChannelModelLocked(channelID, modelName), nil
}

func (s *Store) ListCatalogModels(_ context.Context, ownerUserID int, enabledOnly bool) (domain.ListResponse[domain.CatalogModelDTO], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byName := map[string]*domain.CatalogModelDTO{}
	order := []string{}
	for channelID, models := range s.models {
		ch := s.channels[channelID]
		if ch == nil || ch.OwnerUserID != ownerUserID {
			continue
		}
		for _, m := range models {
			if enabledOnly && !m.Enabled {
				continue
			}
			entry := byName[m.ModelName]
			if entry == nil {
				entry = &domain.CatalogModelDTO{ModelName: m.ModelName, Status: 1}
				byName[m.ModelName] = entry
				order = append(order, m.ModelName)
			}
			entry.Channels = append(entry.Channels, domain.CatalogChannelDTO{ChannelID: channelID, ChannelName: ch.Name, UpstreamModel: m.UpstreamModel, Enabled: m.Enabled})
		}
	}
	sort.Strings(order)
	list := []domain.CatalogModelDTO{}
	for _, name := range order {
		list = append(list, *byName[name])
	}
	return domain.ListResponse[domain.CatalogModelDTO]{List: list, Total: len(list)}, nil
}

func (s *Store) ListPricing(_ context.Context, ownerUserID int) (domain.ListResponse[domain.PricingDTO], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []domain.PricingDTO{}
	for _, p := range s.pricing {
		channel := s.channels[p.ChannelID]
		if channel == nil || channel.OwnerUserID != ownerUserID {
			continue
		}
		list = append(list, s.pricingDTO(p))
	}
	return domain.ListResponse[domain.PricingDTO]{List: list, Total: len(list)}, nil
}

func (s *Store) UpsertPricingRecord(_ context.Context, in domain.PricingRecord) (domain.PricingDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.channels[in.ChannelID]; !ok {
		return domain.PricingDTO{}, store.ErrNotFound
	}

	key := pricingKey(in.ChannelID, in.UpstreamModel)
	p := s.pricing[key]
	if p == nil {
		p = &domain.Pricing{ID: s.nextPricingID, ChannelID: in.ChannelID, UpstreamModel: in.UpstreamModel}
		s.nextPricingID++
		s.pricing[key] = p
	}
	p.InputPricePer1M, p.OutputPricePer1M, p.CachedInputPricePer1M, p.Currency = in.InputPricePer1M, in.OutputPricePer1M, in.CachedInputPricePer1M, in.Currency
	if p.Currency == "" {
		p.Currency = "USD"
	}
	return s.pricingDTO(p), nil
}

func (s *Store) DeletePricing(_ context.Context, in domain.DeletePricingInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pricing, pricingKey(in.ChannelID, in.UpstreamModel))
	return nil
}

func (s *Store) GetPricing(_ context.Context, channelID int, upstreamModel string) (domain.PricingDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pricing, ok := s.pricing[pricingKey(channelID, upstreamModel)]
	if !ok {
		return domain.PricingDTO{}, store.ErrNotFound
	}
	return s.pricingDTO(pricing), nil
}

func (s *Store) ChannelUpstreamExists(_ context.Context, ownerUserID, channelID int, upstreamModel string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	channel, ok := s.channels[channelID]
	if !ok || channel.OwnerUserID != ownerUserID {
		return false, nil
	}
	return s.hasChannelUpstreamLocked(channelID, upstreamModel), nil
}

func (s *Store) RouteCandidate(_ context.Context, ownerUserID int, modelName string, channelID, cooldownSeconds int) (domain.RouteCandidate, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	channel := s.channels[channelID]
	if channel == nil || channel.OwnerUserID != ownerUserID || channel.Status != 1 {
		return domain.RouteCandidate{}, false, nil
	}
	base := s.breaker
	base.Cooldown = time.Duration(cooldownSeconds) * time.Second
	health := s.channelHealthLocked(channelID, base)
	if health.State == domain.HealthOpen {
		return domain.RouteCandidate{}, false, nil
	}
	for _, model := range s.models[channelID] {
		if model.ModelName == modelName && model.Enabled {
			return domain.RouteCandidate{ChannelID: channelID, ChannelName: channel.Name, UpstreamModel: model.UpstreamModel, Priority: channel.Priority, Weight: channel.Weight, Balance: channel.Balance, HealthState: health.State}, true, nil
		}
	}
	return domain.RouteCandidate{}, false, nil
}

func (s *Store) RouteCandidates(_ context.Context, ownerUserID int, modelName string, cooldownSeconds int) (domain.ListResponse[domain.RouteCandidate], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	base := s.breaker
	base.Cooldown = time.Duration(cooldownSeconds) * time.Second

	candidates := []domain.RouteCandidate{}
	for channelID, models := range s.models {
		channel := s.channels[channelID]
		if channel == nil || channel.OwnerUserID != ownerUserID || channel.Status != 1 {
			continue
		}
		// Exclude open (tripped) channels; a missing health row means closed and
		// a cooled-down open channel is treated as half-open.
		health := s.channelHealthLocked(channelID, base)
		if health.State == domain.HealthOpen {
			continue
		}
		for _, model := range models {
			if model.ModelName != modelName || !model.Enabled {
				continue
			}
			candidates = append(candidates, domain.RouteCandidate{ChannelID: channelID, ChannelName: channel.Name, UpstreamModel: model.UpstreamModel, Priority: channel.Priority, Weight: channel.Weight, Balance: channel.Balance, HealthState: health.State})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		if candidates[i].Weight != candidates[j].Weight {
			return candidates[i].Weight > candidates[j].Weight
		}
		return candidates[i].ChannelID < candidates[j].ChannelID
	})
	return domain.ListResponse[domain.RouteCandidate]{List: candidates, Total: len(candidates)}, nil
}

package storefake

import (
	"context"
	"fmt"
	"sort"
	"time"

	domain "LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/store"
)

func (s *Store) GetChannelHealthRow(_ context.Context, channelID int) (domain.ChannelHealth, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.channelHealth[channelID]
	if !ok {
		return domain.NewChannelHealth(channelID), false, nil
	}
	return *current, true, nil
}

func (s *Store) ListChannelHealthRows(_ context.Context) ([]domain.ChannelHealth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := make([]int, 0, len(s.channels))
	for id := range s.channels {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	list := []domain.ChannelHealth{}
	for _, id := range ids {
		if current, ok := s.channelHealth[id]; ok {
			list = append(list, *current)
		} else {
			list = append(list, domain.NewChannelHealth(id))
		}
	}
	return list, nil
}

func (s *Store) AcquireChannelProbe(_ context.Context, channelID int, lease time.Duration) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if current, ok := s.probes[channelID]; ok && current.until.After(now) {
		return "", false, nil
	}
	leaseID := fmt.Sprintf("probe-%d", s.nextProbeLeaseID)
	s.nextProbeLeaseID++
	s.probes[channelID] = fakeProbeLease{id: leaseID, until: now.Add(lease)}
	return leaseID, true, nil
}

func (s *Store) ReleaseChannelProbe(_ context.Context, channelID int, leaseID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.probes[channelID]
	if !ok || current.id != leaseID {
		return false, nil
	}
	delete(s.probes, channelID)
	return true, nil
}

func (s *Store) GetChannelBreakerConfigRow(_ context.Context, channelID int) (domain.ChannelBreakerConfig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok := s.breakerConfigs[channelID]
	return cfg, ok, nil
}

func (s *Store) ListChannelBreakerConfigRows(_ context.Context) (map[int]domain.ChannelBreakerConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[int]domain.ChannelBreakerConfig, len(s.breakerConfigs))
	for id, cfg := range s.breakerConfigs {
		result[id] = cfg
	}
	return result, nil
}

func (s *Store) DeleteStaleChannelHealthBuckets(_ context.Context, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := before.UTC().Unix()
	removed := 0
	for channelID, buckets := range s.healthBuckets {
		for start := range buckets {
			if start < cutoff {
				delete(buckets, start)
				removed++
			}
		}
		if len(buckets) == 0 {
			delete(s.healthBuckets, channelID)
		}
	}
	return removed, nil
}

// channelHealthLocked returns the channel health with the lazy open ->
// half-open transition applied. It does not persist the transition.
func (s *Store) channelHealthLocked(channelID int, cfg domain.ChannelBreakerConfig) domain.ChannelHealth {
	current, ok := s.channelHealth[channelID]
	if !ok {
		return domain.NewChannelHealth(channelID)
	}
	return domain.EvaluateChannelHealth(*current, s.now(), cfg)
}

type catalogRunner struct {
	store *Store
}

func (s *Store) CatalogTx() domain.TxManager { return catalogRunner{store: s} }

func (r catalogRunner) InTx(_ context.Context, fn func(domain.Tx) error) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	return fn(&catalogTx{s: r.store})
}

type catalogTx struct {
	s *Store
}

func (t *catalogTx) EnsureChannelHealth(channelID int) error {
	if t.s.channelHealth[channelID] == nil {
		health := domain.NewChannelHealth(channelID)
		t.s.channelHealth[channelID] = &health
	}
	return nil
}

func (t *catalogTx) GetChannelHealthForUpdate(channelID int) (domain.ChannelHealth, error) {
	current, ok := t.s.channelHealth[channelID]
	if !ok {
		return domain.NewChannelHealth(channelID), nil
	}
	return *current, nil
}

func (t *catalogTx) UpdateChannelHealth(health domain.ChannelHealth) (bool, error) {
	copied := health
	t.s.channelHealth[health.ChannelID] = &copied
	return true, nil
}

func (t *catalogTx) DeleteChannelHealth(channelID int) error {
	delete(t.s.channelHealth, channelID)
	delete(t.s.probes, channelID)
	delete(t.s.healthBuckets, channelID)
	delete(t.s.breakerConfigs, channelID)
	return nil
}

func (t *catalogTx) UpsertChannelHealthBucket(channelID int, bucketStart time.Time, requests, errors, timeouts int64) error {
	buckets := t.s.healthBuckets[channelID]
	if buckets == nil {
		buckets = map[int64]*fakeHealthBucket{}
		t.s.healthBuckets[channelID] = buckets
	}
	key := bucketStart.UTC().Unix()
	bucket := buckets[key]
	if bucket == nil {
		bucket = &fakeHealthBucket{}
		buckets[key] = bucket
	}
	bucket.requests += requests
	bucket.errors += errors
	bucket.timeouts += timeouts
	return nil
}

func (t *catalogTx) GetChannelHealthWindow(channelID int, since time.Time) (domain.ChannelHealthWindow, error) {
	cutoff := since.UTC().Unix()
	var window domain.ChannelHealthWindow
	for start, bucket := range t.s.healthBuckets[channelID] {
		if start < cutoff {
			continue
		}
		window.Requests += bucket.requests
		window.Errors += bucket.errors
		window.Timeouts += bucket.timeouts
	}
	return window, nil
}

func (t *catalogTx) UpsertChannelBreakerConfig(channelID int, cfg domain.ChannelBreakerConfig) error {
	t.s.breakerConfigs[channelID] = cfg
	return nil
}

func (t *catalogTx) DeleteChannelBreakerConfig(channelID int) error {
	delete(t.s.breakerConfigs, channelID)
	return nil
}

func (t *catalogTx) LockChannel(channelID int) error {
	if t.s.channels[channelID] == nil {
		return store.ErrNotFound
	}
	return nil
}

func (t *catalogTx) GetChannelBalanceText(channelID int) (string, error) {
	channel, ok := t.s.channels[channelID]
	if !ok {
		return "", store.ErrNotFound
	}
	if channel.Balance == nil {
		return "", nil
	}
	return *channel.Balance, nil
}

func (t *catalogTx) UpdateChannelBalance(channelID int, balance string) (bool, error) {
	channel, ok := t.s.channels[channelID]
	if !ok {
		return false, nil
	}
	value := balance
	channel.Balance = &value
	return true, nil
}

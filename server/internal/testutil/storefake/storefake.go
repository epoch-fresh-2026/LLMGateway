// Package storefake provides a test-only in-process implementation of the
// persistence ports. Production code must use store/postgres.
package storefake

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/usage"
)

// Store is the in-process test implementation of the module-owned ports.
type Store struct {
	mu            sync.Mutex
	nextChannelID int
	nextModelID   int
	nextPricingID int
	channels      map[int]*catalog.Channel
	models        map[int]map[int]*catalog.ChannelModel
	pricing       map[string]*catalog.Pricing

	nextUserID    int
	nextKeyID     int
	nextSessionID int
	users         map[int]*accounts.User
	credentials   map[int]accounts.Credentials
	keys          map[int]*memoryKey
	sessions      map[string]accounts.Session

	nextRateLimitID int
	rateLimits      map[int]*ratelimit.RateLimitRule
	rateLimitOwners map[int]int
	nextUsageLogID  int
	usageLogs       []usage.UsageLog

	channelHealth              map[int]*catalog.ChannelHealth
	healthBuckets              map[int]map[int64]*fakeHealthBucket
	breakerConfigs             map[int]catalog.ChannelBreakerConfig
	userBreakerConfigs         map[int]catalog.ChannelBreakerConfig
	nextQuotaPolicyID          int
	nextQuotaReservationID     int64
	quotaPolicies              map[int]*quota.QuotaPolicy
	quotaBuckets               map[string]*fakeQuotaBucket
	quotaReservations          map[int64]*fakeQuotaReservation
	nextRateLimitReservationID int64
	rateLimitReservations      map[int64]ratelimit.RateLimitReservationInput
	breaker                    catalog.ChannelBreakerConfig
	now                        func() time.Time
	nextProbeLeaseID           int
	probes                     map[int]fakeProbeLease
}

// fakeProbeLease is one held half-open probe lease.
type fakeProbeLease struct {
	id    string
	until time.Time
}

// fakeHealthBucket is one fixed-width channel attempt bucket, keyed by its
// start time in Unix seconds.
type fakeHealthBucket struct {
	requests int64
	errors   int64
	timeouts int64
}

var (
	_ accounts.Port        = (*Store)(nil)
	_ catalog.Port         = (*Store)(nil)
	_ catalog.HealthPort   = (*Store)(nil)
	_ ratelimit.Port       = (*Store)(nil)
	_ usage.Port           = (*Store)(nil)
	_ quota.Port           = (*Store)(nil)
	_ accounts.TxManager   = accountsRunner{}
	_ catalog.TxManager    = catalogRunner{}
	_ quota.TxManager      = quotaRunner{}
	_ settlement.TxManager = settlementRunner{}
)

// memoryKey is the fake gateway key record. Only the hash is retained;
// the plaintext is returned once at creation/reset and never stored.
type memoryKey struct {
	id                 int
	userID             int
	keyName            string
	prefix             string
	keySuffix          string
	keyHash            string
	permissions        json.RawMessage
	rateLimitOverrides json.RawMessage
	isActive           bool
	createdAt          string
	lastUsedAt         *string
	expiresAt          *string
}

// NewWithClock builds a fake store with an injected clock, used by tests to
// make cooldown behaviour deterministic.
func NewWithClock(now func() time.Time) *Store {
	s := New()
	if now != nil {
		s.now = now
	}
	return s
}

func New() *Store {
	return &Store{
		nextChannelID:              1,
		nextModelID:                1,
		nextPricingID:              1,
		channels:                   map[int]*catalog.Channel{},
		models:                     map[int]map[int]*catalog.ChannelModel{},
		pricing:                    map[string]*catalog.Pricing{},
		nextUserID:                 1,
		nextKeyID:                  1,
		nextSessionID:              1,
		users:                      map[int]*accounts.User{},
		credentials:                map[int]accounts.Credentials{},
		keys:                       map[int]*memoryKey{},
		sessions:                   map[string]accounts.Session{},
		nextRateLimitID:            1,
		rateLimits:                 map[int]*ratelimit.RateLimitRule{},
		rateLimitOwners:            map[int]int{},
		nextUsageLogID:             1,
		nextQuotaPolicyID:          1,
		nextQuotaReservationID:     1,
		nextRateLimitReservationID: 1,
		rateLimitReservations:      map[int64]ratelimit.RateLimitReservationInput{},
		quotaPolicies:              map[int]*quota.QuotaPolicy{},
		quotaBuckets:               map[string]*fakeQuotaBucket{},
		quotaReservations:          map[int64]*fakeQuotaReservation{},
		channelHealth:              map[int]*catalog.ChannelHealth{},
		healthBuckets:              map[int]map[int64]*fakeHealthBucket{},
		breakerConfigs:             map[int]catalog.ChannelBreakerConfig{},
		userBreakerConfigs:         map[int]catalog.ChannelBreakerConfig{},
		breaker:                    catalog.DefaultChannelBreakerConfig(),
		now:                        time.Now,
		nextProbeLeaseID:           1,
		probes:                     map[int]fakeProbeLease{},
	}
}

func (s *Store) channelDTO(ch *catalog.Channel) catalog.ChannelDTO {
	return catalog.ChannelDTO{ID: ch.ID, Name: ch.Name, BaseURL: ch.BaseURL, AuthType: ch.AuthType, Status: ch.Status, Weight: ch.Weight, Priority: ch.Priority, Balance: ch.Balance, ModelCount: len(s.models[ch.ID])}
}

func (s *Store) pricingDTO(p *catalog.Pricing) catalog.PricingDTO {
	channelName := ""
	if ch := s.channels[p.ChannelID]; ch != nil {
		channelName = ch.Name
	}
	return catalog.PricingDTO{ID: p.ID, ChannelID: p.ChannelID, ChannelName: channelName, UpstreamModel: p.UpstreamModel, InputPricePer1M: p.InputPricePer1M, OutputPricePer1M: p.OutputPricePer1M, CachedInputPricePer1M: p.CachedInputPricePer1M, Currency: p.Currency}
}

func (s *Store) hasChannelUpstreamLocked(channelID int, upstreamModel string) bool {
	for _, m := range s.models[channelID] {
		if m.UpstreamModel == upstreamModel {
			return true
		}
	}
	return false
}

func (s *Store) hasChannelModelLocked(channelID int, modelName string) bool {
	for _, m := range s.models[channelID] {
		if m.ModelName == modelName {
			return true
		}
	}
	return false
}

func pricingKey(channelID int, model string) string {
	return fmt.Sprintf("%d:%s", channelID, model)
}

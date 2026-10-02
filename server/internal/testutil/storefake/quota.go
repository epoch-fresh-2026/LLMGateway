package storefake

import (
	"context"
	"fmt"
	"sort"
	"time"

	"LLMGateway/server/internal/money"
	domain "LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/store"
)

type fakeQuotaBucket struct {
	policyID                   int
	start, end                 time.Time
	usedTokens, reservedTokens int64
	usedCost, reservedCost     money.Amount
}

type fakeQuotaReservation struct {
	id              int64
	requestID       string
	userID, keyID   int
	estimatedTokens int64
	estimatedCost   money.Amount
	status          string
	expiresAt       time.Time
	items           []string
}

func (s *Store) ListQuotaPolicies(_ context.Context, filter domain.QuotaPolicyFilter) (domain.ListResponse[domain.QuotaPolicyDTO], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []domain.QuotaPolicyDTO{}
	for _, policy := range s.quotaPolicies {
		if s.quotaPolicyOwnerLocked(policy) != filter.OwnerUserID {
			continue
		}
		if filter.ScopeType != "" && string(policy.ScopeType) != filter.ScopeType || filter.ScopeID > 0 && policy.ScopeID != filter.ScopeID || filter.Enabled != nil && policy.Enabled != *filter.Enabled {
			continue
		}
		list = append(list, domain.QuotaPolicyToDTO(*policy))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return domain.ListResponse[domain.QuotaPolicyDTO]{List: list, Total: len(list)}, nil
}

// quotaPolicyOwnerLocked resolves the owning user for a policy: the scope id
// for user scope, or the key's user for api_key scope.
func (s *Store) quotaPolicyOwnerLocked(policy *domain.QuotaPolicy) int {
	if policy.ScopeType == domain.QuotaScopeUser {
		return policy.ScopeID
	}
	key, ok := s.keys[policy.ScopeID]
	if !ok {
		return 0
	}
	return key.userID
}

func (s *Store) GetQuotaPolicy(_ context.Context, ownerUserID, id int) (domain.QuotaPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy, ok := s.quotaPolicies[id]
	if !ok || s.quotaPolicyOwnerLocked(policy) != ownerUserID {
		return domain.QuotaPolicy{}, store.ErrNotFound
	}
	return *policy, nil
}

func (s *Store) KeyBelongsToUser(_ context.Context, ownerUserID, keyID int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[keyID]
	return ok && key.userID == ownerUserID, nil
}

func (s *Store) InsertQuotaPolicy(_ context.Context, policy domain.QuotaPolicy) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, current := range s.quotaPolicies {
		if current.ScopeType == policy.ScopeType && current.ScopeID == policy.ScopeID && current.PeriodType == policy.PeriodType {
			return 0, fmt.Errorf("%w: duplicate quota policy", store.ErrInvalid)
		}
	}
	policy.ID = s.nextQuotaPolicyID
	s.nextQuotaPolicyID++
	stored := policy
	s.quotaPolicies[stored.ID] = &stored
	return stored.ID, nil
}

func (s *Store) DeleteQuotaPolicy(_ context.Context, ownerUserID, id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.quotaPolicies[id]
	if !ok || s.quotaPolicyOwnerLocked(current) != ownerUserID {
		return false, nil
	}
	delete(s.quotaPolicies, id)
	return true, nil
}

func (s *Store) ListQuotaUsage(_ context.Context, filter domain.QuotaPolicyFilter) (domain.ListResponse[domain.QuotaUsageDTO], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []domain.QuotaUsageDTO{}
	for _, bucket := range s.quotaBuckets {
		policy := s.quotaPolicies[bucket.policyID]
		if policy == nil || s.quotaPolicyOwnerLocked(policy) != filter.OwnerUserID || filter.ScopeType != "" && string(policy.ScopeType) != filter.ScopeType || filter.ScopeID > 0 && policy.ScopeID != filter.ScopeID {
			continue
		}
		list = append(list, domain.QuotaUsageDTO{PolicyID: policy.ID, PolicyName: policy.PolicyName, ScopeType: policy.ScopeType, ScopeID: policy.ScopeID, PeriodType: policy.PeriodType, PeriodStart: bucket.start.Format(time.RFC3339), PeriodEnd: bucket.end.Format(time.RFC3339), TokenLimit: policy.TokenLimit, UsedTokens: bucket.usedTokens, ReservedTokens: bucket.reservedTokens, CostLimit: policy.CostLimit, UsedCost: money.Format6(bucket.usedCost), ReservedCost: money.Format6(bucket.reservedCost)})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].PolicyID < list[j].PolicyID })
	return domain.ListResponse[domain.QuotaUsageDTO]{List: list, Total: len(list)}, nil
}

// --- Tx primitives ---

type quotaRunner struct {
	store *Store
}

func (s *Store) QuotaTx() domain.TxManager { return quotaRunner{store: s} }

func (r quotaRunner) InTx(_ context.Context, fn func(domain.Tx) error) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	snapshot := r.store.snapshotQuota()
	if err := fn(&quotaTx{s: r.store}); err != nil {
		r.store.restoreQuota(snapshot)
		return err
	}
	return nil
}

type quotaSnapshot struct {
	buckets           map[string]*fakeQuotaBucket
	reservations      map[int64]*fakeQuotaReservation
	nextReservationID int64
}

func (s *Store) snapshotQuota() quotaSnapshot {
	snapshot := quotaSnapshot{
		buckets:           make(map[string]*fakeQuotaBucket, len(s.quotaBuckets)),
		reservations:      make(map[int64]*fakeQuotaReservation, len(s.quotaReservations)),
		nextReservationID: s.nextQuotaReservationID,
	}
	for key, bucket := range s.quotaBuckets {
		copied := *bucket
		snapshot.buckets[key] = &copied
	}
	for id, reservation := range s.quotaReservations {
		copied := *reservation
		copied.items = append([]string(nil), reservation.items...)
		snapshot.reservations[id] = &copied
	}
	return snapshot
}

func (s *Store) restoreQuota(snapshot quotaSnapshot) {
	s.quotaBuckets = snapshot.buckets
	s.quotaReservations = snapshot.reservations
	s.nextQuotaReservationID = snapshot.nextReservationID
}

type quotaTx struct {
	s *Store
}

func (t *quotaTx) ApplicablePolicies(userID, keyID int) ([]domain.QuotaPolicy, error) {
	policies := []domain.QuotaPolicy{}
	for _, policy := range t.s.quotaPolicies {
		if !policy.Enabled {
			continue
		}
		if policy.ScopeType == domain.QuotaScopeUser && policy.ScopeID == userID || policy.ScopeType == domain.QuotaScopeAPIKey && policy.ScopeID == keyID {
			policies = append(policies, *policy)
		}
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].ID < policies[j].ID })
	return policies, nil
}

func (t *quotaTx) ReapExpired(now time.Time, limit, userID, keyID int) (int, error) {
	return t.s.reapQuotaLocked(now, limit, userID, keyID), nil
}

func (t *quotaTx) InsertReservation(in domain.QuotaReservationInsert) (int64, error) {
	cost, err := money.Parse6(in.EstimatedCost)
	if err != nil {
		return 0, store.ErrInvalid
	}
	id := t.s.nextQuotaReservationID
	t.s.nextQuotaReservationID++
	t.s.quotaReservations[id] = &fakeQuotaReservation{id: id, requestID: in.RequestID, userID: in.UserID, keyID: in.APIKeyID, estimatedTokens: in.EstimatedTokens, estimatedCost: cost, status: "pending", expiresAt: in.ExpiresAt}
	return id, nil
}

func (t *quotaTx) UpsertBucket(policyID int, start, end time.Time) error {
	key := fakeQuotaBucketKey(policyID, start)
	if t.s.quotaBuckets[key] == nil {
		t.s.quotaBuckets[key] = &fakeQuotaBucket{policyID: policyID, start: start, end: end}
	}
	return nil
}

func (t *quotaTx) LockBucket(int, time.Time) error { return nil }

func (t *quotaTx) ReserveBucket(policyID int, start time.Time, tokens int64, cost string) (bool, error) {
	policy := t.s.quotaPolicies[policyID]
	if policy == nil {
		return false, store.ErrNotFound
	}
	parsedCost, err := money.Parse6(cost)
	if err != nil {
		return false, store.ErrInvalid
	}
	bucket := t.s.quotaBuckets[fakeQuotaBucketKey(policyID, start)]
	if bucket == nil {
		return false, store.ErrNotFound
	}
	if policy.TokenLimit != nil && bucket.usedTokens+bucket.reservedTokens+tokens > *policy.TokenLimit {
		return false, nil
	}
	if policy.CostLimit != nil {
		limit, _ := money.Parse6(*policy.CostLimit)
		if bucket.usedCost.Add(bucket.reservedCost).Add(parsedCost).Cmp(limit) > 0 {
			return false, nil
		}
	}
	bucket.reservedTokens += tokens
	bucket.reservedCost = bucket.reservedCost.Add(parsedCost)
	return true, nil
}

func (t *quotaTx) InsertReservationItem(reservationID int64, policyID int, start time.Time, _ int64, _ string) error {
	reservation := t.s.quotaReservations[reservationID]
	if reservation == nil {
		return store.ErrNotFound
	}
	reservation.items = append(reservation.items, fakeQuotaBucketKey(policyID, start))
	return nil
}

func (t *quotaTx) ReleaseReservation(reservationID int64, status string, _ time.Time) error {
	return t.s.releaseQuotaLocked(reservationID, status)
}

func (s *Store) releaseQuotaLocked(id int64, status string) error {
	r := s.quotaReservations[id]
	if r == nil || r.status != "pending" {
		return nil
	}
	for _, key := range r.items {
		b := s.quotaBuckets[key]
		b.reservedTokens -= r.estimatedTokens
		b.reservedCost = b.reservedCost.Sub(r.estimatedCost)
	}
	r.status = status
	return nil
}

func (s *Store) reapQuotaLocked(now time.Time, limit, userID, keyID int) int {
	if limit <= 0 {
		limit = 100
	}
	count := 0
	for id, r := range s.quotaReservations {
		if count >= limit {
			break
		}
		if r.status == "pending" && !r.expiresAt.After(now) && (userID == 0 || r.userID == userID || r.keyID == keyID) {
			_ = s.releaseQuotaLocked(id, "expired")
			count++
		}
	}
	return count
}

func fakeQuotaBucketKey(policyID int, start time.Time) string {
	return fmt.Sprintf("%d:%s", policyID, start.UTC().Format(time.RFC3339))
}

func (s *Store) settleQuotaLocked(reservationID int64, requestID string, userID, keyID int, actualTokens int64, actualCost money.Amount) error {
	if reservationID == 0 {
		return nil
	}
	r := s.quotaReservations[reservationID]
	if r == nil {
		return store.ErrNotFound
	}
	if r.status == "settled" {
		return nil
	}
	if r.status != "pending" {
		return store.ErrInvalid
	}
	if r.requestID != requestID || r.userID != userID || r.keyID != keyID {
		return store.ErrInvalid
	}
	if actualTokens > r.estimatedTokens || actualCost.Cmp(r.estimatedCost) > 0 {
		return store.ErrInvalid
	}
	for _, key := range r.items {
		b := s.quotaBuckets[key]
		b.reservedTokens -= r.estimatedTokens
		b.reservedCost = b.reservedCost.Sub(r.estimatedCost)
		b.usedTokens += actualTokens
		b.usedCost = b.usedCost.Add(actualCost)
	}
	r.status = "settled"
	return nil
}

func (s *Store) cleanupQuotaLocked(userID, keyID int) {
	for id, reservation := range s.quotaReservations {
		if reservation.userID == userID && (keyID == 0 || reservation.keyID == keyID) {
			_ = s.releaseQuotaLocked(id, "released")
			delete(s.quotaReservations, id)
		}
	}
}

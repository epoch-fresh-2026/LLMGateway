package proxy

import (
	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/money"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// releaseProbes returns every still-held probe lease. It is best-effort: a
// failed release must not change the response, and the lease TTL is the
// backstop. The map is emptied so a caller's deferred release is a no-op.
func (a *Service) releaseProbes(ctx context.Context, probes map[int]string) {
	for channelID, leaseID := range probes {
		detached, cancel := detachedCtx(ctx, bestEffortTimeout)
		_, _ = a.catalog.ReleaseChannelProbe(detached, channelID, leaseID)
		cancel()
		delete(probes, channelID)
	}
}

// orderedCandidates resolves the route order for a public model and, for each
// half-open channel it admits, acquires a single-flight probe lease. The
// returned leases are keyed by channel id; the caller owns releasing them.
func (a *Service) orderedCandidates(ctx context.Context, ownerUserID int, model string, stickyKey ...int) ([]catalog.RouteCandidate, map[int]string, error) {
	return a.orderedCandidatesExcluding(ctx, ownerUserID, model, nil, stickyKey...)
}

func (a *Service) routeBalanceEligible(candidate catalog.RouteCandidate) bool {
	if candidate.Balance == nil {
		return true
	}
	amount, err := money.Parse6(*candidate.Balance)
	return err == nil && amount.Cmp(0) > 0 && amount.Cmp(a.minRouteBalance) >= 0
}

func (a *Service) orderedCandidatesExcluding(ctx context.Context, ownerUserID int, model string, excluded map[int]bool, stickyKey ...int) ([]catalog.RouteCandidate, map[int]string, error) {
	result, err := a.catalog.RouteCandidates(ctx, ownerUserID, model)
	if err != nil {
		return nil, nil, err
	}
	candidates := []catalog.RouteCandidate{}
	probes := map[int]string{}
	seen := map[int]bool{}
	for _, candidate := range result.List {
		if seen[candidate.ChannelID] || excluded[candidate.ChannelID] {
			continue
		}
		state := candidate.HealthState
		if state == "" {
			health, healthErr := a.catalog.GetChannelHealth(ctx, candidate.ChannelID)
			if healthErr == nil {
				state = health.State
			}
		}
		if state == catalog.HealthOpen || !a.routeBalanceEligible(candidate) {
			continue
		}
		if state == catalog.HealthHalfOpen {
			leaseID, allowed, probeErr := a.catalog.AcquireChannelProbe(ctx, candidate.ChannelID, a.requestTimeout)
			if probeErr != nil || !allowed {
				continue
			}
			probes[candidate.ChannelID] = leaseID
		}
		seen[candidate.ChannelID] = true
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return nil, probes, nil
	}

	highest := candidates[0].Priority
	group := []catalog.RouteCandidate{}
	for _, candidate := range candidates {
		if candidate.Priority == highest {
			group = append(group, candidate)
		}
	}

	total := 0
	for _, candidate := range group {
		if candidate.Weight > 0 {
			total += candidate.Weight
		}
	}
	if total <= 0 {
		return append(group, candidates[len(group):]...), probes, nil
	}

	pick := a.routePick(model, total, stickyKey...)
	for _, candidate := range group {
		if candidate.Weight <= 0 {
			continue
		}
		pick -= candidate.Weight
		if pick < 0 {
			ordered := []catalog.RouteCandidate{candidate}
			for _, rest := range group {
				if rest.ChannelID != candidate.ChannelID {
					ordered = append(ordered, rest)
				}
			}
			for _, rest := range candidates {
				if rest.Priority != highest {
					ordered = append(ordered, rest)
				}
			}
			return ordered, probes, nil
		}
	}
	return candidates, probes, nil
}

// routePick keeps an API key on the same weighted candidate for a public model.
// A missing key falls back to the injected random source used by tests and
// unauthenticated internal callers.
func (a *Service) routePick(model string, total int, stickyKey ...int) int {
	if len(stickyKey) == 0 || stickyKey[0] <= 0 {
		return a.randIntN(total)
	}
	seed := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", stickyKey[0], model)))
	return int(binary.BigEndian.Uint64(seed[:8]) % uint64(total))
}

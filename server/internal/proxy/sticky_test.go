package proxy

import (
	"context"
	"testing"
	"time"
)

func TestStickySuccessPreservesExpiryAndRefreshesGeneration(t *testing.T) {
	var cache stickyCache
	key := stickyKey{owner: 1, key: 42, model: "gpt"}
	start := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cache.put(key, 1, start)
	previous := cache.get(key, start)
	for elapsed := time.Second; elapsed < stickyTTL; elapsed += time.Second {
		now := start.Add(elapsed)
		cache.put(key, 1, now)
		current := cache.get(key, now)
		if !current.expires.Equal(start.Add(stickyTTL)) || current.generation <= previous.generation {
			t.Fatalf("binding=%+v previous=%+v, want fixed expiry and newer generation", current, previous)
		}
		cache.invalidate(key, previous)
		if got := cache.get(key, now); got != current {
			t.Fatalf("late failure cleared fresh success: got=%+v want=%+v", got, current)
		}
		previous = current
	}
	if got := cache.get(key, start.Add(stickyTTL)); got != (stickyBinding{}) {
		t.Fatalf("binding=%+v, want expired at fixed TTL", got)
	}
}

func TestStickyPutStartsNewCycle(t *testing.T) {
	for _, mode := range []string{"new", "changed", "expired", "evicted_expiry"} {
		t.Run(mode, func(t *testing.T) {
			var cache stickyCache
			key := stickyKey{owner: 1, key: 42, model: "gpt"}
			start := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
			now := start
			channelID := 1
			var previous stickyBinding
			if mode != "new" {
				cache.put(key, 1, start)
				previous = cache.get(key, start)
				if mode == "changed" {
					now = start.Add(time.Minute)
					channelID = 2
				} else {
					now = start.Add(stickyTTL)
					if mode == "evicted_expiry" {
						cache.get(key, now)
					}
				}
			}
			cache.put(key, channelID, now)
			current := cache.get(key, now)
			if current.channelID != channelID || !current.expires.Equal(now.Add(stickyTTL)) || current.generation <= previous.generation {
				t.Fatalf("binding=%+v, want channel=%d new TTL and generation", current, channelID)
			}
		})
	}
}

func TestStickyFallbackSuccessAllowsHigherPriorityAfterFixedTTL(t *testing.T) {
	ctx := context.Background()
	service := newRouteTestApp(seedRoutingStore(t), func(int) int { return 0 })
	start := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	now := start
	service.now = func() time.Time { return now }
	counted := &routingCountCatalog{Catalog: service.catalog}
	service.catalog = counted
	key := stickyKey{owner: 1, key: 42, model: "gpt"}
	service.sticky.put(key, 1, now)
	for elapsed := time.Duration(0); elapsed < stickyTTL; elapsed += time.Second {
		now = start.Add(elapsed)
		candidates, probes, _, fast, err := service.stickyCandidates(ctx, key)
		service.releaseProbes(ctx, probes)
		if err != nil || !fast || len(candidates) != 1 || candidates[0].ChannelID != 1 {
			t.Fatalf("elapsed=%v candidates=%v fast=%v err=%v", elapsed, candidates, fast, err)
		}
		service.sticky.put(key, 1, now)
	}
	if counted.listCalls != 0 {
		t.Fatalf("list calls=%d, want no cold query within TTL", counted.listCalls)
	}
	now = start.Add(stickyTTL)
	candidates, probes, _, fast, err := service.stickyCandidates(ctx, key)
	service.releaseProbes(ctx, probes)
	if err != nil || fast || len(candidates) != 3 || candidates[0].Priority != 10 || counted.listCalls != 1 {
		t.Fatalf("candidates=%v fast=%v err=%v list calls=%d, want recovered higher priority cold route", candidates, fast, err, counted.listCalls)
	}
	expected, probes, err := service.orderedCandidates(ctx, key.owner, key.model, key.key)
	service.releaseProbes(ctx, probes)
	if err != nil || candidates[0].ChannelID != expected[0].ChannelID {
		t.Fatalf("cold route=%v expected=%v err=%v, want highest-priority stable hash", candidates, expected, err)
	}
}

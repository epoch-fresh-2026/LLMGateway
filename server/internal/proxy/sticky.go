package proxy

import (
	"container/list"
	"context"
	"sync"
	"time"

	"LLMGateway/server/internal/catalog"
)

const stickyTTL = 5 * time.Minute
const stickyCapacity = 4096

type stickyKey struct {
	owner int
	key   int
	model string
}

type stickyBinding struct {
	channelID  int
	generation uint64
	expires    time.Time
}

type stickyItem struct {
	key     stickyKey
	binding stickyBinding
}

type stickyCache struct {
	mu         sync.Mutex
	entries    map[stickyKey]*list.Element
	order      list.List
	generation uint64
}

func (c *stickyCache) get(key stickyKey, now time.Time) stickyBinding {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[key]; e != nil {
		item := e.Value.(stickyItem)
		if now.Before(item.binding.expires) {
			c.order.MoveToFront(e)
			return item.binding
		}
		delete(c.entries, key)
		c.order.Remove(e)
	}
	return stickyBinding{}
}

func (c *stickyCache) put(key stickyKey, channelID int, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[stickyKey]*list.Element)
	}
	c.generation++
	item := stickyItem{key: key, binding: stickyBinding{channelID: channelID, generation: c.generation, expires: now.Add(stickyTTL)}}
	if e := c.entries[key]; e != nil {
		previous := e.Value.(stickyItem).binding
		if previous.channelID == channelID && now.Before(previous.expires) {
			item.binding.expires = previous.expires
		}
		e.Value = item
		c.order.MoveToFront(e)
		return
	}
	if len(c.entries) >= stickyCapacity {
		e := c.order.Back()
		delete(c.entries, e.Value.(stickyItem).key)
		c.order.Remove(e)
	}
	c.entries[key] = c.order.PushFront(item)
}

func (c *stickyCache) invalidate(key stickyKey, binding stickyBinding) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[key]; e != nil && e.Value.(stickyItem).binding == binding {
		delete(c.entries, key)
		c.order.Remove(e)
	}
}

func (a *Service) stickyCandidates(ctx context.Context, key stickyKey) ([]catalog.RouteCandidate, map[int]string, stickyBinding, bool, error) {
	binding := a.sticky.get(key, a.now())
	if binding.channelID != 0 {
		candidate, found, err := a.catalog.RouteCandidate(ctx, key.owner, key.model, binding.channelID)
		if err != nil {
			return nil, nil, binding, false, err
		}
		eligible := found && candidate.ChannelID == binding.channelID && candidate.HealthState != catalog.HealthOpen && a.routeBalanceEligible(candidate)
		probes := map[int]string{}
		if eligible && candidate.HealthState == catalog.HealthHalfOpen {
			lease, allowed, err := a.catalog.AcquireChannelProbe(ctx, candidate.ChannelID, a.requestTimeout)
			eligible = err == nil && allowed
			if eligible {
				probes[candidate.ChannelID] = lease
			}
		}
		if eligible {
			return []catalog.RouteCandidate{candidate}, probes, binding, true, nil
		}
		a.sticky.invalidate(key, binding)
	}
	candidates, probes, err := a.orderedCandidates(ctx, key.owner, key.model, key.key)
	return candidates, probes, binding, false, err
}

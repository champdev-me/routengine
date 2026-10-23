package engine

import (
	"math"
	"time"

	"routengine/internal/nics"
)

// unknownRTT stands in for an adapter nobody has measured yet: it is tried only when no measured
// adapter is faster, and then it gets measured.
const unknownRTT = int64(time.Second)

// noteRTT folds a new connect time into the adapter's smoothed latency for family f.
func (sl *Slot) noteRTT(f Family, d time.Duration) {
	n := int64(d)
	if old := sl.rtt[f].Load(); old > 0 {
		n = (old*3 + n) / 4 // a quarter of the new sample: one slow moment does not flip the choice
	}
	sl.rtt[f].Store(n)
}

// internetAllow accepts the internet adapters that can carry family f right now. It runs with s.mu held.
func (s *Scheduler) internetAllow(f Family) func(*Slot) bool {
	return func(sl *Slot) bool {
		return sl.kind == nics.KindInternet && sl.state[f] != stateDown && !s.now().Before(sl.downUntil[f])
	}
}

// pickFor says how to choose the adapter for each flow that is balanced over a pool: through the
// pool's own rules, over all internet adapters when it has none, and by speed when the rule asks for it.
func (b *Balancer) pickFor(f Family, rt Route) func(tried map[string]struct{}) *Slot {
	var allow func(*Slot) bool
	if len(rt.Pool) > 0 {
		allow = b.S.poolAllow(f, rt.Pool)
	} else {
		allow = b.S.internetAllow(f)
	}
	if rt.Fastest {
		return func(tried map[string]struct{}) *Slot { return b.S.acquireFastest(f, tried, allow) }
	}
	return func(tried map[string]struct{}) *Slot { return b.S.acquire(f, tried, allow) }
}

// acquireFastest takes a slot on the adapter with the lowest measured latency among those allow
// accepts; the adapter with fewer open flows wins a tie.
func (s *Scheduler) acquireFastest(f Family, skip map[string]struct{}, allow func(*Slot) bool) *Slot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *Slot
	bestRTT := int64(math.MaxInt64)
	for _, sl := range s.slots {
		if !sl.present || sl.ip(f) == nil {
			continue
		}
		if _, no := skip[sl.key]; no {
			continue
		}
		if allow != nil && !allow(sl) {
			continue
		}
		rtt := sl.rtt[f].Load()
		if rtt <= 0 {
			rtt = unknownRTT
		}
		if best == nil || rtt < bestRTT || (rtt == bestRTT && sl.actF[f].Load() < best.actF[f].Load()) {
			best, bestRTT = sl, rtt
		}
	}
	if best == nil {
		return nil
	}
	best.active.Add(1)
	best.actF[f].Add(1)
	best.total.Add(1)
	return best
}

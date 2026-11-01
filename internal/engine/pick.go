package engine

import (
	"time"

	"routengine/internal/nics"
)

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

// pickFor says how to choose the adapter for each flow that is balanced over a pool: through the pool's own
// rules, over all internet adapters when it has none, and by speed when the rule asks for it and this build
// can (the fastest adapter is a Pro feature of the window apps; see fastest_pro.go).
func (b *Balancer) pickFor(f Family, rt Route) func(tried map[string]struct{}) *Slot {
	var allow func(*Slot) bool
	if len(rt.Pool) > 0 {
		allow = b.S.poolAllow(f, rt.Pool)
	} else {
		allow = b.S.internetAllow(f)
	}
	if rt.Fastest {
		if pick := b.fastestPick(f, allow); pick != nil {
			return pick
		}
	}
	return func(tried map[string]struct{}) *Slot { return b.S.acquire(f, tried, allow) }
}

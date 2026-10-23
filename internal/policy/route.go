package policy

import (
	"net/netip"
	"strings"
	"time"

	M "github.com/xjasonlyu/tun2socks/v2/metadata"

	"routengine/internal/engine"
	"routengine/internal/license"
)

// capState says which rules have used up their data allowance. It belongs to one rule set: indexes
// from another set mean nothing.
type capState struct {
	set  *Set
	over []bool
}

// capped reports whether the rule at idx has used up its allowance.
func (r *Router) capped(set *Set, idx int) bool {
	cs := r.caps.Load()
	return cs != nil && cs.set == set && idx < len(cs.over) && cs.over[idx]
}

// Route is the hook for tun.Tun.Route. It is called once per new TCP connection or UDP session. It is
// where licensed features are enforced: a rule's speed limit, data cap, DNS server and "fastest adapter"
// only take effect when the license allows them, and the free edition only honours its first few rules.
func (r *Router) Route(m *M.Metadata) engine.Route {
	proc, ok := r.lookup(m.Network == M.TCP, netip.AddrPortFrom(m.SrcIP.Unmap(), m.SrcPort))
	if !ok {
		// Owner unknown (socket already gone, or not ours to see): balance, and count it apart.
		return engine.Route{Meter: engine.Apps.For("")}
	}
	meter := engine.Apps.For(proc.Exe)
	set := r.set.Load()
	feats := license.Allowed()

	idx, ok := set.MatchIndex(proc.Exe)
	if ok && idx >= license.FreeApps && !feats.UnlimitedApps {
		ok = false // the free edition looks after its first few apps; the rest are listed but inert
	}
	if !ok {
		meter.SetLimits(0, 0)
		meter.SetDenied(false)
		return engine.Route{Meter: meter}
	}
	rule := set.rules[idx]
	capped := r.apply(meter, set, idx, feats)

	rt := engine.Route{Meter: meter, OnDown: engine.Block}
	if len(rule.Balance) > 0 {
		rt.Pool = rule.Balance
	} else if rule.Adapter != Merge {
		def := set.Default
		if def == "" && r.Auto != nil && usesDefault(rule.Failover) {
			def = r.Auto()
		}
		rt.Adapter, rt.Failover = rule.Adapter, ResolveFailover(rule.Failover, rule.Adapter, def)
	}
	if rule.OnDown == "merge" {
		rt.OnDown = engine.Merge
	}
	if feats.Fastest && rule.Fastest {
		rt.Fastest = true
	}
	if feats.DNS && len(rule.DNS) > 0 {
		rt.DNS = rule.DNSAddrs()
	}
	if capped {
		rt.Deny = engine.ErrCapReached.Error()
	}
	if rt.Adapter == "" && len(rt.Pool) == 0 && !rt.Fastest && len(rt.DNS) == 0 && !capped {
		return engine.Route{Meter: meter} // nothing but watching: plain balancing
	}
	if rt.Adapter != "" || len(rt.Pool) > 0 {
		r.note(proc, m, rule)
	}
	return rt
}

// usesDefault reports whether a failover list falls back to the default adapter.
func usesDefault(list []string) bool {
	for _, n := range list {
		if strings.EqualFold(strings.TrimSpace(n), DefaultToken) {
			return true
		}
	}
	return false
}

// apply puts the rule's speed limit and cap state on the app's meter, so connections that are already
// open follow it too. It reports whether the app is over its cap.
func (r *Router) apply(m *engine.AppMeter, set *Set, idx int, feats license.Features) (capped bool) {
	rule := set.rules[idx]
	// The free edition limits the speed of its first FreeSpeedLimitApps apps and caps the data of its first FreeCapApps.
	feats = feats.ForSlots(SpeedSlot(set.rules, idx, feats.UnlimitedApps), CapSlot(set.rules, idx, feats.UnlimitedApps))
	var down, up float64
	if feats.SpeedLimit {
		down, up = rule.LimitDown*engine.BytesPerMbit, rule.LimitUp*engine.BytesPerMbit
	}
	m.SetLimits(down, up)
	capped = feats.DataCap && rule.CapGB > 0 && r.capped(set, idx)
	m.SetDenied(capped)
	return capped
}

// maintain keeps limits and caps current without waiting for a new connection: it re-reads the usage
// history to see who has used up their allowance, and applies the result to every app that is running.
func (r *Router) maintain() {
	for range time.Tick(2 * time.Second) {
		r.refreshCaps()
		r.syncMeters()
	}
}

// refreshCaps recomputes which rules are over their allowance, from the usage history.
func (r *Router) refreshCaps() {
	set := r.set.Load()
	over := make([]bool, len(set.rules))
	var wanted bool
	for _, rule := range set.rules {
		if rule.CapGB > 0 && !rule.Disabled {
			wanted = true
		}
	}
	if wanted {
		used := make([]int64, len(set.rules))
		for _, ua := range engine.Apps.UsageReport().Apps {
			if i, ok := set.MatchIndex(ua.Exe); ok {
				b := ua.Month
				if set.rules[i].CapPer == "day" {
					b = ua.Today
				}
				used[i] += b.Total()
			}
		}
		for i, rule := range set.rules {
			if rule.CapGB > 0 && !rule.Disabled {
				over[i] = float64(used[i]) >= rule.CapGB*(1<<30)
			}
		}
	}
	r.caps.Store(&capState{set: set, over: over})
}

// syncMeters applies the current rules, license and caps to every app the engine is counting.
func (r *Router) syncMeters() {
	set := r.set.Load()
	feats := license.Allowed()
	engine.Apps.Each(func(m *engine.AppMeter) {
		if idx, ok := set.MatchIndex(m.Exe); ok && (idx < license.FreeApps || feats.UnlimitedApps) {
			r.apply(m, set, idx, feats)
			return
		}
		m.SetLimits(0, 0)
		m.SetDenied(false)
	})
}

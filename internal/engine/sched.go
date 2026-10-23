package engine

import (
	"math"
	"math/rand/v2"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"routengine/internal/nics"
)

// Family selects which address of a NIC a flow is pinned to. Load, speed and health are tracked
// per family because one link can behave very differently over each (e.g. a carrier hotspot that
// is fast over native IPv6 and slow over translated IPv4).
type Family int

const (
	Fam4 Family = iota
	Fam6
)

// linkState is the result of the internet probe: new adapters are "unknown" (still preferred
// over dead ones, so a probe that can't run never causes an outage) until the first probe lands.
type linkState int

const (
	stateUnknown linkState = iota
	stateUp
	stateDown
)

func (l linkState) String() string {
	return [...]string{"checking", "up", "no internet"}[l]
}

type slotAddr struct{ ip4, ip6 net.IP }

// Slot is a NIC plus its live counters. Counters are atomic so the data path never takes a lock.
type Slot struct {
	Name  string
	Index int
	kind  nics.Kind // what the adapter is for: only an internet adapter takes part in plain balancing
	conn  nics.Conn // how it connects, for choosing the default adapter
	key   string
	addr  atomic.Pointer[slotAddr]

	active, total, failed atomic.Int64 // active is across families (idle check on removal)
	actF                  [2]atomic.Int64
	bytesIn, bytesOut     [2]atomic.Int64
	probing               [2]atomic.Bool
	rtt                   [2]atomic.Int64 // smoothed time to connect through the adapter, in nanoseconds; 0 until measured

	// guarded by Scheduler.mu
	weight          [2]float64
	present         bool
	state           [2]linkState
	lastProbe       [2]time.Time
	downUntil       [2]time.Time
	lastIn, lastOut [2]int64
	rateIn, rateOut [2]float64 // bytes/s over the last tick
	peak            [2]float64 // observed capacity: max recent rateIn+rateOut

	// The adapter's total traffic from the OS's counters (all apps, not only RoutEngine's flows).
	linkIn, linkOut         uint64
	linkRateIn, linkRateOut float64
	linkPrimed              bool
}

func (sl *Slot) ip(f Family) net.IP {
	a := sl.addr.Load()
	if f == Fam6 {
		return a.ip6
	}
	return a.ip4
}

type SlotStats struct {
	Name              string
	Latency           float64 // smoothed connect time to the internet probe target, in ms; 0 if not measured
	Kind              string  // "internet", "local network" or "virtual/VPN"
	Conn              string  // how it connects: "ethernet", "wifi", "tether" or "virtual"
	IP                string
	IP6               string
	State             string // IPv4 probe state, "-" if the adapter has no IPv4
	State6            string
	Weight, Weight6   float64
	Present           bool
	Active, Total     int64
	Failed            int64
	BytesIn, BytesOut int64
	RateIn, RateOut   float64 // through RoutEngine, both families summed (bytes/s)
	LinkIn, LinkOut   float64 // through the adapter by any app, from the OS's counters (bytes/s)
	Peak, Peak6       float64
}

// Scheduler is weighted least-active-flows, per address family: each new flow goes to the NIC
// with the lowest (active + 1) / weight among those that have an address of that family. Only
// new flows take the lock.
type Scheduler struct {
	mu    sync.Mutex
	slots []*Slot
	auto  bool
	now   func() time.Time
}

func NewScheduler(nics []nics.Nic) *Scheduler {
	s := &Scheduler{now: time.Now}
	s.Update(nics)
	return s
}

// SetAuto makes Tick derive weights from observed throughput instead of the static ones.
func (s *Scheduler) SetAuto(on bool) {
	s.mu.Lock()
	s.auto = on
	s.mu.Unlock()
}

func normAddr(n nics.Nic) *slotAddr {
	a := &slotAddr{}
	if n.IP != nil {
		a.ip4 = n.IP.To4()
	}
	if n.IP6 != nil && n.IP6.To4() == nil {
		a.ip6 = n.IP6
	}
	return a
}

// Update applies a re-scan: keeps counters of known NICs, picks up changed addresses (DHCP
// renewal, a new IPv6 prefix), adds new ones (a tether plugged in later), and drops vanished
// ones once their last flow ends. Adapters are keyed by name.
func (s *Scheduler) Update(nics []nics.Nic) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sl := range s.slots {
		sl.present = false
	}
	for _, n := range nics {
		na := normAddr(n)
		var found *Slot
		for _, sl := range s.slots {
			if sl.key == n.Name {
				found = sl
				break
			}
		}
		if found == nil {
			sl := &Slot{Name: n.Name, Index: n.Index, kind: n.Kind, conn: n.Conn, key: n.Name, weight: [2]float64{n.Weight, n.Weight}, present: true}
			sl.addr.Store(na)
			s.slots = append(s.slots, sl)
			continue
		}
		found.present = true
		found.kind, found.conn = n.Kind, n.Conn
		if !s.auto {
			found.weight = [2]float64{n.Weight, n.Weight}
		}
		old := found.addr.Load()
		if !old.ip4.Equal(na.ip4) {
			found.state[Fam4] = stateUnknown // new address: re-verify
		}
		if !old.ip6.Equal(na.ip6) {
			found.state[Fam6] = stateUnknown
		}
		if !old.ip4.Equal(na.ip4) || !old.ip6.Equal(na.ip6) {
			found.addr.Store(na)
		}
	}
	kept := s.slots[:0]
	for _, sl := range s.slots {
		if sl.present || sl.active.Load() > 0 {
			kept = append(kept, sl)
		}
	}
	s.slots = kept
}

// A NIC counts as measured once it has carried this much (bytes/s, about 0.4 Mbit/s).
const minMeasuredPeak = 50_000

// Tick samples throughput per family. With auto on, a family's weights follow each NIC's
// observed peak for that family (floored at 0.05, so a slow path still sees an occasional flow
// and is re-measured). Until every NIC with that family has been measured the weights stay
// equal, which spreads load so each one gets measured. The peak holds while a NIC is idle and
// only decays when it is in use but underperforming, so capacity is remembered across pauses.
func (s *Scheduler) Tick(dt time.Duration) {
	// nics.Octets calls into the OS, so read the counters on a snapshot of the slots without s.mu
	// (it would stall AcquireFor and Update) and take the lock only to store the results.
	s.mu.Lock()
	slots := append([]*Slot(nil), s.slots...)
	s.mu.Unlock()

	type sample struct {
		in, out uint64
		ok      bool
	}

	samples := make([]sample, len(slots))
	for i, sl := range slots {
		in, out, ok := nics.Octets(sl.Index)
		samples[i] = sample{in, out, ok}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	sec := dt.Seconds()
	for i, sl := range slots {
		in, out, ok := samples[i].in, samples[i].out, samples[i].ok
		if !sl.present || !ok {
			sl.linkRateIn, sl.linkRateOut, sl.linkPrimed = 0, 0, false
			continue
		}
		if sl.linkPrimed && in >= sl.linkIn && out >= sl.linkOut { // counters can reset when an adapter reconnects
			sl.linkRateIn = float64(in-sl.linkIn) / sec
			sl.linkRateOut = float64(out-sl.linkOut) / sec
		}
		sl.linkIn, sl.linkOut, sl.linkPrimed = in, out, true
	}
	for _, f := range []Family{Fam4, Fam6} {
		var maxPeak float64
		live, measured := 0, true
		idle := map[*Slot]bool{}
		for _, sl := range s.slots {
			in, out := sl.bytesIn[f].Load(), sl.bytesOut[f].Load()
			sl.rateIn[f] = float64(in-sl.lastIn[f]) / sec
			sl.rateOut[f] = float64(out-sl.lastOut[f]) / sec
			sl.lastIn[f], sl.lastOut[f] = in, out
			r := sl.rateIn[f] + sl.rateOut[f]
			if r >= 0.05*sl.peak[f] {
				sl.peak[f] = math.Max(r, sl.peak[f]*0.97)
			} else {
				idle[sl] = true
			}
			if !sl.balanced(f) {
				continue
			}
			live++
			maxPeak = math.Max(maxPeak, sl.peak[f])
		}
		// An idle NIC's old estimate may be stale or pessimistic (it was measured during a bad
		// moment and then starved of flows, so it could never correct itself). Let it drift back
		// up, capped at half of the best NIC, so it keeps getting a fair chance to be re-measured.
		for sl := range idle {
			if sl.balanced(f) && sl.peak[f] > 0 {
				sl.peak[f] = math.Min(sl.peak[f]*1.03, math.Max(sl.peak[f], 0.5*maxPeak))
			}
		}
		for _, sl := range s.slots {
			if sl.balanced(f) && sl.peak[f] < minMeasuredPeak {
				measured = false
			}
		}
		if !s.auto {
			continue
		}
		for _, sl := range s.slots {
			if !sl.balanced(f) {
				continue
			}
			if live < 2 || !measured {
				sl.weight[f] = 1
			} else {
				sl.weight[f] = math.Max(0.05, sl.peak[f]/maxPeak)
			}
		}
	}
}

// Acquire picks an IPv4 NIC for a new flow, skipping the ones in skip. The caller must Release it.
func (s *Scheduler) Acquire(skip map[string]struct{}) *Slot { return s.AcquireFor(Fam4, skip) }
func (s *Scheduler) Release(sl *Slot)                       { s.ReleaseFor(sl, Fam4) }

// AcquireFor picks a NIC that has an address of the given family.
func (s *Scheduler) AcquireFor(f Family, skip map[string]struct{}) *Slot {
	return s.acquire(f, skip, nil)
}

// acquire is AcquireFor limited to the NICs allow accepts (nil accepts all). allow runs with s.mu held.
func (s *Scheduler) acquire(f Family, skip map[string]struct{}, allow func(*Slot) bool) *Slot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	// Tiers: 0 = probed up, 1 = not probed yet, 2 = probe failed or link error cooldown.
	// Only the best non-empty tier is used, so a dead tether gets no flows but, if every NIC
	// looks dead, flows still go somewhere rather than nowhere.
	var tiers [3][]*Slot
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
		if allow == nil && sl.kind != nics.KindInternet {
			continue // a VPN or a LAN-only adapter is only used when a rule names it
		}
		t := 1
		switch {
		case now.Before(sl.downUntil[f]) || sl.state[f] == stateDown:
			t = 2
		case sl.state[f] == stateUp:
			t = 0
		}
		tiers[t] = append(tiers[t], sl)
	}
	var pool []*Slot
	for _, tier := range tiers {
		if len(tier) > 0 {
			pool = tier
			break
		}
	}
	if len(pool) == 0 {
		return nil
	}
	// Random start so ties don't resonate with the arrival pattern (a rotating counter can
	// lock onto e.g. "every third flow is a short DNS-like one" and starve an adapter).
	start := rand.IntN(len(pool))
	var best *Slot
	bestScore := math.Inf(1)
	for i := range pool {
		sl := pool[(start+i)%len(pool)]
		if score := float64(sl.actF[f].Load()+1) / sl.weight[f]; score < bestScore {
			best, bestScore = sl, score
		}
	}
	best.active.Add(1)
	best.actF[f].Add(1)
	best.total.Add(1)
	return best
}

// AutoDefault is the adapter that serves as the default when the user has not chosen one: wired Ethernet,
// then Wi-Fi, then a phone's tethering, then virtual adapters, taking the first of those that is present
// and not known to be without internet (see nics.PreferredDefault). "" if there is none.
func (s *Scheduler) AutoDefault() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	c := make([]nics.Candidate, 0, len(s.slots))
	for _, sl := range s.slots {
		up := sl.present && sl.ip(Fam4) != nil && !now.Before(sl.downUntil[Fam4]) && sl.state[Fam4] != stateDown
		c = append(c, nics.Candidate{Name: sl.Name, Conn: sl.conn, Kind: sl.kind, Up: up})
	}
	return nics.PreferredDefault(c)
}

// V6Usable reports whether any adapter can currently carry IPv6.
func (s *Scheduler) V6Usable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sl := range s.slots {
		if sl.present && sl.kind == nics.KindInternet && sl.ip(Fam6) != nil && sl.state[Fam6] != stateDown {
			return true
		}
	}
	return false
}

// V6CoversAll reports whether every adapter that can carry IPv4 can also carry IPv6, i.e. whether
// balancing IPv6 flows would use the same set of adapters as IPv4 does. If not, IPv6 would funnel
// everything onto the adapters that have it.
func (s *Scheduler) V6CoversAll() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	any6 := false
	for _, sl := range s.slots {
		if !sl.present || sl.kind != nics.KindInternet {
			continue
		}
		has6 := sl.ip(Fam6) != nil && sl.state[Fam6] != stateDown
		if has6 {
			any6 = true
		}
		if sl.ip(Fam4) != nil && sl.state[Fam4] != stateDown && !has6 {
			return false
		}
	}
	return any6
}

func (s *Scheduler) ReleaseFor(sl *Slot, f Family) {
	sl.active.Add(-1)
	sl.actF[f].Add(-1)
}

// SetState records a probe result. A passing probe also clears any link-error cooldown.
func (s *Scheduler) SetState(sl *Slot, f Family, up bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if up {
		sl.state[f] = stateUp
		sl.downUntil[f] = time.Time{}
	} else {
		sl.state[f] = stateDown
	}
}

// Fail sidelines a NIC whose link itself is broken (for one address family).
func (s *Scheduler) Fail(sl *Slot, f Family, cooldown time.Duration) {
	s.mu.Lock()
	sl.downUntil[f] = s.now().Add(cooldown)
	s.mu.Unlock()
	sl.failed.Add(1)
}

func (s *Scheduler) Snapshot() []SlotStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SlotStats, 0, len(s.slots))
	for _, sl := range s.slots {
		a := sl.addr.Load()
		st := SlotStats{
			Name: sl.Name, Kind: sl.kind.String(), Conn: sl.conn.String(), Latency: float64(sl.rtt[Fam4].Load()) / 1e6, State: "-", State6: "-", Present: sl.present,
			Weight: sl.weight[Fam4], Weight6: sl.weight[Fam6],
			Active: sl.active.Load(), Total: sl.total.Load(), Failed: sl.failed.Load(),
			BytesIn:  sl.bytesIn[Fam4].Load() + sl.bytesIn[Fam6].Load(),
			BytesOut: sl.bytesOut[Fam4].Load() + sl.bytesOut[Fam6].Load(),
			RateIn:   sl.rateIn[Fam4] + sl.rateIn[Fam6], RateOut: sl.rateOut[Fam4] + sl.rateOut[Fam6],
			LinkIn: sl.linkRateIn, LinkOut: sl.linkRateOut,
			Peak: sl.peak[Fam4], Peak6: sl.peak[Fam6],
		}
		if a.ip4 != nil {
			st.IP, st.State = a.ip4.String(), sl.state[Fam4].String()
		}
		if a.ip6 != nil {
			st.IP6, st.State6 = a.ip6.String(), sl.state[Fam6].String()
		}
		out = append(out, st)
	}
	return out
}

// balanced reports whether the adapter takes part in plain balancing for family f.
func (sl *Slot) balanced(f Family) bool {
	return sl.present && sl.ip(f) != nil && sl.kind == nics.KindInternet
}

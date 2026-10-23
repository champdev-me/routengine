package engine

import (
	"context"
	"net"
	"path"
	"routengine/internal/procmap"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"routengine/internal/usage"
)

// An AppMeter counts one application's traffic through RoutEngine: bytes each way, open flows, and
// which adapters those flows are on.
type AppMeter struct {
	Name, Exe string

	in, out atomic.Int64 // bytes received from / sent to the network
	flows   atomic.Int64
	active  atomic.Int64 // unix nanos of the last new flow or byte movement

	vmu sync.Mutex
	via map[string]int64    // adapter -> open flows
	ad  map[string]*adBytes // adapter -> bytes this app moved through it

	limIn, limOut atomic.Pointer[rate.Limiter] // speed limits, shared by all the app's flows; nil is no limit
	denied        atomic.Bool                  // the app used up its data allowance: its traffic stops

	// guarded by AppRegistry.mu
	lastIn, lastOut int64
	rateIn, rateOut float64
}

// AppStats is a point-in-time copy of an AppMeter, safe to encode and hand to a UI.
type AppStats struct {
	Name, Exe          string
	Down, Up           float64 // bytes/s over the last sample (half a second)
	TotalDown, TotalUp int64
	Flows              int64
	Via                map[string]int64 // adapter -> open flows
}

const (
	SampleEvery  = 500 * time.Millisecond // how often per-app rates are measured and the status file is written
	appShowFor   = 2 * time.Minute        // an idle app stays listed this long
	appForgetFor = 10 * time.Minute       // and its counters are dropped after this long without flows
)

// AppRegistry holds a meter per application, keyed by executable path.
type AppRegistry struct {
	mu     sync.Mutex
	by     map[string]*AppMeter
	start  sync.Once
	ledger *usage.Ledger // if set, every sample is also added to the usage history
}

// Apps is the process-wide registry the data path reports into.
var Apps = &AppRegistry{by: map[string]*AppMeter{}}

// For returns the meter of the application at exe, creating it on first use. An empty exe is the
// catch-all for flows whose owner could not be identified.
func (r *AppRegistry) For(exe string) *AppMeter {
	r.start.Do(func() { go r.run() })
	key := strings.ToLower(exe)
	r.mu.Lock()
	m := r.by[key]
	if m == nil {
		name := path.Base(strings.ReplaceAll(exe, `\`, "/")) // either separator, so a Windows path reads the same everywhere
		if app := procmap.Bundle(exe); app != "" {
			name = path.Base(app) // a macOS app's helper programs are listed as the app
		}
		if exe == "" {
			name = "(unidentified)"
		}
		m = &AppMeter{Name: name, Exe: exe, via: map[string]int64{}}
		r.by[key] = m
	}
	r.mu.Unlock()
	m.active.Store(time.Now().UnixNano())
	return m
}

func (r *AppRegistry) run() {
	last := time.Now()
	for now := range time.Tick(SampleEvery) {
		r.tick(now.Sub(last), now)
		last = now
	}
}

// tick turns the byte counters into per-second rates, passes the bytes to the usage history, and forgets applications that went quiet.
func (r *AppRegistry) tick(dt time.Duration, now time.Time) {
	sec := dt.Seconds()
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, m := range r.by {
		in, out := m.in.Load(), m.out.Load()
		m.rateIn, m.rateOut = float64(in-m.lastIn)/sec, float64(out-m.lastOut)/sec
		if in != m.lastIn || out != m.lastOut {
			m.active.Store(now.UnixNano())
		}
		m.lastIn, m.lastOut = in, out
		r.record(m)
		if m.flows.Load() == 0 && now.Sub(time.Unix(0, m.active.Load())) > appForgetFor {
			delete(r.by, k)
		}
	}
}

// Snapshot lists the applications that are open or were active recently.
func (r *AppRegistry) Snapshot() []AppStats {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AppStats, 0, len(r.by))
	for _, m := range r.by {
		if m.flows.Load() == 0 && now.Sub(time.Unix(0, m.active.Load())) > appShowFor {
			continue
		}
		st := AppStats{
			Name: m.Name, Exe: m.Exe, Down: m.rateIn, Up: m.rateOut,
			TotalDown: m.in.Load(), TotalUp: m.out.Load(), Flows: m.flows.Load(),
		}
		m.vmu.Lock()
		if len(m.via) > 0 {
			st.Via = make(map[string]int64, len(m.via))
			for k, v := range m.via {
				st.Via[k] = v
			}
		}
		m.vmu.Unlock()
		out = append(out, st)
	}
	return out
}

func (m *AppMeter) attach(adapter string) {
	m.flows.Add(1)
	m.vmu.Lock()
	m.via[adapter]++
	m.vmu.Unlock()
}

func (m *AppMeter) detach(adapter string) {
	m.flows.Add(-1)
	m.vmu.Lock()
	if m.via[adapter]--; m.via[adapter] <= 0 {
		delete(m.via, adapter)
	}
	m.vmu.Unlock()
}

// meterConn starts counting c's traffic for m. Connections that are not ours pass through untouched.
func meterConn(c net.Conn, m *AppMeter) net.Conn {
	if sc, ok := c.(*slotConn); ok && m != nil {
		sc.m, sc.mb = m, m.bytesFor(sc.sl.Name)
		sc.ctx, sc.cancel = context.WithCancel(context.Background())
		m.attach(sc.sl.Name)
	}
	return c
}

// meterPacket is meterConn for UDP.
func meterPacket(pc net.PacketConn, m *AppMeter) net.PacketConn {
	if sp, ok := pc.(*slotPacketConn); ok && m != nil {
		sp.m, sp.mb = m, m.bytesFor(sp.sl.Name)
		sp.ctx, sp.cancel = context.WithCancel(context.Background())
		m.attach(sp.sl.Name)
	}
	return pc
}

// adBytes is the traffic one application moved through one adapter. lastIn and lastOut belong to
// the registry's sampling and are only touched under AppRegistry.mu.
type adBytes struct {
	in, out         atomic.Int64
	lastIn, lastOut int64
}

// bytesFor returns the counters for this application on one adapter, creating them on first use.
func (m *AppMeter) bytesFor(adapter string) *adBytes {
	m.vmu.Lock()
	defer m.vmu.Unlock()
	if m.ad == nil {
		m.ad = map[string]*adBytes{}
	}
	b := m.ad[adapter]
	if b == nil {
		b = &adBytes{}
		m.ad[adapter] = b
	}
	return b
}

// record moves what m carried since the last sample into the usage history. The caller holds r.mu.
func (r *AppRegistry) record(m *AppMeter) {
	if r.ledger == nil {
		return
	}
	type pair struct {
		name string
		b    *adBytes
	}
	m.vmu.Lock()
	pairs := make([]pair, 0, len(m.ad))
	for name, b := range m.ad {
		pairs = append(pairs, pair{name, b})
	}
	m.vmu.Unlock()
	for _, p := range pairs {
		in, out := p.b.in.Load(), p.b.out.Load()
		r.ledger.AddApp(m.Exe, m.Name, p.name, in-p.b.lastIn, out-p.b.lastOut)
		p.b.lastIn, p.b.lastOut = in, out
	}
}

// SetLedger makes the registry keep a usage history from now on.
func (r *AppRegistry) SetLedger(l *usage.Ledger) {
	r.mu.Lock()
	r.ledger = l
	r.mu.Unlock()
}

// Flush brings the usage history up to date and saves it. Call it before the program exits.
func (r *AppRegistry) Flush() {
	r.mu.Lock()
	l := r.ledger
	for _, m := range r.by {
		r.record(m)
	}
	r.mu.Unlock()
	if l != nil {
		l.Flush()
	}
}

// UsageReport is today's and this month's usage, for the status the window reads.
func (r *AppRegistry) UsageReport() usage.Report {
	r.mu.Lock()
	l := r.ledger
	r.mu.Unlock()
	if l == nil {
		return usage.Report{}
	}
	return l.Report()
}

// Each calls fn for every application the registry knows about.
func (r *AppRegistry) Each(fn func(*AppMeter)) {
	r.mu.Lock()
	ms := make([]*AppMeter, 0, len(r.by))
	for _, m := range r.by {
		ms = append(ms, m)
	}
	r.mu.Unlock()
	for _, m := range ms {
		fn(m)
	}
}

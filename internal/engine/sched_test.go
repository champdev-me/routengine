package engine

import (
	"net"
	"testing"
	"time"

	"routengine/internal/nics"
)

func nic(name, ip string, w float64) nics.Nic {
	return nics.Nic{Name: name, IP: net.ParseIP(ip), Weight: w}
}

func countBy(picks []*Slot, name string) int {
	n := 0
	for _, p := range picks {
		if p.Name == name {
			n++
		}
	}
	return n
}

func acquireN(s *Scheduler, n int) []*Slot {
	out := make([]*Slot, n)
	for i := range out {
		out[i] = s.Acquire(nil)
	}
	return out
}

func TestEqualWeightsSpreadEvenly(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	p := acquireN(s, 6)
	if countBy(p, "A") != 3 || countBy(p, "B") != 3 {
		t.Fatalf("want 3/3, got A=%d B=%d", countBy(p, "A"), countBy(p, "B"))
	}
}

func TestWeightsSkew(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 3), nic("B", "10.0.0.2", 1)})
	p := acquireN(s, 8)
	if countBy(p, "A") != 6 || countBy(p, "B") != 2 {
		t.Fatalf("want 6/2, got A=%d B=%d", countBy(p, "A"), countBy(p, "B"))
	}
}

func TestReleaseFreesNic(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	first := s.Acquire(nil)
	s.Acquire(nil)
	s.Release(first)
	if got := s.Acquire(nil); got != first {
		t.Fatalf("want the released NIC %s, got %s", first.Name, got.Name)
	}
}

func TestFailCooldown(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	clock := time.Unix(1000, 0)
	s.now = func() time.Time { return clock }
	bad := s.Snapshot()[0]
	var badSlot *Slot
	for _, sl := range s.slots {
		if sl.Name == bad.Name {
			badSlot = sl
		}
	}
	s.Fail(badSlot, Fam4, 5*time.Second)
	for i := 0; i < 4; i++ {
		if s.Acquire(nil) == badSlot {
			t.Fatal("cooling-down NIC was picked")
		}
	}
	clock = clock.Add(6 * time.Second)
	seen := false
	for i := 0; i < 6; i++ {
		if s.Acquire(nil) == badSlot {
			seen = true
		}
	}
	if !seen {
		t.Fatal("NIC never came back after cooldown")
	}
}

func TestAllDownStillServes(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
	s.Fail(s.slots[0], Fam4, time.Hour)
	if s.Acquire(nil) == nil {
		t.Fatal("total outage: all-NICs-down must still hand one out")
	}
}

func TestProbeStateTiers(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1), nic("C", "10.0.0.3", 1)})
	a, b, c := s.slots[0], s.slots[1], s.slots[2]
	s.SetState(a, Fam4, true)
	s.SetState(c, Fam4, false)
	// A is up, B is unprobed, C is dead: only A gets flows while it is the best tier.
	for i := 0; i < 5; i++ {
		if got := s.Acquire(nil); got != a {
			t.Fatalf("flow %d went to %s, want the probed-up NIC", i, got.Name)
		}
	}
	s.SetState(a, Fam4, false) // now B (unknown) beats the two dead ones
	if got := s.Acquire(nil); got != b {
		t.Fatalf("want unprobed NIC over dead ones, got %s", got.Name)
	}
	s.SetState(b, Fam4, false) // everything dead: still serve, never an outage
	if s.Acquire(nil) == nil {
		t.Fatal("all-dead must still hand out a NIC")
	}
	s.SetState(c, Fam4, true) // a recovered NIC takes over immediately
	if got := s.Acquire(nil); got != c {
		t.Fatalf("recovered NIC should win, got %s", got.Name)
	}
}

func TestProbeAgainstLocalListener(t *testing.T) {
	needLoopbackAliases(t)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	old := probeTargets[Fam4]
	defer func() { probeTargets[Fam4] = old }()
	probeTargets[Fam4] = []string{ln.Addr().String()}

	s := NewScheduler([]nics.Nic{nic("A", "127.0.0.2", 1)})
	if !probe(s.slots[0], Fam4) {
		t.Fatal("probe should pass against a reachable target")
	}
	ln.Close()
	if probe(s.slots[0], Fam4) {
		t.Fatal("probe should fail once the target is gone")
	}
}

func TestIPv6OnlyGoesToNicsWithV6(t *testing.T) {
	n6 := nic("B", "10.0.0.2", 1)
	n6.IP6 = net.ParseIP("2001:db8::2")
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), n6})
	for i := 0; i < 4; i++ {
		if got := s.AcquireFor(Fam6, nil); got == nil || got.Name != "B" {
			t.Fatalf("v6 flow %d went to %v, want the only v6-capable NIC B", i, got)
		}
	}
	if got := s.AcquireFor(Fam4, nil); got == nil {
		t.Fatal("v4 flows must still work")
	}
	s2 := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
	if s2.AcquireFor(Fam6, nil) != nil {
		t.Fatal("no v6-capable NIC: v6 flows must be refused so apps fall back to IPv4")
	}
}

func TestV6StateIsIndependentOfV4(t *testing.T) {
	n := nic("A", "10.0.0.1", 1)
	n.IP6 = net.ParseIP("2001:db8::1")
	m := nic("B", "10.0.0.2", 1)
	m.IP6 = net.ParseIP("2001:db8::2")
	s := NewScheduler([]nics.Nic{n, m})
	a, b := s.slots[0], s.slots[1]
	s.SetState(a, Fam4, true)
	s.SetState(b, Fam4, true)
	s.SetState(a, Fam6, false) // A has working v4 but its v6 is dead
	s.SetState(b, Fam6, true)
	for i := 0; i < 4; i++ {
		got := s.AcquireFor(Fam6, nil)
		if got != b {
			t.Fatalf("v6 flow %d went to %s, want B", i, got.Name)
		}
		s.ReleaseFor(got, Fam6)
	}
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		seen[s.AcquireFor(Fam4, nil).Name] = true
	}
	if !seen["A"] || !seen["B"] {
		t.Fatalf("v4 should still use both NICs, got %v", seen)
	}
}

// A hotspot that is fast over native IPv6 but slow over translated IPv4 must be weighted per
// family: nearly starved for v4 flows, fully used for v6 flows.
func TestAutoWeightsArePerFamily(t *testing.T) {
	a := nic("A", "10.0.0.1", 1)
	a.IP6 = net.ParseIP("2001:db8::1")
	b := nic("B", "10.0.0.2", 1)
	b.IP6 = net.ParseIP("2001:db8::2")
	s := NewScheduler([]nics.Nic{a, b})
	s.SetAuto(true)
	sa, sb := s.slots[0], s.slots[1]
	const mbit = 125_000 // bytes/s
	// v4: A 100 Mbit/s, B 5 Mbit/s.  v6: both 100 Mbit/s.
	for i := 0; i < 3; i++ {
		sa.bytesIn[Fam4].Add(100 * mbit)
		sb.bytesIn[Fam4].Add(5 * mbit)
		sa.bytesIn[Fam6].Add(100 * mbit)
		sb.bytesIn[Fam6].Add(100 * mbit)
		s.Tick(time.Second)
	}
	if got := sb.weight[Fam4] / sa.weight[Fam4]; got > 0.06 {
		t.Fatalf("slow v4 path should be down-weighted to ~0.05, got ratio %.2f", got)
	}
	if got := sb.weight[Fam6] / sa.weight[Fam6]; got < 0.95 {
		t.Fatalf("fast v6 path must keep full weight, got ratio %.2f", got)
	}
	// And the peak is remembered while idle (no traffic for a while).
	for i := 0; i < 20; i++ {
		s.Tick(time.Second)
	}
	if sb.peak[Fam6] < 90*mbit {
		t.Fatalf("idle NIC forgot its capacity: peak %.0f", sb.peak[Fam6])
	}
}

// A NIC whose estimate was poisoned by one bad moment must not stay starved forever.
func TestPessimisticEstimateRecoversWhileIdle(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	s.SetAuto(true)
	sa, sb := s.slots[0], s.slots[1]
	const mbit = 125_000
	for i := 0; i < 3; i++ { // B looks 20x slower than A
		sa.bytesIn[Fam4].Add(100 * mbit)
		sb.bytesIn[Fam4].Add(5 * mbit)
		s.Tick(time.Second)
	}
	if w := sb.weight[Fam4] / sa.weight[Fam4]; w > 0.06 {
		t.Fatalf("setup: B should be down-weighted, ratio %.2f", w)
	}
	for i := 0; i < 80; i++ { // then nobody uses B, and A keeps working
		sa.bytesIn[Fam4].Add(100 * mbit)
		s.Tick(time.Second)
	}
	if w := sb.weight[Fam4] / sa.weight[Fam4]; w < 0.45 {
		t.Fatalf("idle NIC should drift back up to ~0.5 so it is re-measured, ratio %.2f", w)
	}
}

func TestV6CoverageDecidesWhetherIPv6IsBalanced(t *testing.T) {
	a := nic("A", "10.0.0.1", 1)
	a.IP6 = net.ParseIP("2001:db8::1")
	b := nic("B", "10.0.0.2", 1) // IPv4 only
	s := NewScheduler([]nics.Nic{a, b})
	if s.V6CoversAll() {
		t.Fatal("B has no IPv6: IPv6 would funnel onto A, coverage must be partial")
	}
	if !s.V6Usable() {
		t.Fatal("A can carry IPv6")
	}
	b.IP6 = net.ParseIP("2001:db8::2")
	s.Update([]nics.Nic{a, b})
	if !s.V6CoversAll() {
		t.Fatal("every adapter has IPv6 now")
	}
	s.SetState(s.slots[1], Fam6, false) // B's IPv6 probe fails
	if s.V6CoversAll() {
		t.Fatal("a dead IPv6 path on B means partial coverage again")
	}
	only4 := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
	if only4.V6CoversAll() || only4.V6Usable() {
		t.Fatal("no IPv6 anywhere: nothing to allow")
	}
}

func TestSkipSet(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	a, b := s.slots[0], s.slots[1]
	if got := s.Acquire(map[string]struct{}{a.key: {}}); got != b {
		t.Fatal("skip set not honoured")
	}
	if got := s.Acquire(map[string]struct{}{a.key: {}, b.key: {}}); got != nil {
		t.Fatal("want nil when everything is skipped")
	}
}

func TestUpdateAddsAndKeepsCounters(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
	s.Acquire(nil)
	s.Update([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	snap := s.Snapshot()
	if len(snap) != 2 || snap[0].Total != 1 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

func TestUpdateDropsVanishedWhenIdle(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	busy := s.Acquire(nil)
	s.Update(nil)
	if len(s.Snapshot()) != 1 {
		t.Fatalf("idle NIC should be dropped, busy one kept: %+v", s.Snapshot())
	}
	if s.Acquire(nil) != nil {
		t.Fatal("vanished NIC must not get new flows")
	}
	s.Release(busy)
	s.Update(nil)
	if len(s.Snapshot()) != 0 {
		t.Fatal("NIC should be dropped once idle")
	}
}

func kindNic(name, ip string, k nics.Kind) nics.Nic {
	return nics.Nic{Name: name, IP: net.ParseIP(ip), Weight: 1, Kind: k}
}

func TestPlainBalancingNeverUsesAVPNOrALANOnlyAdapter(t *testing.T) {
	s := NewScheduler([]nics.Nic{
		kindNic("WiFi", "10.0.0.1", nics.KindInternet),
		kindNic("Tailscale", "100.64.0.1", nics.KindVirtual),
		kindNic("Cable to NAS", "10.9.0.1", nics.KindLAN),
	})
	for i := 0; i < 20; i++ {
		sl := s.Acquire(nil)
		if sl == nil || sl.Name != "WiFi" {
			t.Fatalf("flow %d went to %v: a VPN or LAN adapter must not receive plain balanced flows", i, sl)
		}
		s.Release(sl)
	}
}

func TestAVPNStillWorksWhenARuleNamesIt(t *testing.T) {
	s := NewScheduler([]nics.Nic{
		kindNic("WiFi", "10.0.0.1", nics.KindInternet),
		kindNic("Tailscale", "100.64.0.1", nics.KindVirtual),
	})
	sl, err := s.AcquireNamed(Fam4, "tailscale")
	if err != nil || sl.Name != "Tailscale" {
		t.Fatalf("pinning to a VPN adapter must work: %v %v", sl, err)
	}
	s.Release(sl)
	// A rule's explicit pool may include it too.
	allow := s.poolAllow(Fam4, []string{"Tailscale", "WiFi"})
	seen := map[string]bool{}
	for i := 0; i < 12; i++ {
		p := s.acquire(Fam4, nil, allow)
		seen[p.Name] = true
		s.Release(p)
	}
	if !seen["Tailscale"] {
		t.Fatalf("a pool that names the VPN may use it, saw %v", seen)
	}
}

func TestANonInternetAdapterIsNeverProbedForInternet(t *testing.T) {
	s := NewScheduler([]nics.Nic{
		kindNic("WiFi", "10.0.0.1", nics.KindInternet),
		kindNic("Tailscale", "100.64.0.1", nics.KindVirtual),
	})
	due := s.probeDue()
	for _, j := range due {
		if j.sl.Name == "Tailscale" {
			t.Fatal("a tailnet-only link has no internet to probe; probing would mark it down")
		}
	}
	if len(due) == 0 {
		t.Fatal("the internet adapter still needs its probe")
	}
	s.mu.Lock()
	state := s.slots[1].state[Fam4]
	s.mu.Unlock()
	if state != stateUp {
		t.Fatalf("the VPN counts as up while it exists, got %v", state)
	}
	if sl, err := s.AcquireNamed(Fam4, "Tailscale"); err != nil || sl == nil {
		t.Fatalf("so a pinned flow is allowed: %v", err)
	}
}

func TestIPv6DecisionsIgnoreAdaptersThatAreNotInPlainBalancing(t *testing.T) {
	v6 := func(name string, k nics.Kind) nics.Nic {
		return nics.Nic{Name: name, IP: net.ParseIP("10.0.0.1"), IP6: net.ParseIP("2001:db8::1"), Weight: 1, Kind: k}
	}
	// One internet adapter with IPv4 only, and a VPN that has IPv6: IPv6 must not look usable for balancing.
	s := NewScheduler([]nics.Nic{kindNic("WiFi", "10.0.0.2", nics.KindInternet), v6("VPN", nics.KindVirtual)})
	if s.V6Usable() {
		t.Fatal("a VPN's IPv6 must not make plain balancing think IPv6 is usable")
	}
	if s.V6CoversAll() {
		t.Fatal("and it must not count as covering the internet adapters")
	}
}

func TestAutoDefaultFollowsTheOrderAndSkipsWhatIsDown(t *testing.T) {
	eth, wifi, phone, vpn := nic("Ethernet", "10.0.0.1", 1), nic("WiFi", "10.0.0.2", 1), nic("Ethernet 4", "10.0.0.3", 1), nic("Tailscale", "100.64.0.1", 1)
	eth.Conn, wifi.Conn, phone.Conn = nics.ConnEthernet, nics.ConnWiFi, nics.ConnTether
	vpn.Conn, vpn.Kind = nics.ConnVirtual, nics.KindVirtual
	s := NewScheduler([]nics.Nic{vpn, phone, wifi, eth})

	if got := s.AutoDefault(); got != "Ethernet" {
		t.Fatalf("all up: %q, want Ethernet", got)
	}
	slot := func(name string) *Slot {
		for _, sl := range s.slots {
			if sl.Name == name {
				return sl
			}
		}
		t.Fatalf("no slot %s", name)
		return nil
	}
	slot("Ethernet").state[Fam4] = stateDown
	if got := s.AutoDefault(); got != "WiFi" {
		t.Fatalf("wired is down: %q, want WiFi", got)
	}
	slot("WiFi").present = false
	if got := s.AutoDefault(); got != "Ethernet 4" {
		t.Fatalf("Wi-Fi gone: %q, want the phone", got)
	}
	slot("Ethernet 4").state[Fam4] = stateDown
	if got := s.AutoDefault(); got != "Tailscale" {
		t.Fatalf("only the VPN is left: %q", got)
	}
	slot("Tailscale").state[Fam4] = stateDown
	if got := s.AutoDefault(); got != "" {
		t.Fatalf("nothing up: %q", got)
	}
}

func TestAnAdapterPluggedInLaterJoinsTheSchedulerAndCanTakeFlows(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
	if n := len(s.Snapshot()); n != 1 {
		t.Fatalf("before: %d adapters", n)
	}
	s.Update([]nics.Nic{nic("A", "10.0.0.1", 1), nic("Tether", "10.0.0.2", 1)}) // the re-scan found a phone
	snap := s.Snapshot()
	if len(snap) != 2 || !snap[1].Present || snap[1].Name != "Tether" {
		t.Fatalf("the new adapter must appear and be present: %+v", snap)
	}
	got := map[string]bool{}
	for i := 0; i < 20; i++ {
		sl := s.Acquire(nil)
		got[sl.Name] = true
		s.Release(sl)
	}
	if !got["Tether"] {
		t.Fatalf("new flows must start using the new adapter: %v", got)
	}
}

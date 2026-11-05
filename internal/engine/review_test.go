package engine

import (
	"context"
	"net"
	"testing"

	"routengine/internal/nics"
)

// The flows of an app with a rule are tracked like balanced ones, so the scheduler can tell an idle one from a busy one.
// Untracked, every open connection of a pinned app counted as busy for as long as it lived, and plain balancing
// steered other apps away from that adapter (net-merge #48, back for apps with a rule).
func TestFlowsOfAnAppWithARuleAreTracked(t *testing.T) {
	for name, rt := range map[string]Route{"pinned": {Adapter: "A"}, "several adapters": {Pool: []string{"A", "B"}}} {
		t.Run(name, func(t *testing.T) {
			b := routeBalancer(t)
			port := startTarget(t)
			rt.Meter = &AppMeter{Name: "x", via: map[string]int64{}}
			c, err := b.DialVia(context.Background(), "127.0.0.1", port, rt)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			sl := c.(*slotConn).sl
			tracked := func() int {
				b.S.mu.Lock()
				defer b.S.mu.Unlock()
				return len(sl.flows[Fam4])
			}
			if n := tracked(); n != 1 {
				t.Fatalf("an open flow of an app with a rule: %d tracked, want 1", n)
			}
			c.Close()
			if n := tracked(); n != 0 {
				t.Fatalf("after closing it: %d tracked, want 0", n)
			}
		})
	}
}

// An adapter that goes away while it still has open flows, and comes back with a new interface index, must be reached
// through the new index: the old one forced every new flow onto an interface that no longer exists.
func TestAnAdapterThatComesBackGetsItsNewIndex(t *testing.T) {
	tether := func(idx int) nics.Nic {
		return nics.Nic{Name: "usb0", Index: idx, IP: net.ParseIP("192.168.42.10"), Weight: 1}
	}
	s := NewScheduler([]nics.Nic{tether(7)})
	sl := s.slots[0]
	sl.active.Add(1) // a flow is still open, so the slot is kept while the adapter is gone
	s.Update(nil)
	s.Update([]nics.Nic{tether(12)})
	if len(s.slots) != 1 || s.slots[0] != sl {
		t.Fatalf("the adapter should keep its slot and counters")
	}
	if got := sl.ifIndex(); got != 12 {
		t.Fatalf("interface index %d, want the new 12", got)
	}
}

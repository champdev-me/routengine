package engine

import (
	"bytes"
	"context"
	"io"
	"net"
	"path/filepath"
	"routengine/internal/nics"
	"routengine/internal/usage"
	"testing"
	"time"
)

func TestMeterCountsBytesFlowsAndAdapter(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	m := &AppMeter{Name: "t", via: map[string]int64{}}

	c, err := b.DialVia(context.Background(), "127.0.0.1", port, Route{Adapter: "B", Meter: m})
	if err != nil {
		t.Fatal(err)
	}
	if m.flows.Load() != 1 || m.via["B"] != 1 {
		t.Fatalf("an open flow must show on its adapter: flows=%d via=%v", m.flows.Load(), m.via)
	}
	if _, err := c.Write(bytes.Repeat([]byte("x"), 1000)); err != nil {
		t.Fatal(err)
	}
	// The target greets with the address it saw ("127.0.0.3\n", 10 bytes), then echoes.
	if _, err := io.ReadFull(c, make([]byte, 10+1000)); err != nil {
		t.Fatal(err)
	}
	if m.out.Load() != 1000 || m.in.Load() != 1010 {
		t.Fatalf("want 1000 up / 1010 down, got %d / %d", m.out.Load(), m.in.Load())
	}
	c.Close()
	if m.flows.Load() != 0 || len(m.via) != 0 {
		t.Fatalf("a closed flow must leave no trace: flows=%d via=%v", m.flows.Load(), m.via)
	}
}

func TestMeterCountsBalancedFlowsToo(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	m := &AppMeter{Name: "t", via: map[string]int64{}}
	var conns []net.Conn
	for i := 0; i < 4; i++ {
		c, err := b.DialVia(context.Background(), "127.0.0.1", port, Route{Meter: m}) // no rule: plain balancing
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	if m.flows.Load() != 4 || len(m.via) != 2 {
		t.Fatalf("four balanced flows should sit on both adapters: flows=%d via=%v", m.flows.Load(), m.via)
	}
	for _, c := range conns {
		c.Close()
	}
	if m.flows.Load() != 0 {
		t.Fatalf("flows left open: %d", m.flows.Load())
	}
}

func TestMeterCountsUDP(t *testing.T) {
	b := routeBalancer(t)
	srv, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	m := &AppMeter{Name: "t", via: map[string]int64{}}
	pc, err := b.ListenUDPVia(context.Background(), Fam4, Route{Adapter: "A", Meter: m})
	if err != nil {
		t.Fatal(err)
	}
	if m.flows.Load() != 1 {
		t.Fatalf("flows=%d", m.flows.Load())
	}
	pc.WriteTo(make([]byte, 300), srv.LocalAddr())
	if m.out.Load() != 300 {
		t.Fatalf("want 300 bytes up, got %d", m.out.Load())
	}
	pc.Close()
	pc.Close() // closing twice must not drive the count negative
	if m.flows.Load() != 0 {
		t.Fatalf("flows=%d after close", m.flows.Load())
	}
}

func TestRegistryTurnsBytesIntoRatesAndForgetsIdleApps(t *testing.T) {
	r := &AppRegistry{by: map[string]*AppMeter{}}
	r.start.Do(func() {}) // no background ticker: the test drives it
	m := r.For(`C:\Apps\Foo.exe`)
	if r.For(`c:\apps\foo.EXE`) != m {
		t.Fatal("the same executable must share one meter, whatever the case")
	}
	if m.Name != "Foo.exe" {
		t.Fatalf("name %q", m.Name)
	}
	now := time.Now()
	m.in.Add(5000)
	m.out.Add(700)
	r.tick(time.Second, now)
	snap := r.Snapshot()
	if len(snap) != 1 || snap[0].Down != 5000 || snap[0].Up != 700 || snap[0].TotalDown != 5000 {
		t.Fatalf("got %+v", snap)
	}
	r.tick(time.Second, now.Add(time.Second)) // nothing moved this second
	if s := r.Snapshot(); s[0].Down != 0 {
		t.Fatalf("rate must fall back to 0, got %+v", s)
	}
	r.tick(time.Second, now.Add(appForgetFor+time.Minute))
	if len(r.by) != 0 {
		t.Fatal("an app idle for longer than appForgetFor should be dropped")
	}
	if r.For("").Name != "(unidentified)" {
		t.Fatal("empty exe is the catch-all meter")
	}
}

func TestUsageIsRecordedPerAdapter(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	l, _ := usage.Open(filepath.Join(t.TempDir(), "usage.json"))
	r := &AppRegistry{by: map[string]*AppMeter{}}
	r.start.Do(func() {}) // the test drives the sampling
	r.SetLedger(l)
	m := r.For(`C:\x\app.exe`)

	for _, ad := range []string{"A", "B", "B"} {
		c, err := b.DialVia(context.Background(), "127.0.0.1", port, Route{Adapter: ad, Meter: m})
		if err != nil {
			t.Fatal(err)
		}
		c.Write(make([]byte, 500))
		io.ReadFull(c, make([]byte, 10+500)) // the greeting, then the echo
		c.Close()
	}
	r.tick(SampleEvery, time.Now())
	r.tick(SampleEvery, time.Now()) // a second sample must add nothing new

	rep := l.Report()
	if len(rep.Apps) != 1 {
		t.Fatalf("report: %+v", rep)
	}
	via := rep.Apps[0].TodayVia
	if via["A"] != (usage.Bytes{Down: 510, Up: 500}) || via["B"] != (usage.Bytes{Down: 1020, Up: 1000}) {
		t.Fatalf("by adapter: %+v", via)
	}
	if got := rep.Apps[0].Today; got != (usage.Bytes{Down: 1530, Up: 1500}) {
		t.Fatalf("total today: %+v", got)
	}
}

func TestLinkFeederAddsOnlyWhatWasCarriedSince(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
	l, _ := usage.Open(filepath.Join(t.TempDir(), "usage.json"))
	set := func(in, out uint64) {
		s.mu.Lock()
		s.slots[0].linkPrimed, s.slots[0].linkIn, s.slots[0].linkOut = true, in, out
		s.mu.Unlock()
	}
	var f LinkFeeder
	today := func() usage.Bytes {
		for _, a := range l.Report().Adapters {
			if a.Name == "A" {
				return a.Today
			}
		}
		return usage.Bytes{}
	}

	set(1000, 200)
	f.Feed(s, l) // first sight: a baseline, not usage
	if today().Total() != 0 {
		t.Fatalf("the baseline must not count: %+v", today())
	}
	set(1600, 300)
	f.Feed(s, l)
	if today() != (usage.Bytes{Down: 600, Up: 100}) {
		t.Fatalf("got %+v", today())
	}
	set(50, 10) // the adapter reconnected and its counters restarted
	f.Feed(s, l)
	set(250, 40)
	f.Feed(s, l)
	if today() != (usage.Bytes{Down: 800, Up: 130}) {
		t.Fatalf("after a counter reset: %+v", today())
	}
}

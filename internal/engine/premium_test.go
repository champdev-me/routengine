package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"routengine/internal/nics"
)

func dialMetered(t *testing.T, b *Balancer, port int, rt Route) net.Conn {
	t.Helper()
	c, err := b.DialVia(context.Background(), "127.0.0.1", port, rt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestAnUploadLimitSlowsTheAppDownToTheLimit(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	data := bytes.Repeat([]byte("x"), 1_300_000)

	free := &AppMeter{Name: "free", via: map[string]int64{}}
	c := dialMetered(t, b, port, Route{Adapter: "A", Meter: free})
	go io.Copy(io.Discard, c) // the target echoes everything; unread, it fills the socket buffers and stalls the write
	start := time.Now()
	c.Write(data)
	unlimited := time.Since(start)

	slow := &AppMeter{Name: "slow", via: map[string]int64{}}
	slow.SetLimits(0, 1*BytesPerMbit*8) // 1 MB/s up, no limit down
	c2 := dialMetered(t, b, port, Route{Adapter: "A", Meter: slow})
	go io.Copy(io.Discard, c2)
	start = time.Now()
	c2.Write(data)
	limited := time.Since(start)

	// 1.3 MB at 1 MB/s with a quarter-second burst takes about a second; without the limit it is instant.
	if limited < 700*time.Millisecond || limited > 4*time.Second {
		t.Fatalf("a 1 MB/s limit should make 1.3 MB take about a second, took %v (unlimited took %v)", limited, unlimited)
	}
	if unlimited > limited/2 {
		t.Fatalf("the unlimited app must be clearly faster: %v vs %v", unlimited, limited)
	}
	if _, up := slow.Limits(); up != 1*BytesPerMbit*8 {
		t.Fatalf("limits readback: %v", up)
	}
}

func TestADownloadLimitHoldsDataBackAndRemovingItReleasesTheApp(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	m := &AppMeter{Name: "m", via: map[string]int64{}}
	m.SetLimits(500_000, 0) // 0.5 MB/s down
	c := dialMetered(t, b, port, Route{Adapter: "A", Meter: m})

	go c.Write(bytes.Repeat([]byte("y"), 1_000_000)) // the target echoes it back
	start := time.Now()
	n, _ := io.ReadFull(c, make([]byte, 10+1_000_000))
	took := time.Since(start)
	if n < 1_000_000 || took < 1*time.Second {
		t.Fatalf("1 MB at 0.5 MB/s should take over a second, got %d bytes in %v", n, took)
	}

	m.SetLimits(0, 0)
	if d, u := m.Limits(); d != 0 || u != 0 {
		t.Fatalf("limits are removed: %v %v", d, u)
	}
}

func TestASpeedLimitDoesNotOutliveItsConnection(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	m := &AppMeter{Name: "m", via: map[string]int64{}}
	m.SetLimits(0, 1000) // 1 KB/s: a big write would sleep for minutes
	c := dialMetered(t, b, port, Route{Adapter: "A", Meter: m})
	done := make(chan struct{})
	go func() { c.Write(make([]byte, 5_000_000)); close(done) }()
	time.Sleep(100 * time.Millisecond)
	c.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("closing the connection must end a throttled wait at once")
	}
}

func TestADeniedAppIsCutOffAndRefused(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	m := &AppMeter{Name: "m", via: map[string]int64{}}
	c := dialMetered(t, b, port, Route{Adapter: "A", Meter: m})
	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}

	m.SetDenied(true) // the data cap was reached
	if _, err := c.Write([]byte("more")); !errors.Is(err, ErrCapReached) {
		t.Fatalf("an open connection must stop at its next write, got %v", err)
	}
	if _, err := c.Read(make([]byte, 8)); !errors.Is(err, ErrCapReached) {
		t.Fatalf("and at its next read, got %v", err)
	}
	if !m.Denied() {
		t.Fatal("denied reads back")
	}
	m.SetDenied(false)
	if _, err := c.Write([]byte("again")); err != nil {
		t.Fatalf("lifting the cap lets the connection carry on: %v", err)
	}
}

func TestADeniedRouteRefusesNewFlowsWithTheReason(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	if _, err := b.DialVia(context.Background(), "127.0.0.1", port, Route{Adapter: "A", Deny: "data cap reached"}); err == nil || err.Error() != "data cap reached" {
		t.Fatalf("a flow over its cap is refused with the reason, got %v", err)
	}
	if _, err := b.ListenUDPVia(context.Background(), Fam4, Route{Deny: "data cap reached"}); err == nil {
		t.Fatal("and so is a UDP flow")
	}
}

func TestDNSQueriesGoToTheChosenServerAndAnswersLookOriginal(t *testing.T) {
	server, err := net.ListenPacket("udp4", "127.0.0.1:0") // the DNS server the user chose
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := server.ReadFrom(buf)
			if err != nil {
				return
			}
			server.WriteTo(append([]byte("answer:"), buf[:n]...), from)
		}
	}()
	client, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	orig := &net.UDPAddr{IP: net.IPv4(8, 8, 8, 8), Port: 53} // what the app thought it was talking to
	pc := &dnsRedirect{PacketConn: client, orig: orig, to: server.LocalAddr().(*net.UDPAddr)}

	if _, err := pc.WriteTo([]byte("example.com?"), orig); err != nil {
		t.Fatal(err)
	}
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	n, from, err := pc.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "answer:example.com?" {
		t.Fatalf("the chosen server must have answered: %q %v", buf[:n], err)
	}
	if from.String() != orig.String() {
		t.Fatalf("the app must see the answer as coming from %s, not %s", orig, from)
	}
}

func TestFastestPicksTheLowestLatencyAdapterThatIsUp(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1), nic("C", "10.0.0.3", 1)})
	set := func(name string, rtt time.Duration) {
		for _, sl := range s.slots {
			if sl.Name == name {
				sl.rtt[Fam4].Store(int64(rtt))
			}
		}
	}
	set("A", 80*time.Millisecond)
	set("B", 25*time.Millisecond)
	set("C", 40*time.Millisecond)
	allow := s.internetAllow(Fam4)
	pick := func(skip map[string]struct{}) string {
		sl := s.acquireFastest(Fam4, skip, allow)
		if sl == nil {
			return ""
		}
		s.Release(sl)
		return sl.Name
	}
	if got := pick(nil); got != "B" {
		t.Fatalf("the 25 ms adapter wins, got %s", got)
	}
	if got := pick(map[string]struct{}{"B": {}}); got != "C" {
		t.Fatalf("with B tried and failed, C is next: %s", got)
	}
	s.SetState(s.slots[1], Fam4, false) // B goes down
	if got := pick(nil); got != "C" {
		t.Fatalf("a down adapter is never the fastest: %s", got)
	}
	set("C", 0) // C has not been measured yet
	if got := pick(nil); got != "A" {
		t.Fatalf("a measured adapter beats an unmeasured one: %s", got)
	}
}

func TestFastestUsesPinnedPoolsAndAllInternetAdaptersAlike(t *testing.T) {
	b := routeBalancer(t) // A=127.0.0.2, B=127.0.0.3
	port := startTarget(t)
	for _, sl := range b.S.slots {
		if sl.Name == "A" {
			sl.rtt[Fam4].Store(int64(90 * time.Millisecond))
		} else {
			sl.rtt[Fam4].Store(int64(10 * time.Millisecond))
		}
	}
	for _, rt := range []Route{{Fastest: true}, {Fastest: true, Pool: []string{"A", "B"}}} {
		for i := 0; i < 6; i++ {
			if got, err := sourceSeen(t, b, port, rt); err != nil || got != "127.0.0.3" {
				t.Fatalf("route %+v flow %d: want the fast adapter (127.0.0.3), got %q (%v)", rt, i, got, err)
			}
		}
	}
	// A pool that leaves the fast adapter out cannot use it, however fast it is.
	if got, err := sourceSeen(t, b, port, Route{Fastest: true, Pool: []string{"A"}}); err != nil || got != "127.0.0.2" {
		t.Fatalf("pool [A]: got %q (%v)", got, err)
	}
}

func TestLatencyIsSmoothedNotJumpy(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
	sl := s.slots[0]
	sl.noteRTT(Fam4, 20*time.Millisecond)
	if got := sl.rtt[Fam4].Load(); got != int64(20*time.Millisecond) {
		t.Fatalf("the first sample is taken as is: %v", got)
	}
	sl.noteRTT(Fam4, 220*time.Millisecond) // one slow moment
	got := time.Duration(sl.rtt[Fam4].Load())
	if got < 60*time.Millisecond || got > 90*time.Millisecond {
		t.Fatalf("one slow sample should move the estimate only a quarter of the way, got %v", got)
	}
	if st := s.Snapshot(); st[0].Latency < 60 || st[0].Latency > 90 {
		t.Fatalf("the status reports it in ms: %v", st[0].Latency)
	}
}

// A speed limit belongs to the app, not to an adapter: spreading the app over several adapters, or over all of
// them, must not multiply it.
func TestASpeedLimitHoldsTheWholeAppAcrossSeveralAdapters(t *testing.T) {
	for name, pool := range map[string][]string{"several adapters": {"A", "B"}, "use all": nil} {
		t.Run(name, func(t *testing.T) {
			b := routeBalancer(t)
			port := startTarget(t)
			m := &AppMeter{Name: "multi", via: map[string]int64{}}
			m.SetLimits(0, 2_000_000) // 2 MB/s up, for the whole app
			rt := Route{Pool: pool, Meter: m}

			const flows, each = 8, 250_000 // 2 MB in all
			conns := make([]net.Conn, flows)
			for i := range conns {
				conns[i] = dialMetered(t, b, port, rt)
			}
			carried := map[string]int64{}
			for _, st := range b.S.Snapshot() {
				carried[st.Name] = st.Total
			}
			if carried["A"] == 0 || carried["B"] == 0 {
				t.Fatalf("the flows must be spread over both adapters, got %v", carried)
			}

			start := time.Now()
			var wg sync.WaitGroup
			for _, c := range conns {
				wg.Add(1)
				go func(c net.Conn) {
					defer wg.Done()
					c.Write(bytes.Repeat([]byte("z"), each))
				}(c)
			}
			wg.Wait()
			took := time.Since(start)

			// 2 MB at 2 MB/s with a quarter-second burst takes about 0.75 s in all. If each flow, or each adapter,
			// had its own 2 MB/s the same data would be done in a fraction of that.
			if took < 500*time.Millisecond || took > 4*time.Second {
				t.Fatalf("2 MB over two adapters at a 2 MB/s app limit should take about 0.75 s, took %v", took)
			}
		})
	}
}

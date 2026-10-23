package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"routengine/internal/nics"
)

func routeBalancer(t *testing.T) *Balancer {
	t.Helper()
	needLoopbackAliases(t)
	b := NewBalancer(NewScheduler([]nics.Nic{nic("A", "127.0.0.2", 1), nic("B", "127.0.0.3", 1)}))
	b.ConnectTimeout = 2 * time.Second
	return b
}

// sourceSeen dials the target through rt and returns the source address the target saw.
func sourceSeen(t *testing.T, b *Balancer, port int, rt Route) (string, error) {
	t.Helper()
	c, err := b.DialVia(context.Background(), "127.0.0.1", port, rt)
	if err != nil {
		return "", err
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(line), nil
}

func TestPinnedFlowsStayOnTheirAdapter(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	for i := 0; i < 20; i++ { // balancing would alternate; pinning must not
		if got, err := sourceSeen(t, b, port, Route{Adapter: "B"}); err != nil || got != "127.0.0.3" {
			t.Fatalf("flow %d: want 127.0.0.3, got %q (err %v)", i, got, err)
		}
	}
	if got, err := sourceSeen(t, b, port, Route{Adapter: "a"}); err != nil || got != "127.0.0.2" { // names ignore case
		t.Fatalf("want 127.0.0.2, got %q (err %v)", got, err)
	}
}

func TestZeroRouteStillBalances(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		got, err := sourceSeen(t, b, port, Route{})
		if err != nil {
			t.Fatal(err)
		}
		seen[got] = true
	}
	if !seen["127.0.0.2"] || !seen["127.0.0.3"] {
		t.Fatalf("want both adapters used, got %v", seen)
	}
}

func TestStrictRouteNeverEscapes(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	b.S.mu.Lock()
	for _, sl := range b.S.slots {
		if sl.Name == "B" {
			sl.state[Fam4] = stateDown
		}
	}
	b.S.mu.Unlock()

	_, err := sourceSeen(t, b, port, Route{Adapter: "B", OnDown: Block})
	var ue *UnavailableError
	if !errors.As(err, &ue) || ue.Why != "no internet" {
		t.Fatalf("want an UnavailableError (no internet), got %v", err)
	}
	if got, err := sourceSeen(t, b, port, Route{Adapter: "B", OnDown: Merge}); err != nil || got != "127.0.0.2" {
		t.Fatalf("merge fallback: want 127.0.0.2, got %q (err %v)", got, err)
	}
}

func setState(b *Balancer, name string, st linkState) {
	b.S.mu.Lock()
	defer b.S.mu.Unlock()
	for _, sl := range b.S.slots {
		if sl.Name == name {
			sl.state[Fam4] = st
		}
	}
}

func TestFailoverUsesTheBackupThenReturns(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	rt := Route{Adapter: "B", Failover: []string{"A"}}

	if got, err := sourceSeen(t, b, port, rt); err != nil || got != "127.0.0.3" {
		t.Fatalf("healthy: want 127.0.0.3, got %q (err %v)", got, err)
	}
	setState(b, "B", stateDown)
	for i := 0; i < 5; i++ { // every new flow takes the backup, none are refused
		if got, err := sourceSeen(t, b, port, rt); err != nil || got != "127.0.0.2" {
			t.Fatalf("B down: want 127.0.0.2, got %q (err %v)", got, err)
		}
	}
	setState(b, "B", stateUp)
	if got, err := sourceSeen(t, b, port, rt); err != nil || got != "127.0.0.3" {
		t.Fatalf("B back: want 127.0.0.3, got %q (err %v)", got, err)
	}
}

func TestFailoverSkipsMissingBackupsInOrder(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	setState(b, "B", stateDown)
	if got, err := sourceSeen(t, b, port, Route{Adapter: "B", Failover: []string{"Nope", "A"}}); err != nil || got != "127.0.0.2" {
		t.Fatalf("want 127.0.0.2, got %q (err %v)", got, err)
	}
}

func TestFailoverNeverLeavesTheList(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	setState(b, "B", stateDown)
	setState(b, "A", stateDown)
	if got, err := sourceSeen(t, b, port, Route{Adapter: "B", Failover: []string{"A"}}); err == nil {
		t.Fatalf("every listed adapter is down: the flow must be refused, but it left from %q", got)
	}
}

func TestFailoverIsReported(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	var lines []string
	routeLogged.Lock()
	routeLogged.at = nil // the repeat filter must not hide this test's line
	routeLogged.Unlock()
	RouteLog = func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }
	t.Cleanup(func() { RouteLog = nil })
	setState(b, "B", stateDown)
	if _, err := sourceSeen(t, b, port, Route{Adapter: "B", Failover: []string{"A"}}); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], `"B" unavailable: no internet`) || !strings.Contains(lines[0], `"A"`) {
		t.Fatalf("want one failover line naming both adapters, got %q", lines)
	}
}

// threeNics adds a third fake adapter, C, that the pool tests must never touch.
func threeNics(t *testing.T) *Balancer {
	t.Helper()
	needLoopbackAliases(t)
	if ln, err := net.Listen("tcp4", "127.0.0.4:0"); err != nil {
		t.Skip("127.0.0.4 is not bindable; on macOS run: sudo ifconfig lo0 alias 127.0.0.4/32")
	} else {
		ln.Close()
	}
	b := NewBalancer(NewScheduler([]nics.Nic{nic("A", "127.0.0.2", 1), nic("B", "127.0.0.3", 1), nic("C", "127.0.0.4", 1)}))
	b.ConnectTimeout = 2 * time.Second
	return b
}

func TestPoolUsesBothMembersAndNothingElse(t *testing.T) {
	b := threeNics(t)
	port := startTarget(t)
	seen := map[string]int{}
	for i := 0; i < 30; i++ {
		got, err := sourceSeen(t, b, port, Route{Pool: []string{"A", "b"}})
		if err != nil {
			t.Fatal(err)
		}
		seen[got]++
	}
	if seen["127.0.0.2"] == 0 || seen["127.0.0.3"] == 0 {
		t.Fatalf("the app should use both pool adapters at once, got %v", seen)
	}
	if seen["127.0.0.4"] != 0 {
		t.Fatalf("adapter C is not in the pool but carried %d flows", seen["127.0.0.4"])
	}
}

func TestPoolSkipsADownMemberAndCarriesOn(t *testing.T) {
	b := threeNics(t)
	port := startTarget(t)
	setState(b, "B", stateDown)
	for i := 0; i < 10; i++ {
		if got, err := sourceSeen(t, b, port, Route{Pool: []string{"A", "B"}}); err != nil || got != "127.0.0.2" {
			t.Fatalf("B is down: want every flow on A (127.0.0.2), got %q (err %v)", got, err)
		}
	}
}

func TestPoolWithEveryMemberDownBlocksOrMerges(t *testing.T) {
	b := threeNics(t)
	port := startTarget(t)
	setState(b, "A", stateDown)
	setState(b, "B", stateDown)
	if got, err := sourceSeen(t, b, port, Route{Pool: []string{"A", "B"}}); err == nil {
		t.Fatalf("both pool adapters are down, C must not take over, but the flow left from %q", got)
	}
	if got, err := sourceSeen(t, b, port, Route{Pool: []string{"A", "B"}, OnDown: Merge}); err != nil || got != "127.0.0.4" {
		t.Fatalf("on_down merge: want C (127.0.0.4), got %q (err %v)", got, err)
	}
}

func TestPoolUDPStaysInsideThePool(t *testing.T) {
	b := threeNics(t)
	srv, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	dst := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: srv.LocalAddr().(*net.UDPAddr).Port}
	seen := map[string]bool{}
	for i := 0; i < 30; i++ {
		pc, err := b.ListenUDPVia(context.Background(), Fam4, Route{Pool: []string{"A", "B"}})
		if err != nil {
			t.Fatal(err)
		}
		pc.WriteTo([]byte("x"), dst)
		srv.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, from, err := srv.ReadFrom(make([]byte, 8))
		if err != nil {
			t.Fatal(err)
		}
		host, _, _ := net.SplitHostPort(from.String())
		seen[host] = true
		pc.Close()
	}
	if seen["127.0.0.4"] || !seen["127.0.0.2"] || !seen["127.0.0.3"] {
		t.Fatalf("want datagrams from A and B only, got %v", seen)
	}
}

func TestUnknownAdapterIsRefused(t *testing.T) {
	b := routeBalancer(t)
	port := startTarget(t)
	if _, err := sourceSeen(t, b, port, Route{Adapter: "Nope"}); err == nil {
		t.Fatal("a rule for an adapter that is not connected must not fall back to another one")
	}
}

func TestPinnedUDPLeavesThroughItsAdapter(t *testing.T) {
	b := routeBalancer(t)
	srv, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	port := srv.LocalAddr().(*net.UDPAddr).Port
	for i := 0; i < 6; i++ {
		pc, err := b.ListenUDPVia(context.Background(), Fam4, Route{Adapter: "B"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pc.WriteTo([]byte("x"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}); err != nil {
			t.Fatal(err)
		}
		srv.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 8)
		_, from, err := srv.ReadFrom(buf)
		if err != nil {
			t.Fatal(err)
		}
		if host, _, _ := net.SplitHostPort(from.String()); host != "127.0.0.3" {
			t.Fatalf("datagram %d came from %s, want 127.0.0.3", i, host)
		}
		pc.Close()
	}
}

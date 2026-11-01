package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"routengine/internal/nics"
)

// OnDown is what a pinned flow does when its adapter, and every failover adapter, cannot carry it.
type OnDown int

const (
	// Block refuses the flow: it never leaves through an adapter the rule did not allow (strict routing).
	Block OnDown = iota
	// Merge falls back to balancing across all adapters.
	Merge
)

// Route says which adapters a flow may use. The zero value balances over all of them. Adapter pins
// the flow to one (with Failover as backups); Pool balances it over just the listed adapters.
type Route struct {
	// Meter, if set, counts the flow's bytes for the application that owns it.
	Meter   *AppMeter
	Pool    []string
	Adapter string
	// Fastest picks, for each new flow, the adapter that answers fastest: among Pool, or among all
	// internet adapters when Pool is empty.
	Fastest bool
	// DNS, if set, replaces the server the flow's DNS queries (port 53) are sent to.
	DNS []netip.Addr
	// Deny, if set, refuses the flow with this reason (a data cap that has been reached).
	Deny string
	// Failover are the adapters to try, in order, when Adapter cannot carry the flow. Only new flows
	// move: a flow that is already open stays where it is, and the next one returns to Adapter as
	// soon as it is healthy again.
	Failover []string
	OnDown   OnDown
}

func (r Route) chain() []string { return append([]string{r.Adapter}, r.Failover...) }

// UnavailableError says why a pinned adapter cannot take a flow right now.
type UnavailableError struct{ Adapter, Why string }

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("adapter %q unavailable: %s", e.Adapter, e.Why)
}

// RouteLog, if set, receives one line when a flow is moved to a failover adapter or refused because
// no allowed adapter can carry it. The same line is not repeated within a few seconds, so a busy
// app does not flood it.
var RouteLog func(format string, a ...any)

var routeLogged struct {
	sync.Mutex
	at map[string]time.Time
}

func routeNote(format string, a ...any) {
	if RouteLog == nil {
		return
	}
	line := fmt.Sprintf(format, a...)
	routeLogged.Lock()
	if routeLogged.at == nil {
		routeLogged.at = map[string]time.Time{}
	}
	now := time.Now()
	if now.Sub(routeLogged.at[line]) < 5*time.Second {
		routeLogged.Unlock()
		return
	}
	routeLogged.at[line] = now
	routeLogged.Unlock()
	RouteLog("%s\n", line)
}

// AcquireNamed takes a flow slot on the adapter called name (case-insensitive) if it can carry the
// family right now. Unlike AcquireFor it never substitutes another adapter. The caller must release it.
func (s *Scheduler) AcquireNamed(f Family, name string) (*Slot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sl *Slot
	for _, c := range s.slots {
		if c.present && strings.EqualFold(c.Name, name) {
			sl = c
			break
		}
	}
	fam := "IPv4"
	if f == Fam6 {
		fam = "IPv6"
	}
	switch {
	case sl == nil:
		return nil, &UnavailableError{name, "not connected"}
	case sl.ip(f) == nil:
		return nil, &UnavailableError{name, "no " + fam + " address"}
	case sl.state[f] == stateDown:
		return nil, &UnavailableError{name, "no internet"}
	case s.now().Before(sl.downUntil[f]):
		return nil, &UnavailableError{name, "link error, retrying shortly"}
	}
	sl.active.Add(1)
	sl.actF[f].Add(1)
	sl.total.Add(1)
	return sl, nil
}

// moved records that the flow is not on the rule's first-choice adapter.
func moved(rt Route, i int, why error) {
	if i > 0 {
		routeNote("failover: %v, using %q instead", why, rt.chain()[i])
	}
}

// poolAllow accepts the adapters named in pool that can carry family f right now. A pool member
// that is down is skipped, never used as a last resort the way plain balancing does: the rule
// names the only adapters the app may use. It runs with s.mu held.
func (s *Scheduler) poolAllow(f Family, pool []string) func(*Slot) bool {
	return func(sl *Slot) bool {
		if sl.state[f] == stateDown || s.now().Before(sl.downUntil[f]) {
			return false
		}
		for _, n := range pool {
			if strings.EqualFold(sl.Name, n) {
				return true
			}
		}
		return false
	}
}

// DialVia is DialPinned with a per-flow route. A zero route is DialPinned.
func (b *Balancer) DialVia(ctx context.Context, host string, port int, rt Route) (net.Conn, error) {
	c, err := b.dialVia(ctx, host, port, rt)
	if err != nil {
		return nil, err
	}
	return meterConn(c, rt.Meter), nil
}

func (b *Balancer) dialVia(ctx context.Context, host string, port int, rt Route) (net.Conn, error) {
	if rt.Deny != "" {
		return nil, &dialError{rep: repFailure, err: errors.New(rt.Deny)}
	}
	if rt.Adapter == "" && len(rt.Pool) == 0 && !rt.Fastest {
		return b.DialPinned(ctx, host, port)
	}
	f, network := Fam4, "tcp4"
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		f, network = Fam6, "tcp6"
	}
	if len(rt.Pool) > 0 || rt.Fastest {
		return b.dialPool(ctx, host, port, f, network, rt)
	}
	rep := byte(repNetUnreachable)
	var lastErr error
	for i, name := range rt.chain() {
		sl, err := b.S.AcquireNamed(f, name)
		if err != nil {
			lastErr, rep = err, repNetUnreachable
			continue
		}
		d := net.Dialer{
			LocalAddr: &net.TCPAddr{IP: sl.ip(f)},
			Control:   nics.ForceEgress(sl.Index, f == Fam6),
			Timeout:   b.ConnectTimeout,
			KeepAlive: 30 * time.Second,
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			moved(rt, i, lastErr)
			return &slotConn{TCPConn: conn.(*net.TCPConn), sl: sl, fam: f, s: b.S}, nil
		}
		b.S.ReleaseFor(sl, f)
		c := classify(err)
		if c.linkDown {
			b.S.Fail(sl, f, b.FailCooldown)
		}
		lastErr, rep = err, c.rep
		if !(c.linkDown || c.retry) || ctx.Err() != nil {
			return nil, &dialError{rep: rep, err: err} // e.g. refused by the server: another adapter will not help
		}
	}
	if rt.OnDown == Merge {
		routeNote("failover: %v, balancing across all adapters", lastErr)
		return b.DialPinned(ctx, host, port)
	}
	routeNote("blocked: %v, and the rule allows no other adapter", lastErr)
	return nil, &dialError{rep: rep, err: lastErr}
}

// dialPool balances a TCP flow over the adapters in rt.Pool only, the way DialTCP does over all of them.
func (b *Balancer) dialPool(ctx context.Context, host string, port int, f Family, network string, rt Route) (net.Conn, error) {
	pick := b.pickFor(f, rt)
	tried := map[string]struct{}{}
	rep := byte(repNetUnreachable)
	lastErr := fmt.Errorf("no adapter in [%s] can carry this flow", strings.Join(rt.Pool, ", "))
	for {
		sl := pick(tried)
		if sl == nil {
			break
		}
		tried[sl.key] = struct{}{}
		d := net.Dialer{
			LocalAddr: &net.TCPAddr{IP: sl.ip(f)},
			Control:   nics.ForceEgress(sl.Index, f == Fam6),
			Timeout:   b.ConnectTimeout,
			KeepAlive: 30 * time.Second,
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			return &slotConn{TCPConn: conn.(*net.TCPConn), sl: sl, fam: f, s: b.S}, nil
		}
		b.S.ReleaseFor(sl, f)
		c := classify(err)
		if c.linkDown {
			b.S.Fail(sl, f, b.FailCooldown)
		}
		lastErr, rep = err, c.rep
		if !(c.linkDown || c.retry) || ctx.Err() != nil {
			return nil, &dialError{rep: rep, err: err}
		}
	}
	if rt.OnDown == Merge {
		routeNote("failover: %v, balancing across all adapters", lastErr)
		return b.DialPinned(ctx, host, port)
	}
	routeNote("blocked: %v, and the rule allows no other adapter", lastErr)
	return nil, &dialError{rep: rep, err: lastErr}
}

// listenPool is dialPool for UDP.
func (b *Balancer) listenPool(ctx context.Context, f Family, network string, rt Route) (net.PacketConn, error) {
	pick := b.pickFor(f, rt)
	tried := map[string]struct{}{}
	lastErr := fmt.Errorf("no adapter in [%s] can carry this flow", strings.Join(rt.Pool, ", "))
	for {
		sl := pick(tried)
		if sl == nil {
			break
		}
		tried[sl.key] = struct{}{}
		lc := net.ListenConfig{Control: nics.ForceEgress(sl.Index, f == Fam6)}
		pc, err := lc.ListenPacket(ctx, network, net.JoinHostPort(sl.ip(f).String(), "0"))
		b.S.ReleaseFor(sl, f)
		if err != nil {
			if classify(err).linkDown {
				b.S.Fail(sl, f, b.FailCooldown)
			}
			lastErr = err
			continue
		}
		return &slotPacketConn{PacketConn: pc, sl: sl, fam: f}, nil
	}
	if rt.OnDown == Merge {
		routeNote("failover: %v, balancing across all adapters", lastErr)
		return b.ListenUDP(ctx, f)
	}
	routeNote("blocked: %v, and the rule allows no other adapter", lastErr)
	return nil, lastErr
}

// ListenUDPVia is ListenUDP with a per-flow route.
func (b *Balancer) ListenUDPVia(ctx context.Context, f Family, rt Route) (net.PacketConn, error) {
	pc, err := b.listenUDPVia(ctx, f, rt)
	if err != nil {
		return nil, err
	}
	return meterPacket(pc, rt.Meter), nil
}

func (b *Balancer) listenUDPVia(ctx context.Context, f Family, rt Route) (net.PacketConn, error) {
	if rt.Deny != "" {
		return nil, errors.New(rt.Deny)
	}
	if rt.Adapter == "" && len(rt.Pool) == 0 && !rt.Fastest {
		return b.ListenUDP(ctx, f)
	}
	network := "udp4"
	if f == Fam6 {
		network = "udp6"
	}
	if len(rt.Pool) > 0 || rt.Fastest {
		return b.listenPool(ctx, f, network, rt)
	}
	var lastErr error
	for i, name := range rt.chain() {
		sl, err := b.S.AcquireNamed(f, name)
		if err != nil {
			lastErr = err
			continue
		}
		lc := net.ListenConfig{Control: nics.ForceEgress(sl.Index, f == Fam6)}
		pc, err := lc.ListenPacket(ctx, network, net.JoinHostPort(sl.ip(f).String(), "0"))
		b.S.ReleaseFor(sl, f) // like ListenUDP, a UDP flow holds no balancing slot
		if err != nil {
			if classify(err).linkDown {
				b.S.Fail(sl, f, b.FailCooldown)
			}
			lastErr = err
			continue
		}
		moved(rt, i, lastErr)
		return &slotPacketConn{PacketConn: pc, sl: sl, fam: f}, nil
	}
	if rt.OnDown == Merge {
		routeNote("failover: %v, balancing across all adapters", lastErr)
		return b.ListenUDP(ctx, f)
	}
	routeNote("blocked: %v, and the rule allows no other adapter", lastErr)
	return nil, lastErr
}

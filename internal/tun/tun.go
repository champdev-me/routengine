// Package tun is the system-wide front-end: a virtual adapter whose routes capture every flow,
// terminated in a userland TCP/IP stack and re-dialed through the Balancer.
package tun

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/tun"
	"github.com/xjasonlyu/tun2socks/v2/core/option"
	tlog "github.com/xjasonlyu/tun2socks/v2/log"
	M "github.com/xjasonlyu/tun2socks/v2/metadata"
	"github.com/xjasonlyu/tun2socks/v2/proxy/reject"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"

	"routengine/internal/engine"
)

const (
	tunName = "RoutEngine"
	tunIP   = "198.18.0.1"
	tunMask = "255.255.255.0"
	// A ULA on purpose. The OS's source-address policy (RFC 6724, on Windows and in glibc) prefers
	// IPv4 over IPv6 when the only IPv6 source on the route is a ULA, so dual-stack sites go over
	// IPv4 and spread across every adapter, while IPv6-only sites (and explicit IPv6) still work
	// through the IPv6 path. A global-scope address here makes the OS prefer IPv6 everywhere, which
	// funnels all traffic onto whichever adapters happen to have IPv6. Nothing is ever sent from
	// this address.
	tunIP6 = "fd6e:6d65:7267::1"
)

// openName is what the OS is asked to call the adapter; macOS only accepts "utunN" and picks the number.
var openName = tunName

// staleHook, if set by a platform, also clears what a crashed run left outside the adapter.
var staleHook func()

// Two /1 routes per family beat any default route (/0) without touching it, so Off() is just
// deleting these.
var (
	routes4 = []string{"0.0.0.0/1", "128.0.0.0/1"}
	routes6 = []string{"::/1", "8000::/1"}
)

// tunProxy plugs the Balancer into tun2socks: every TCP/UDP flow that enters the TUN is dialed
// out through a pinned NIC. There is no SOCKS hop in between.
type tunProxy struct {
	b     *engine.Balancer
	route func(*M.Metadata) engine.Route // per-app routing; nil balances every flow
}

func (p tunProxy) routeFor(m *M.Metadata) engine.Route {
	if p.route == nil {
		return engine.Route{}
	}
	return p.route(m)
}

func (p tunProxy) DialContext(ctx context.Context, m *M.Metadata) (net.Conn, error) {
	rt := p.routeFor(m)
	host := m.DstIP.Unmap().String()
	if m.DstPort == 53 && len(rt.DNS) > 0 {
		host = rt.DNS[0].String() // this app's lookups go to the DNS server chosen for it
	}
	return p.b.DialVia(ctx, host, int(m.DstPort), rt)
}

func (p tunProxy) DialUDP(m *M.Metadata) (net.PacketConn, error) {
	rt := p.routeFor(m)
	dst := m.DstIP.Unmap()
	target := dst
	if m.DstPort == 53 && len(rt.DNS) > 0 {
		target = rt.DNS[0] // a DNS query: send it to the server chosen for this app
	}
	f := engine.Fam4
	if target.Is6() {
		f = engine.Fam6
	}
	pc, err := p.b.ListenUDPVia(context.Background(), f, rt)
	if err != nil {
		return nil, err
	}
	if target != dst {
		return engine.NewDNSRedirect(pc, dst, target), nil
	}
	return pc, nil
}

// Tun is the system-wide switch: On creates the adapter and routes, Off removes them. Both are
// idempotent. If the process dies, the adapter and its routes die with it.
type Tun struct {
	mu  sync.Mutex
	B   *engine.Balancer // set before On
	MTU int              // set before On; see DefaultMTU
	// Route, if set before On, picks the adapter for each new flow (per-app routing). A zero Route balances.
	Route func(*M.Metadata) engine.Route
	dev   device.Device
	stk   *stack.Stack
	idx   int
	on    bool
}

// DefaultMTU is the TUN's MTU, used both by the userland stack and by the OS. TUN traffic never
// leaves the machine (flows are re-dialed with normal sockets), so it isn't bound by the links'
// 1500. Bigger packets mean far fewer per-packet trips through the userland stack: about 6x fewer
// for bulk TCP at 9000. Not 65535: the TUN library allocates a full-MTU buffer for every inbound
// packet, even a 40-byte ACK.
const DefaultMTU = 9000

// udpSessionTimeout ends idle UDP sessions sooner than tun2socks' 60s. Torrent DHT opens a session
// per peer queried, each holding a socket, two goroutines and two 64 KB buffers. 30s stays above the
// ~25s keepalives common in VPN and NAT-traversal protocols.
const udpSessionTimeout = 30 * time.Second

func New(b *engine.Balancer) *Tun { return &Tun{B: b, MTU: DefaultMTU} }

func (t *Tun) IsOn() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.on
}

// keepDevice makes Off pause instead of tear down: only the capture routes go, the adapter stays.
// macOS sets it, because there taking a utun away or adding one makes the system drop other
// virtual links such as a TetherKit phone, so pausing and resuming must never touch the adapter.
var keepDevice bool

// open creates the adapter and the userland stack, without capturing anything yet.
func (t *Tun) open() error {
	// "error", not "warn": tun2socks warns once per dropped UDP packet ("symmetric NAT ... drop
	// packet"), which a torrent client triggers continuously.
	if lvl, e := tlog.ParseLevel("error"); e == nil {
		tlog.SetLogger(tlog.Must(tlog.NewLeveled(lvl)))
	}
	tunnel.T().SetUDPTimeout(udpSessionTimeout)
	CleanupStale()

	dev, err := tun.Open(openName, uint32(t.MTU))
	if err != nil {
		return fmt.Errorf("open TUN (%s): %w", openHint, err)
	}
	stk, err := core.CreateStack(&core.Config{
		LinkEndpoint:     dev,
		TransportHandler: tunnel.T(),
		Options:          []option.Option{option.WithTCPModerateReceiveBuffer(true)},
	})
	if err != nil {
		dev.Close()
		return err
	}
	installForwarders(stk, t.B.V6Allowed)
	t.dev, t.stk = dev, stk
	return nil
}

// Warm creates the adapter ahead of time, so a later On only adds routes. Only platforms with
// keepDevice need it; elsewhere it does nothing.
func (t *Tun) Warm() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !keepDevice || t.dev != nil {
		return nil
	}
	return t.open()
}

func (t *Tun) On() (err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.on {
		return nil
	}
	if t.dev == nil {
		if err = t.open(); err != nil {
			return err
		}
	}
	defer func() {
		if err != nil {
			t.teardown()
		}
	}()
	tunnel.T().SetProxy(tunProxy{t.B, t.Route})

	if t.idx, err = configure(t.dev.Name(), t.MTU); err != nil {
		return err
	}
	t.on = true
	return nil
}

// Off stops capturing: with keepDevice it only removes the routes, otherwise it removes the adapter.
func (t *Tun) Off() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if keepDevice && t.dev != nil {
		if t.idx != 0 {
			delRoutes(t.idx) // traffic falls straight back to the normal default route
		}
		tunnel.T().SetProxy(&reject.Reject{})
		t.on = false
		return
	}
	t.teardown()
}

// Close removes everything, including a kept adapter. Call it when the program exits.
func (t *Tun) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.teardown()
}

func (t *Tun) teardown() {
	if t.idx != 0 {
		delRoutes(t.idx) // traffic falls straight back to the normal default route
	}
	tunnel.T().SetProxy(&reject.Reject{})
	if t.dev != nil {
		t.dev.Close()
	}
	if t.stk != nil {
		done := make(chan struct{})
		go func() { t.stk.Close(); t.stk.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
	t.dev, t.stk, t.idx, t.on = nil, nil, 0, false
}

// CleanupStale removes routes left on a surviving RoutEngine adapter (e.g. after a crash).
func CleanupStale() {
	if staleHook != nil {
		staleHook()
	}
	if ifc, err := net.InterfaceByName(tunName); err == nil {
		delRoutes(ifc.Index)
	}
}

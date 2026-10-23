package tun

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"routengine/internal/logx"
)

const openHint = "needs root or CAP_NET_ADMIN, and /dev/net/tun"

// configure gives the new adapter its addresses and routes, and returns its index. tun2socks has
// already set the MTU. Everything goes through netlink rather than `ip`, so it also works when the
// privilege is a file capability, which child processes don't inherit.
func configure(name string, _ int) (int, error) {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return 0, fmt.Errorf("adapter %q did not appear: %w", name, err)
	}
	idx := link.Attrs().Index
	if err := netlink.AddrReplace(link, &netlink.Addr{IPNet: hostCIDR(tunIP + "/24")}); err != nil {
		return idx, fmt.Errorf("set address: %w", err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return idx, fmt.Errorf("bring up: %w", err)
	}
	for _, dst := range routes4 {
		if err := netlink.RouteReplace(tunRoute(idx, dst)); err != nil {
			return idx, fmt.Errorf("route %s: %w", dst, err)
		}
	}
	// IPv6 is best-effort: if it can't be set up (IPv6 disabled), IPv4 still works and IPv6 stays on
	// its normal path. No DAD: the address is ours alone, and it should be usable immediately.
	if err := netlink.AddrReplace(link, &netlink.Addr{IPNet: hostCIDR(tunIP6 + "/64"), Flags: unix.IFA_F_NODAD}); err != nil {
		logx.Printf("warning: IPv6 not captured: %v\n", err)
	} else {
		for _, dst := range routes6 {
			if err := netlink.RouteReplace(tunRoute(idx, dst)); err != nil {
				logx.Printf("warning: IPv6 route %s: %v\n", dst, err)
			}
		}
	}
	return idx, nil
}

// delRoutes removes the capturing routes. Closing the TUN also removes the adapter and everything
// on it, but deleting the routes first stops capture before the stack goes away.
func delRoutes(idx int) {
	for _, dst := range append(append([]string{}, routes4...), routes6...) {
		netlink.RouteDel(tunRoute(idx, dst)) // already gone is fine: IPv6 may never have been set up
	}
}

func tunRoute(idx int, dst string) *netlink.Route {
	return &netlink.Route{LinkIndex: idx, Dst: prefix(dst), Scope: netlink.SCOPE_LINK}
}

// hostCIDR is an address with its subnet, e.g. 198.18.0.1/24.
func hostCIDR(s string) *net.IPNet {
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	n.IP = ip
	return n
}

func prefix(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

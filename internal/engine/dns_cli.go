//go:build nogui

package engine

import (
	"net"
	"net/netip"
)

// NewDNSRedirect would send an app's DNS queries to its own servers, a Pro feature of the window apps. The command
// line never asks for it; if it did, the queries would go where the app sent them.
func NewDNSRedirect(pc net.PacketConn, _, _ netip.Addr) net.PacketConn { return pc }

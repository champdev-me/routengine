package engine

import (
	"net"
	"net/netip"
)

// dnsRedirect is a UDP socket whose DNS queries go to a different server than the app asked for. The
// app is shown the answers as if the server it chose had sent them, which is what its own stack
// expects, so nothing on the app's side knows.
type dnsRedirect struct {
	net.PacketConn
	orig, to *net.UDPAddr
}

// NewDNSRedirect sends everything written to pc to the DNS server `to` (port 53) instead of the one the
// app addressed, `orig`, and reports replies as coming from `orig`.
func NewDNSRedirect(pc net.PacketConn, orig, to netip.Addr) net.PacketConn {
	return &dnsRedirect{
		PacketConn: pc,
		orig:       &net.UDPAddr{IP: orig.AsSlice(), Port: 53},
		to:         &net.UDPAddr{IP: to.AsSlice(), Port: 53},
	}
}

func (d *dnsRedirect) WriteTo(p []byte, _ net.Addr) (int, error) {
	return d.PacketConn.WriteTo(p, d.to)
}

func (d *dnsRedirect) ReadFrom(p []byte) (int, net.Addr, error) {
	n, from, err := d.PacketConn.ReadFrom(p)
	if ua, ok := from.(*net.UDPAddr); ok && ua.Port == d.to.Port && ua.IP.Equal(d.to.IP) {
		from = d.orig
	}
	return n, from, err
}

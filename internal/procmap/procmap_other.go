//go:build !windows && !linux && !darwin

package procmap

import "net/netip"

// Supported reports whether this platform can map a flow to its process.
const Supported = false

// Lookup is not implemented on this platform yet: no flow is ever attributed to a process.
func Lookup(tcp bool, src netip.AddrPort) (Proc, bool) { return Proc{}, false }

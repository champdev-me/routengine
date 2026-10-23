package nics

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// ForceEgress binds a socket to an interface, so our own outbound flows can never be captured by
// the TUN's routes (no routing loop): the kernel only considers routes scoped to that interface.
func ForceEgress(idx int, v6 bool) func(network, address string, c syscall.RawConn) error {
	if idx <= 0 {
		return nil
	}
	level, opt := unix.IPPROTO_IP, unix.IP_BOUND_IF
	if v6 {
		level, opt = unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF
	}
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptInt(int(fd), level, opt, idx)
		}); err != nil {
			return err
		}
		return serr
	}
}

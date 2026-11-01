package nics

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// ForceEgress binds a socket to an interface, so our own outbound flows can never be captured by
// the TUN's routes (no routing loop): the kernel only considers routes through the bound device.
// Unprivileged since Linux 5.7; older kernels need CAP_NET_RAW. SO_BINDTOIFINDEX (5.0+) avoids
// a name lookup per socket, with SO_BINDTODEVICE as the fallback for older kernels.
func ForceEgress(idx int, v6 bool) func(network, address string, c syscall.RawConn) error {
	if idx <= 0 {
		return nil
	}
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BINDTOIFINDEX, idx)
			if serr == unix.ENOPROTOOPT {
				var ifc *net.Interface
				if ifc, serr = net.InterfaceByIndex(idx); serr == nil {
					serr = unix.BindToDevice(int(fd), ifc.Name)
				}
			}
		}); err != nil {
			return err
		}
		return serr
	}
}

package nics

import (
	"encoding/binary"
	"syscall"

	"golang.org/x/sys/unix"
)

// if_msghdr2 on darwin: 32 bytes of header, then if_data64, whose ibytes/obytes sit at +64/+72.
// The 64-bit counters matter: if_data's 32-bit ones wrap every 4 GB.
const (
	rtmIfInfo2 = 0x12
	offIBytes  = 32 + 64
	offOBytes  = 32 + 72
)

// Octets reads the kernel's byte counters for an adapter. They cover every app's traffic, not
// just what RoutEngine relays: apps that bind to each adapter directly bypass the TUN but still
// show up here.
func Octets(idx int) (in, out uint64, ok bool) {
	if idx <= 0 {
		return 0, 0, false
	}
	rib, err := syscall.RouteRIB(unix.NET_RT_IFLIST2, idx)
	if err != nil {
		return 0, 0, false
	}
	for len(rib) >= 4 {
		n := int(binary.LittleEndian.Uint16(rib))
		if n < 4 || n > len(rib) {
			return 0, 0, false
		}
		if rib[3] == rtmIfInfo2 && n >= offOBytes+8 && int(binary.LittleEndian.Uint16(rib[12:])) == idx {
			return binary.LittleEndian.Uint64(rib[offIBytes:]), binary.LittleEndian.Uint64(rib[offOBytes:]), true
		}
		rib = rib[n:]
	}
	return 0, 0, false
}

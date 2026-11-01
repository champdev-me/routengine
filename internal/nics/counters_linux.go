package nics

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Octets reads the kernel's byte counters for an adapter. They cover every app's traffic, not
// just what RoutEngine relays: apps like qBittorrent bind to each adapter directly and bypass the
// TUN, but still show up here.
func Octets(idx int) (in, out uint64, ok bool) {
	if idx <= 0 {
		return 0, 0, false
	}
	ifc, err := net.InterfaceByIndex(idx)
	if err != nil {
		return 0, 0, false
	}
	stats := filepath.Join("/sys/class/net", ifc.Name, "statistics")
	in, err1 := readUint(filepath.Join(stats, "rx_bytes"))
	out, err2 := readUint(filepath.Join(stats, "tx_bytes"))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return in, out, true
}

func readUint(path string) (uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}

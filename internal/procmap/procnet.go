package procmap

import (
	"bufio"
	"encoding/hex"
	"io"
	"net/netip"
	"strconv"
	"strings"
)

// findInode reads one /proc/net/{tcp,udp}{,6} table and returns the inode of the socket whose local end is
// src. A UDP socket that sent without binding shows the wildcard address, so wildcard allows that match;
// an exact address always wins over it.
func findInode(r io.Reader, src netip.AddrPort, wildcard bool) (uint64, bool) {
	var best uint64
	sc := bufio.NewScanner(r)
	sc.Scan() // header
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 {
			continue
		}
		local, ok := parseHexAddr(f[1])
		if !ok || local.Port() != src.Port() {
			continue
		}
		inode, err := strconv.ParseUint(f[9], 10, 64)
		if err != nil || inode == 0 {
			continue
		}
		if local.Addr() == src.Addr().Unmap() {
			return inode, true
		}
		if wildcard && local.Addr().IsUnspecified() {
			best = inode
		}
	}
	return best, best != 0
}

// parseHexAddr reads "0100007F:1F90" or the 32-digit IPv6 form. The kernel prints each 32-bit word in the
// CPU's byte order, which is little-endian on every machine this runs on.
func parseHexAddr(s string) (netip.AddrPort, bool) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return netip.AddrPort{}, false
	}
	port, err := strconv.ParseUint(s[i+1:], 16, 16)
	b, err2 := hex.DecodeString(s[:i])
	if err != nil || err2 != nil || (len(b) != 4 && len(b) != 16) {
		return netip.AddrPort{}, false
	}
	for g := 0; g < len(b); g += 4 {
		b[g], b[g+1], b[g+2], b[g+3] = b[g+3], b[g+2], b[g+1], b[g]
	}
	a, _ := netip.AddrFromSlice(b)
	return netip.AddrPortFrom(a.Unmap(), uint16(port)), true
}

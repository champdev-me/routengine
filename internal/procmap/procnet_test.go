package procmap

import (
	"net/netip"
	"strings"
	"testing"
)

const tcpTable = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 11111 1 0000 100 0 0 10 0
   1: 0100007F:D431 0100007F:1F90 01 00000000:00000000 00:00000000 00000000  1000        0 22222 1 0000 20 4 30 10 -1
   2: 0100007F:D432 0100007F:1F90 06 00000000:00000000 03:00000000 00000000     0        0 0 3 0000 0
`

func TestFindInodeV4(t *testing.T) {
	got, ok := findInode(strings.NewReader(tcpTable), netip.MustParseAddrPort("127.0.0.1:54321"), false)
	if !ok || got != 22222 {
		t.Fatalf("got %d %v, want 22222", got, ok)
	}
	if _, ok := findInode(strings.NewReader(tcpTable), netip.MustParseAddrPort("127.0.0.1:54322"), false); ok {
		t.Fatal("a TIME_WAIT row (inode 0) has no owner")
	}
}

func TestFindInodeV6AndWildcard(t *testing.T) {
	const udp6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
   0: 00000000000000000000000000000000:1F90 00000000000000000000000001000000:0000 07 00000000:00000000 00:00000000 00000000  1000        0 33333 2 0000 0
   1: 00000000000000000000000001000000:1F91 00000000000000000000000000000000:0000 07 00000000:00000000 00:00000000 00000000  1000        0 44444 2 0000 0
`
	if got, ok := findInode(strings.NewReader(udp6), netip.MustParseAddrPort("[::1]:8081"), true); !ok || got != 44444 {
		t.Fatalf("exact ::1: got %d %v", got, ok)
	}
	if got, ok := findInode(strings.NewReader(udp6), netip.MustParseAddrPort("198.18.0.1:8080"), true); !ok || got != 33333 {
		t.Fatalf("wildcard: got %d %v", got, ok)
	}
	if _, ok := findInode(strings.NewReader(udp6), netip.MustParseAddrPort("198.18.0.1:8080"), false); ok {
		t.Fatal("TCP must not match a wildcard row")
	}
}

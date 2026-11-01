//go:build linux || darwin

package procmap

import (
	"net"
	"net/netip"
	"os"
	"testing"
)

// A socket this test process opens must be attributed to this process, with this program's path.
func TestLookupFindsOwnSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			defer c.Close()
			c.Read(make([]byte, 1))
		}
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p, ok := Lookup(true, c.LocalAddr().(*net.TCPAddr).AddrPort())
	if !ok || p.PID != uint32(os.Getpid()) {
		t.Fatalf("got %+v %v, want pid %d", p, ok, os.Getpid())
	}
	if exe, _ := os.Executable(); p.Exe == "" || (os.Getenv("CI") == "" && p.Exe != exe) {
		t.Logf("exe %q (test binary %q)", p.Exe, exe)
	}

	u, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if p, ok := Lookup(false, u.LocalAddr().(*net.UDPAddr).AddrPort()); !ok || p.PID != uint32(os.Getpid()) {
		t.Fatalf("udp: got %+v %v", p, ok)
	}
	if _, ok := Lookup(true, netip.MustParseAddrPort("127.0.0.1:1")); ok {
		t.Fatal("nothing owns port 1")
	}
}

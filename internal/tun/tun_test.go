package tun

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
)

func TestPickNetSkipsAdaptersOnTheRange(t *testing.T) {
	p := func(s string) netip.Prefix { return netip.MustParsePrefix(s) }
	for _, c := range []struct {
		name string
		used []netip.Prefix
		want string
	}{
		{"nothing else there", nil, "198.18.0.1/24"},
		{"real LANs only", []netip.Prefix{p("192.168.1.12/24"), p("10.0.0.5/8"), p("127.0.0.1/8")}, "198.18.0.1/24"},
		{"Clash Meta on a /30", []netip.Prefix{p("198.18.0.1/30")}, "198.18.1.1/24"},
		{"another app on a /16", []netip.Prefix{p("198.18.0.1/16")}, "198.19.0.1/24"},
		{"two /24s taken", []netip.Prefix{p("198.18.0.7/24"), p("198.18.1.1/24")}, "198.18.2.1/24"},
		// The whole range is someone's subnet: sit inside it, but never on the address it holds.
		{"whole /15 taken", []netip.Prefix{p("198.18.0.1/15")}, "198.18.1.1/24"},
	} {
		if got := pickNet(c.used); got.String() != c.want {
			t.Errorf("%s: pickNet = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestUsedNetsReadsSubnets(t *testing.T) {
	if ifs, _ := net.Interfaces(); len(ifs) == 0 {
		t.Skip("the OS lists no adapters (qemu-user on s390x can't translate netlink)")
	}
	used := usedNets("")
	for _, u := range used {
		if u.String() == "127.0.0.1/8" {
			return
		}
	}
	t.Errorf("loopback 127.0.0.1/8 not among %v", used)
}

func TestCmdErrorPutsTheReasonFirst(t *testing.T) {
	err := cmdError("netsh", strings.Fields("interface ipv4 set address name=13 source=static address=198.18.0.1"),
		errors.New("exit status 1"), []byte("\r\nThe object already exists.\r\n\r\n"))
	want := "The object already exists. (netsh interface ipv4 set address name=13 source=static address=198.18.0.1: exit status 1)"
	if err.Error() != want {
		t.Errorf("got  %q\nwant %q", err, want)
	}
	if err := cmdError("route", []string{"add"}, errors.New("exit status 1"), nil); err.Error() != "route add: exit status 1" {
		t.Errorf("no output: got %q", err)
	}
}

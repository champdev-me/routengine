package nics

import (
	"net"
	"testing"
)

func TestLinkCountersReadRealAdapter(t *testing.T) {
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if _, _, ok := Octets(ifc.Index); !ok {
			t.Fatalf("could not read OS counters for %s (index %d)", ifc.Name, ifc.Index)
		}
		return
	}
	t.Skip("no up adapter")
}

func TestAdaptersAreSortedIntoKinds(t *testing.T) {
	for _, c := range []struct {
		name, desc, virtual string
		gateway             bool
		kind                Kind
		skipped             bool
	}{
		{"WiFi", "RZ616 Wi-Fi 6E 160MHz", "", true, KindInternet, false},
		{"Ethernet 4", "Apple Mobile Device Ethernet", "", true, KindInternet, false}, // a phone tether
		{"Ethernet", "Realtek Gaming 2.5GbE", "", false, KindLAN, false},              // a cable to a NAS: no internet route
		{"Tailscale", "Tailscale Tunnel", "tunnel/virtual interface type 131", true, KindVirtual, false},
		{"wg0", "WireGuard Tunnel", "tunnel/virtual interface type 131", false, KindVirtual, false},
		{"MyOffice", "Wintun Userspace Tunnel", "tunnel/virtual interface type 53", false, KindVirtual, false},
		{"Work VPN", "Cisco AnyConnect Secure Mobility Client Virtual Miniport Adapter", "", true, KindInternet, true}, // a miniport: skipped
		{"ProtonVPN", "ProtonVPN TAP-Windows Adapter V9", "", true, KindVirtual, false},
		{"RoutEngine", "Wintun Userspace Tunnel", "tunnel/virtual interface type 53", true, KindInternet, true}, // our own adapter
		{"vEthernet (WSL)", "Hyper-V Virtual Ethernet Adapter", "", true, KindInternet, true},
		{"Bluetooth Network Connection", "Bluetooth Device (Personal Area Network)", "", false, KindInternet, true},
		{"Ethernet 2", "Kaspersky Security Data Escort Adapter", "tunnel/virtual interface type 53", false, KindInternet, true}, // virtual, not a VPN product
	} {
		kind, skip := classify(c.name, c.desc, c.virtual, c.gateway)
		if (skip != "") != c.skipped || (!c.skipped && kind != c.kind) {
			t.Errorf("%s (%s): got kind %v skip %q, want kind %v skipped=%v", c.name, c.desc, kind, skip, c.kind, c.skipped)
		}
	}
}

func TestKindNames(t *testing.T) {
	for k, want := range map[Kind]string{KindInternet: "internet", KindLAN: "local network", KindVirtual: "virtual/VPN"} {
		if k.String() != want {
			t.Errorf("%d: %q", k, k.String())
		}
	}
}

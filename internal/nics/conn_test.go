package nics

import "testing"

func TestConnOf(t *testing.T) {
	for _, c := range []struct {
		name, desc string
		ifType     uint32
		kind       Kind
		want       Conn
	}{
		{"Ethernet", "Realtek PCIe GbE Family Controller", 6, KindInternet, ConnEthernet},
		{"Ethernet 2", "Realtek USB GbE Family Controller", 6, KindInternet, ConnEthernet}, // a USB dongle is wired
		{"WiFi", "Intel(R) Wi-Fi 6 AX201 160MHz", 71, KindInternet, ConnWiFi},
		{"WiFi", "", 71, KindInternet, ConnWiFi}, // the interface type alone is enough
		{"Wi-Fi", "Qualcomm Atheros QCA9377 Wireless Network Adapter", 6, KindInternet, ConnWiFi},
		{"Ethernet 4", "Apple Mobile Device Ethernet", 6, KindInternet, ConnTether}, // an iPhone over USB
		{"Ethernet 5", "Remote NDIS based Internet Sharing Device", 6, KindInternet, ConnTether},
		{"Ethernet 6", "Samsung Mobile USB Remote NDIS Network Device", 6, KindInternet, ConnTether},
		{"Cellular", "Huawei Mobile Broadband", 243, KindInternet, ConnTether},
		{"Tailscale", "Tailscale Tunnel", 53, KindVirtual, ConnVirtual},
		{"Ethernet 7", "Wintun Userspace Tunnel", 6, KindVirtual, ConnVirtual}, // a VPN that reports as Ethernet
		{"wlan0", "iwlwifi", 0, KindInternet, ConnWiFi},
		{"enp3s0", "r8169", 0, KindInternet, ConnEthernet},
		{"usb0", "rndis_host", 0, KindInternet, ConnTether},
		{"en0", "iPhone USB", 0, KindInternet, ConnTether},
	} {
		if got := connOf(c.name, c.desc, c.ifType, c.kind); got != c.want {
			t.Errorf("%s / %s: %v, want %v", c.name, c.desc, got, c.want)
		}
	}
}

func TestPreferredDefaultFollowsTheOrder(t *testing.T) {
	eth := Candidate{"Ethernet", ConnEthernet, KindInternet, true}
	wifi := Candidate{"WiFi", ConnWiFi, KindInternet, true}
	tether := Candidate{"Ethernet 4", ConnTether, KindInternet, true}
	vpn := Candidate{"Tailscale", ConnVirtual, KindVirtual, true}
	down := func(c Candidate) Candidate { c.Up = false; return c }

	for _, c := range []struct {
		name string
		in   []Candidate
		want string
	}{
		{"wired beats everything", []Candidate{vpn, tether, wifi, eth}, "Ethernet"},
		{"then Wi-Fi", []Candidate{vpn, tether, wifi}, "WiFi"},
		{"then the phone", []Candidate{vpn, tether}, "Ethernet 4"},
		{"a VPN only when nothing else is left", []Candidate{vpn}, "Tailscale"},
		{"a wired link that is down is passed over", []Candidate{down(eth), wifi, tether}, "WiFi"},
		{"everything down", []Candidate{down(eth), down(wifi)}, ""},
		{"nothing at all", nil, ""},
		{"a local-network-only cable is never the default", []Candidate{{"NAS cable", ConnEthernet, KindLAN, true}, wifi}, "WiFi"},
		{"two of a kind: the first listed", []Candidate{{"Ethernet 2", ConnEthernet, KindInternet, true}, eth}, "Ethernet 2"},
	} {
		if got := PreferredDefault(c.in); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseRoundTrips(t *testing.T) {
	for c := ConnEthernet; c <= ConnVirtual; c++ {
		if ParseConn(c.String()) != c {
			t.Errorf("conn %v", c)
		}
	}
	for _, k := range []Kind{KindInternet, KindLAN, KindVirtual} {
		if ParseKind(k.String()) != k {
			t.Errorf("kind %v", k)
		}
	}
	if ParseConn("") != ConnEthernet || ParseKind("") != KindInternet {
		t.Error("anything unknown must read as the plain kind")
	}
}

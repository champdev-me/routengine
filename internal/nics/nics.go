package nics

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Kind says what an adapter is for.
type Kind uint8

const (
	// KindInternet is real hardware with a default gateway: Wi-Fi, Ethernet, a phone tether. Plain
	// balancing spreads connections over these.
	KindInternet Kind = iota
	// KindLAN is real hardware with no default gateway: a local network only (a direct cable to a NAS
	// or a camera, say). It can be chosen for an app but is never balanced over automatically.
	KindLAN
	// KindVirtual is a VPN or overlay such as Tailscale, WireGuard or OpenVPN. It can be chosen for an
	// app (send only this app through the VPN) but is never balanced over automatically, because a VPN
	// would quietly receive traffic that was never meant to go through it.
	KindVirtual
)

func (k Kind) String() string {
	switch k {
	case KindLAN:
		return "local network"
	case KindVirtual:
		return "virtual/VPN"
	}
	return "internet"
}

// Nic is one adapter with the source addresses flows are pinned to: its IPv4 address and/or its
// global IPv6 address (either may be nil).
type Nic struct {
	Name   string
	Index  int // interface index, used to force egress (IP_UNICAST_IF); 0 = don't force
	IP     net.IP
	IP6    net.IP
	Weight float64
	Kind   Kind
	Conn   Conn   // how it connects: sets how much it is preferred as the default adapter
	Desc   string // the driver's description, e.g. "Tailscale Tunnel"
}

// Skip is an adapter that was seen but not used, with the reason (shown by `routengine list`).
type Skip struct{ Name, Reason string }

var (
	// neverUsable are interfaces nobody routes an app through: our own adapter, loopback, VM and
	// container plumbing, transition tunnels, Bluetooth and Wi-Fi Direct. The anchored names are
	// Linux interface naming conventions.
	neverUsable = regexp.MustCompile(`(?i)^routengine$|loopback|vethernet|\bwsl\b|hyper-v|docker|podman|vmware|virtualbox|vboxnet|npcap|teredo|isatap|6to4|pseudo|bluetooth|wi-?fi direct|kernel debug|miniport|^veth|^virbr|^br-|^vmnet|^vnet\d|^lxc|^lxdbr|^cni|^flannel`)

	// vpnLike are the VPN and overlay products people pick an app's route through.
	vpnLike = regexp.MustCompile(`(?i)tailscale|zerotier|wireguard|openvpn|\bvpn\b|\btap\b|tap-|wintun|anyconnect|fortinet|forticlient|globalprotect|nordlynx|nordvpn|proton|mullvad|expressvpn|surfshark|pritunl|softether|^wg\d|^tun\d|^tap\d|^zt`)

	// generic markers of a virtual adapter that reports as plain Ethernet.
	virtualName = regexp.MustCompile(`(?i)tailscale|zerotier|wintun|^wg\d|^tun\d|^tap\d|^zt`)
	virtualDesc = regexp.MustCompile(`(?i)virtual|vpn|\btap\b|tap-|tunnel|wintun|wireguard|tailscale|zerotier|openvpn|anyconnect|fortinet`)
)

// classify decides what an adapter is. virtualReason is the operating system's reason it is not real
// hardware ("" if it is), and gateway says whether it has a default route. A non-empty skip means the
// adapter is not offered at all.
func classify(name, desc, virtualReason string, gateway bool) (kind Kind, skip string) {
	if neverUsable.MatchString(name) || neverUsable.MatchString(desc) {
		return KindInternet, fmt.Sprintf("not a network apps connect through (%s)", firstNonEmpty(desc, name))
	}
	if virtualReason != "" || virtualName.MatchString(name) || virtualDesc.MatchString(desc) {
		if vpnLike.MatchString(name) || vpnLike.MatchString(desc) {
			return KindVirtual, ""
		}
		return KindInternet, fmt.Sprintf("virtual interface (%s)", firstNonEmpty(desc, name))
	}
	if !gateway {
		return KindLAN, ""
	}
	return KindInternet, ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func DiscoverNics(include []string, weights map[string]float64) ([]Nic, error) {
	nics, _, err := DiscoverNicsDetailed(include, weights)
	return nics, err
}

func DiscoverNicsDetailed(include []string, weights map[string]float64) ([]Nic, []Skip, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, err
	}
	// Auto mode offers real hardware and VPN/overlay adapters. Which of them plain balancing uses is
	// decided by Kind, not here. An explicit -include list overrides these checks.
	var info map[int]adapterInfo
	if len(include) == 0 {
		info, _ = adapterInfos() // nil on error: fall back to name filtering only
	}
	var out []Nic
	var skipped []Skip
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		skip := func(reason string) { skipped = append(skipped, Skip{ifc.Name, reason}) }
		ai, haveInfo := info[ifc.Index]
		gateway := info == nil || !haveInfo || ai.v4 || ai.v6
		kind := KindInternet
		if len(include) > 0 {
			if !containsFold(include, ifc.Name) {
				continue
			}
		} else {
			var why string
			kind, why = classify(ifc.Name, ai.desc, ai.virtual, gateway)
			if why != "" {
				skip(why)
				continue
			}
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		var ip4, ip6 net.IP
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if v4 := ipn.IP.To4(); v4 != nil {
				if ip4 == nil && !v4.IsLinkLocalUnicast() { // 169.254/16: no DHCP lease, no internet
					ip4 = v4
				}
			} else if ip6 == nil && ipn.IP.IsGlobalUnicast() && !ipn.IP.IsPrivate() { // skips fe80:: and ULA
				ip6 = ipn.IP
			}
		}
		if info != nil && kind == KindInternet { // only an internet adapter is held to its gateways
			if !ai.v4 {
				ip4 = nil
			}
			if !ai.v6 {
				ip6 = nil
			}
		}
		if ip4 == nil && ip6 == nil {
			if kind == KindInternet && info != nil && !ai.v4 && !ai.v6 {
				skip("no default gateway (no internet route)")
			} else {
				skip("no usable IP address")
			}
			continue
		}
		w := 1.0
		if v, ok := weights[ifc.Name]; ok {
			w = v
		}
		out = append(out, Nic{Name: ifc.Name, Index: ifc.Index, IP: ip4, IP6: ip6, Weight: w, Kind: kind, Conn: connOf(ifc.Name, ai.desc, ai.ifType, kind), Desc: ai.desc})
	}
	return out, skipped, nil
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

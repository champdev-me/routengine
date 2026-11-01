package nics

import (
	"regexp"
	"strings"
)

// Conn is how an adapter connects. Its order is the order the default adapter is chosen in when the user
// has not picked one: a wired network card, then Wi-Fi, then a phone's tethering (USB or mobile
// broadband), then virtual adapters such as a VPN.
type Conn uint8

const (
	ConnEthernet Conn = iota
	ConnWiFi
	ConnTether
	ConnVirtual
)

func (c Conn) String() string {
	return [...]string{"ethernet", "wifi", "tether", "virtual"}[c]
}

var (
	// tetherLike are the drivers and names of a phone sharing its connection over a cable (an iPhone,
	// Android's RNDIS) and of mobile-broadband modems. A plain USB Ethernet dongle is not one of these.
	tetherLike = regexp.MustCompile(`(?i)apple mobile device|iphone|ipad|\brndis\b|remote ndis|rndis_host|ipheth|android|tether|mobile broadband|\bwwan\b|^ww[a-z0-9]+$|^usb\d+$|\blte\b`)

	// wifiLike are the names and drivers of wireless cards.
	wifiLike = regexp.MustCompile(`(?i)wi-?fi|wireless|802\.11|\bwlan\b|^wl[a-z0-9]+$|iwlwifi|brcmfmac|ath\d+k?|rtw\d*`)
)

// connOf says how an adapter connects. ifType is the IANA interface type where the system gives one
// (Windows: 71 is Wi-Fi, 243 and 244 are mobile broadband), 0 where it does not.
func connOf(name, desc string, ifType uint32, kind Kind) Conn {
	switch {
	case kind == KindVirtual:
		return ConnVirtual
	case ifType == 71:
		return ConnWiFi
	case ifType == 243 || ifType == 244:
		return ConnTether
	case tetherLike.MatchString(name) || tetherLike.MatchString(desc):
		return ConnTether
	case wifiLike.MatchString(name) || wifiLike.MatchString(desc):
		return ConnWiFi
	}
	return ConnEthernet
}

// Candidate is an adapter the default can be chosen among.
type Candidate struct {
	Name string
	Conn Conn
	Kind Kind
	Up   bool // present and not known to be without internet
}

// PreferredDefault is the adapter that serves as the default when the user has not chosen one: the best
// connection type that is up, in the order of Conn. An adapter that only reaches a local network is never
// chosen, since a default has to carry internet traffic, and an adapter that is down is passed over. Of
// two adapters of the same type the first listed wins. It returns "" when nothing qualifies.
func PreferredDefault(c []Candidate) string {
	best, bestRank := "", Conn(255)
	for _, a := range c {
		if !a.Up || a.Kind == KindLAN || strings.TrimSpace(a.Name) == "" {
			continue
		}
		if a.Conn < bestRank {
			best, bestRank = a.Name, a.Conn
		}
	}
	return best
}

// ParseConn reads back what Conn.String wrote; anything unknown is plain Ethernet.
func ParseConn(s string) Conn {
	for c := ConnEthernet; c <= ConnVirtual; c++ {
		if c.String() == s {
			return c
		}
	}
	return ConnEthernet
}

// ParseKind reads back what Kind.String wrote; anything unknown is an internet adapter.
func ParseKind(s string) Kind {
	for _, k := range []Kind{KindLAN, KindVirtual} {
		if k.String() == s {
			return k
		}
	}
	return KindInternet
}

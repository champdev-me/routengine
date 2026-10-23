package tun

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"routengine/internal/logx"
	"routengine/internal/nics"
)

const openHint = "needs root"

// macOS only accepts "utun" or "utunN" as a TUN name; "utun" lets the kernel pick the number.
func init() {
	openName = "utun"
	staleHook = cleanupScoped
	keepDevice = true
}

// configure gives the new utun its addresses and capture routes, and returns its index.
// ponytail: shells out to ifconfig/route (once per on/off); PF_ROUTE messages if that ever matters.
func configure(name string, _ int) (int, error) {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("adapter %q did not appear: %w", name, err)
	}
	// Before the capture routes: pinned sockets need these the moment the /1 routes exist.
	if err := startScopedSync(); err != nil {
		return ifc.Index, err
	}
	if err := run("/sbin/ifconfig", name, "inet", tunIP, tunIP, "up"); err != nil {
		return ifc.Index, err
	}
	for _, dst := range routes4 {
		if err := run("/sbin/route", "-q", "-n", "add", "-inet", dst, "-interface", name); err != nil {
			return ifc.Index, err
		}
	}
	// IPv6 is best-effort: if it can't be set up, IPv4 still works and IPv6 stays on its normal path.
	if err := run("/sbin/ifconfig", name, "inet6", tunIP6, "prefixlen", "64", "alias"); err != nil {
		logx.Printf("warning: IPv6 not captured: %v\n", err)
		return ifc.Index, nil
	}
	for _, dst := range routes6 {
		if err := run("/sbin/route", "-q", "-n", "add", "-inet6", dst, "-interface", name); err != nil {
			logx.Printf("warning: IPv6 route %s: %v\n", dst, err)
		}
	}
	return ifc.Index, nil
}

// delRoutes removes the capturing routes. Closing the utun removes them anyway, but deleting
// them first stops capture before the stack goes away.
func delRoutes(idx int) {
	defer stopScopedSync()
	ifc, err := net.InterfaceByIndex(idx)
	if err != nil {
		return // adapter already gone, and its routes with it
	}
	for _, dst := range routes4 {
		run("/sbin/route", "-q", "-n", "delete", "-inet", dst, "-interface", ifc.Name)
	}
	for _, dst := range routes6 {
		run("/sbin/route", "-q", "-n", "delete", "-inet6", dst, "-interface", ifc.Name)
	}
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Our own sockets are bound to an adapter (IP_BOUND_IF), and the kernel then looks routes up
// among that adapter's scoped routes. macOS only keeps a scoped default for non-primary services;
// the primary one has just an unscoped default, which the bound lookup falls back to until the
// more specific /1 capture routes shadow it ("no route to host", network dead). So every adapter
// with a default route also gets a scoped one, kept in step with gateway changes.
var realName = regexp.MustCompile(`^(en|feth)\d+$`)

type scopedKey struct {
	name string
	v6   bool
}

var scoped struct {
	mu    sync.Mutex
	added map[scopedKey]string // scoped defaults we added, with their gateway
	stop  chan struct{}        // non-nil while capture is on
}

func scopedArgs(verb string, k scopedKey, gw string) []string {
	fam := "-inet"
	if k.v6 {
		fam, gw = "-inet6", gw+"%"+k.name
	}
	return []string{"-q", "-n", verb, fam, "default", gw, "-ifscope", k.name}
}

func startScopedSync() error {
	stop := make(chan struct{})
	scoped.mu.Lock()
	scoped.stop, scoped.added = stop, map[scopedKey]string{}
	scoped.mu.Unlock()
	if err := syncScoped(stop); err != nil {
		stopScopedSync()
		return fmt.Errorf("scoped routes: %w", err)
	}
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if err := syncScoped(stop); err != nil {
					logx.Printf("warning: scoped routes: %v\n", err)
				}
			}
		}
	}()
	return nil
}

func stopScopedSync() {
	routes, _ := nics.DefaultRoutes()
	scoped.mu.Lock()
	defer scoped.mu.Unlock()
	if scoped.stop != nil {
		close(scoped.stop)
		scoped.stop = nil
	}
	primary := map[scopedKey]bool{}
	for _, r := range routes {
		if !r.Scoped && r.GW != nil {
			primary[scopedKey{r.Name, r.V6}] = true
		}
	}
	for k, gw := range scoped.added {
		// Only where the adapter is still primary: on one that stopped being primary since the last
		// sync, macOS needs this route as its own scoped default (see syncScoped).
		if routes == nil || primary[k] {
			run("/sbin/route", scopedArgs("delete", k, gw)...)
		}
	}
	scoped.added = nil
}

func syncScoped(stop chan struct{}) error {
	routes, err := nics.DefaultRoutes()
	if err != nil {
		return err
	}
	want := map[scopedKey]string{}
	have := map[scopedKey]bool{}
	for _, r := range routes {
		if !realName.MatchString(r.Name) {
			continue
		}
		k := scopedKey{r.Name, r.V6}
		if r.Scoped {
			have[k] = true
		} else if r.GW != nil {
			want[k] = r.GW.String()
		}
	}
	scoped.mu.Lock()
	defer scoped.mu.Unlock()
	if scoped.stop != stop {
		return nil // capture was switched off meanwhile
	}
	var first error
	for k, gw := range want {
		old, ours := scoped.added[k]
		switch {
		case ours && old == gw && have[k]:
			continue
		case !ours && have[k]:
			continue // macOS keeps its own scoped default here
		case ours:
			run("/sbin/route", scopedArgs("delete", k, old)...)
			delete(scoped.added, k)
		}
		if err := run("/sbin/route", scopedArgs("add", k, gw)...); err != nil && !strings.Contains(err.Error(), "File exists") {
			if first == nil {
				first = err
			}
			continue
		}
		scoped.added[k] = gw
	}
	for k, gw := range scoped.added {
		if _, ok := want[k]; !ok {
			// No longer the primary service (a tether took over): macOS now wants a scoped default
			// there itself, and ours, already in its place, made its own add fail. Leave the route to
			// macOS; deleting it would leave the adapter with no default route until it reconnects (#20).
			if !have[k] {
				run("/sbin/route", scopedArgs("delete", k, gw)...)
			}
			delete(scoped.added, k)
		}
	}
	return first
}

// cleanupScoped removes scoped defaults a crashed run left behind: on a primary adapter any scoped
// default that just repeats its unscoped one is ours, since macOS makes none there.
func cleanupScoped() {
	routes, err := nics.DefaultRoutes()
	if err != nil {
		return
	}
	unscoped := map[scopedKey]string{}
	for _, r := range routes {
		if !r.Scoped && r.GW != nil && realName.MatchString(r.Name) {
			unscoped[scopedKey{r.Name, r.V6}] = r.GW.String()
		}
	}
	for _, r := range routes {
		k := scopedKey{r.Name, r.V6}
		if gw, ok := unscoped[k]; ok && r.Scoped && r.GW != nil && r.GW.String() == gw {
			run("/sbin/route", scopedArgs("delete", k, gw)...)
		}
	}
}

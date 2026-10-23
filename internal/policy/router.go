package policy

import (
	"bytes"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	M "github.com/xjasonlyu/tun2socks/v2/metadata"

	"routengine/internal/procmap"
)

// Router turns a new flow into a Route: it finds the owning process, matches it against the rules
// and says which adapter the flow is pinned to. Flows nobody has a rule for stay balanced.
type Router struct {
	path   string
	set    atomic.Pointer[Set]
	lookup func(tcp bool, src netip.AddrPort) (procmap.Proc, bool)
	logf   func(format string, a ...any)

	// Auto, if set, names the adapter "default" stands for when the rules file does not choose one. It is
	// asked only for a flow whose rule actually falls back to "default".
	Auto func() string

	mu      sync.Mutex
	lastLog map[string]time.Time
	caps    atomic.Pointer[capState]
}

// Open loads the rules and keeps watching the file: edits apply to new flows within a second, and
// flows already open are left alone. A bad edit is reported and the previous rules stay in force.
func Open(path string, logf func(string, ...any)) (*Router, error) {
	set, seen, err := load(path)
	if errors.Is(err, os.ErrNotExist) {
		set, err = NewSet(nil) // no file yet is an empty list; the watcher picks the file up when it appears
	}
	if err != nil {
		return nil, err
	}
	r := &Router{path: path, lookup: procmap.Lookup, logf: logf, lastLog: map[string]time.Time{}}
	r.set.Store(set)
	logf("per-app routing: %d rule(s) from %s\n", set.Len(), path)
	if !procmap.Supported {
		logf("warning: per-app routing cannot identify applications on this platform yet; every flow is balanced\n")
	}
	go r.watch(seen)
	go r.maintain()
	return r, nil
}

// watch re-reads the file every second and applies it when its content changed. It compares
// content, not modification time: a quick same-length edit (adapter "One" to "Two") can leave both
// the size and the timestamp unchanged. A file caught half-written fails to parse, is reported, and
// is read again next second.
func (r *Router) watch(seen []byte) {
	for range time.Tick(time.Second) {
		b, err := os.ReadFile(r.path)
		if err != nil || bytes.Equal(b, seen) {
			continue
		}
		seen = b
		set, err := parse(r.path, b)
		if err != nil {
			r.logf("rules NOT applied, keeping the previous ones: %v\n", err)
			continue
		}
		r.set.Store(set)
		r.refreshCaps()
		r.syncMeters()
		r.logf("rules reloaded: %d rule(s)\n", set.Len())
	}
}

// note logs a routed flow, at most once a second per application and adapter, so a busy app does
// not flood the log.
func (r *Router) note(p procmap.Proc, m *M.Metadata, rule Rule) {
	target := rule.Adapter
	if len(rule.Balance) > 0 {
		target = "balanced over " + strings.Join(rule.Balance, " + ")
	}
	k := p.Exe + "|" + target
	now := time.Now()
	r.mu.Lock()
	if now.Sub(r.lastLog[k]) < time.Second {
		r.mu.Unlock()
		return
	}
	r.lastLog[k] = now
	r.mu.Unlock()
	r.logf("route %s %s (pid %d) -> %s  [%s]\n", m.Network, filepath.Base(p.Exe), p.PID, target, m.DestinationAddress())
}

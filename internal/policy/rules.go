// Package policy decides, per application, which adapter its connections leave through.
package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"routengine/internal/confdir"
	"routengine/internal/procmap"
	"strings"

	"routengine/internal/license"
)

// Merge as a rule's adapter means "balance across all adapters", i.e. plain balancing.
const Merge = "merge"

// Rule sends one application's flows through one adapter, or spreads them over several.
type Rule struct {
	Name string `json:"name,omitempty"`
	// Exe is the application: a full path, or a bare file name such as "chrome.exe" that matches in any folder.
	Exe string `json:"exe"`
	// Adapter pins the app to one adapter, named as `routengine list` shows it. "merge" balances over
	// all adapters. Give either Adapter or Balance, not both.
	Adapter string `json:"adapter,omitempty"`
	// Balance lets the app use these adapters at the same time: each new connection
	// goes to the least busy one, weighted by measured speed. Adapters outside the list are never
	// used, and one that goes down is skipped while the others carry on. A single connection still
	// stays on one adapter.
	Balance []string `json:"balance,omitempty"`
	// Failover lists backup adapters for a pinned app, tried in order when Adapter cannot carry a new
	// flow. Empty means no failover. Flows already open stay where they are, and new ones return to
	// Adapter as soon as it is healthy again.
	Failover []string `json:"failover,omitempty"`
	// OnDown is what happens when no adapter the rule names can carry the flow: "block" (the default,
	// strict: the flow fails rather than leaving through an adapter the rule did not name) or
	// "merge" (balance across all adapters).
	OnDown   string `json:"on_down,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`

	// The fields below are premium features. They are kept for everyone, but only take effect with a
	// license; without one the engine ignores them.

	// DNS is the DNS server the app uses, as addresses such as "1.1.1.1". It covers apps that look
	// names up themselves (browsers, many games); apps that ask Windows' own resolver are not covered.
	DNS []string `json:"dns,omitempty"`
	// LimitDown and LimitUp cap the app's speed, in megabits per second. 0 is no limit.
	LimitDown float64 `json:"limit_down_mbit,omitempty"`
	LimitUp   float64 `json:"limit_up_mbit,omitempty"`
	// CapGB stops the app once it has used this many gigabytes in the period. 0 is no cap.
	CapGB float64 `json:"cap_gb,omitempty"`
	// CapPer is the period the cap counts over: "day" or "month" (the default).
	CapPer string `json:"cap_per,omitempty"`
	// Fastest sends each new flow through whichever of the rule's adapters answers fastest. It goes
	// with "balance" (those adapters) or "adapter": "merge" (all of them).
	Fastest bool `json:"fastest,omitempty"`
}

// UsesPremium reports whether the rule asks for anything only a license unlocks.
func (r Rule) UsesPremium() bool {
	return len(r.DNS) > 0 || r.LimitDown > 0 || r.LimitUp > 0 || r.CapGB > 0 || r.Fastest
}

// DNSAddrs is the rule's DNS servers as addresses. Invalid entries are skipped (validation rejects them).
func (r Rule) DNSAddrs() []netip.Addr {
	var out []netip.Addr
	for _, s := range r.DNS {
		if a, err := netip.ParseAddr(strings.TrimSpace(s)); err == nil {
			out = append(out, a.Unmap())
		}
	}
	return out
}

// checkPremium validates the premium fields of one rule.
func checkPremium(r Rule) error {
	if len(r.DNS) > 4 {
		return errors.New(`"dns" takes at most four servers`)
	}
	for _, s := range r.DNS {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil || a.IsUnspecified() || a.IsMulticast() {
			return fmt.Errorf(`"dns" entry %q is not a server address such as 1.1.1.1`, s)
		}
	}
	for name, v := range map[string]float64{"limit_down_mbit": r.LimitDown, "limit_up_mbit": r.LimitUp, "cap_gb": r.CapGB} {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) || v > 1e7 {
			return fmt.Errorf("%q must be a number from 0 up", name)
		}
	}
	if r.CapPer != "" && r.CapPer != "day" && r.CapPer != "month" {
		return fmt.Errorf(`"cap_per" must be "day" or "month", got %q`, r.CapPer)
	}
	if r.Fastest && len(r.Balance) < 2 && !strings.EqualFold(strings.TrimSpace(r.Adapter), Merge) {
		return errors.New(`"fastest" needs "balance" (two or more adapters) or "adapter": "merge"`)
	}
	return nil
}

// File is the rules file on disk.
type File struct {
	// DefaultAdapter is the adapter a failover list can name with "default". It lets one setting say
	// "everything falls back to this adapter" without repeating its name in every rule.
	DefaultAdapter string `json:"default_adapter,omitempty"`
	Rules          []Rule `json:"rules"`
}

// DefaultToken, in a rule's failover list, stands for the file's default adapter.
const DefaultToken = "default"

// Set is a validated, ready-to-match list of rules.
type Set struct {
	// Default is the default adapter ("" when none is set).
	Default string
	rules   []Rule
	keys    []string // normalised Exe, parallel to rules
}

// slashed makes both separators one, so a rule written on Windows (C:\Games\x.exe) reads the same everywhere.
func slashed(p string) string { return strings.ReplaceAll(strings.TrimSpace(p), `\`, "/") }

func norm(p string) string { return strings.ToLower(path.Clean(slashed(p))) }

func hasDir(p string) bool { return strings.ContainsAny(p, `\/`) }

// NewSet validates rules and prepares them for matching.
func NewSet(rules []Rule) (*Set, error) { return newSet(File{Rules: rules}) }

func newSet(f File) (*Set, error) {
	def := strings.TrimSpace(f.DefaultAdapter)
	if strings.EqualFold(def, Merge) {
		return nil, errors.New(`default_adapter must be an adapter name, not "merge"`)
	}
	s := &Set{Default: def}
	for i, r := range f.Rules {
		switch {
		case strings.TrimSpace(r.Exe) == "":
			return nil, fmt.Errorf("rule %d: exe is empty", i+1)
		case r.OnDown != "" && r.OnDown != "block" && r.OnDown != "merge":
			return nil, fmt.Errorf(`rule %d (%s): on_down must be "block" or "merge", got %q`, i+1, r.Exe, r.OnDown)
		}
		if err := checkAdapters(r); err != nil {
			return nil, fmt.Errorf("rule %d (%s): %w", i+1, r.Exe, err)
		}
		if err := checkPremium(r); err != nil {
			return nil, fmt.Errorf("rule %d (%s): %w", i+1, r.Exe, err)
		}
		s.rules = append(s.rules, r)
		s.keys = append(s.keys, norm(r.Exe))
	}
	return s, nil
}

// checkAdapters validates the adapter fields of one rule.
func checkAdapters(r Rule) error {
	adapter := strings.TrimSpace(r.Adapter)
	if len(r.Balance) > 0 {
		switch {
		case adapter != "":
			return errors.New(`give "adapter" or "balance", not both`)
		case len(r.Failover) > 0:
			return errors.New(`"failover" is for a pinned app; a balanced app already skips adapters that are down`)
		case len(r.Balance) < 2:
			return errors.New(`"balance" needs at least two adapters; use "adapter" for one`)
		}
		return noDuplicates("balance", nil, r.Balance)
	}
	if adapter == "" {
		return errors.New(`give "adapter" (one adapter, or "merge") or "balance" (a list)`)
	}
	return noDuplicates("failover", []string{adapter}, r.Failover)
}

// noDuplicates rejects empty names, "merge" and repeats in list (case-insensitive); first holds names already taken.
func noDuplicates(field string, first, list []string) error {
	seen := map[string]bool{}
	for _, n := range first {
		seen[strings.ToLower(strings.TrimSpace(n))] = true
	}
	for _, n := range list {
		k := strings.ToLower(strings.TrimSpace(n))
		switch {
		case k == "":
			return fmt.Errorf("an adapter in %q is empty", field)
		case k == Merge:
			return fmt.Errorf(`%q cannot contain "merge": use on_down "merge", or "adapter": "merge"`, field)
		case seen[k]:
			return fmt.Errorf("adapter %q is listed twice", n)
		}
		seen[k] = true
	}
	return nil
}

// Len is the number of rules, enabled or not.
func (s *Set) Len() int { return len(s.rules) }

// Match returns the first enabled rule for the executable at path exe.
func (s *Set) Match(exe string) (Rule, bool) {
	i, ok := s.MatchIndex(exe)
	if !ok {
		return Rule{}, false
	}
	return s.rules[i], true
}

// Load reads and validates a rules file.
func Load(path string) (*Set, error) {
	set, _, err := load(path)
	return set, err
}

// load also returns the file's bytes, so a watcher can tell later whether it changed.
func load(path string) (*Set, []byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	set, err := parse(path, b)
	return set, b, err
}

func parse(path string, b []byte) (*Set, error) {
	var f File
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields() // a typo like "adaptor" must not silently turn into a rule that does nothing
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return newSet(f)
}

// DefaultPath is where the rules live when -rules is not given: it survives restarts and reboots,
// and the autostart entry picks it up without extra arguments. A config.json, if there is one, is the
// same file under the name headless installs use.
func DefaultPath() string {
	dir := confdir.Dir()
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return filepath.Join(dir, "config.json")
	}
	return filepath.Join(dir, "rules.json")
}

// Rules returns a copy of the rules, enabled or not, in file order.
func (s *Set) Rules() []Rule { return append([]Rule(nil), s.rules...) }

// MatchIndex returns the position in Rules of the first enabled rule for the executable at path exe.
func (s *Set) MatchIndex(exe string) (int, bool) {
	if exe == "" {
		return 0, false
	}
	full, base := norm(exe), strings.ToLower(path.Base(slashed(exe)))
	app := procmap.Bundle(exe)
	for i, r := range s.rules {
		if r.Disabled {
			continue
		}
		k := s.keys[i]
		if (hasDir(r.Exe) && k == full) || (!hasDir(r.Exe) && k == base) {
			return i, true
		}
		// A rule naming a macOS app matches every program inside it.
		if app != "" && strings.HasSuffix(k, ".app") && ((hasDir(r.Exe) && k == norm(app)) || (!hasDir(r.Exe) && k == strings.ToLower(path.Base(app)))) {
			return i, true
		}
	}
	return 0, false
}

// LoadRules reads the rules file for editing. A missing file is an empty list.
func LoadRules(path string) ([]Rule, error) {
	set, err := Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return set.Rules(), nil
}

// LoadFile reads the whole rules file for editing: rules and default adapter. A missing file is empty.
func LoadFile(path string) (File, error) {
	set, err := Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	return File{DefaultAdapter: set.Default, Rules: set.Rules()}, nil
}

// SaveRules writes new rules and keeps the default adapter the file already has.
func SaveRules(path string, rules []Rule) error {
	cur, _ := LoadFile(path) // an unreadable file has no default to keep
	return SaveFile(path, File{DefaultAdapter: cur.DefaultAdapter, Rules: rules})
}

// SaveFile validates f and writes it to path in one step (a temporary file, then a rename), so the
// running service never reads a half-written file. It picks the change up within a second.
func SaveFile(path string, f File) error {
	if _, err := newSet(f); err != nil {
		return err
	}
	if f.Rules == nil {
		f.Rules = []Rule{}
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Describe says in a few words where a rule sends its app, for lists and chips.
func (r Rule) Describe() string { return r.DescribeWith("") }

// DescribeWith is Describe, naming the default adapter where the rule falls back to it.
func (r Rule) DescribeWith(def string) string {
	switch {
	case len(r.Balance) > 0:
		return strings.Join(r.Balance, " + ") + ", together"
	case r.Adapter == Merge:
		return "any adapter"
	case len(r.Failover) > 0:
		names := make([]string, len(r.Failover))
		for i, n := range r.Failover {
			names[i] = n
			if strings.EqualFold(n, DefaultToken) {
				names[i] = "default adapter"
				if def != "" {
					names[i] = "default (" + def + ")"
				}
			}
		}
		return r.Adapter + ", backup " + strings.Join(names, ", ")
	}
	return r.Adapter
}

// ResolveFailover turns a rule's failover list into adapter names: "default" becomes the default
// adapter (and disappears if none is set), and the rule's own adapter and repeats are dropped, so
// "fall back to the default" does nothing for an app that already uses the default.
func ResolveFailover(list []string, adapter, def string) []string {
	var out []string
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(adapter)): true}
	for _, n := range list {
		if strings.EqualFold(strings.TrimSpace(n), DefaultToken) {
			n = def
		}
		k := strings.ToLower(strings.TrimSpace(n))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, n)
	}
	return out
}

func (r Rule) usesSpeedLimit() bool { return r.LimitDown > 0 || r.LimitUp > 0 }
func (r Rule) usesCap() bool        { return r.CapGB > 0 }

// slotOf is the place of rule idx among the rules for which uses is true, in list order, counting only rules in
// force: not disabled and, for the free edition, within its first license.FreeApps. It is -1 for a rule that does
// not use the feature or is not in force.
func slotOf(rules []Rule, idx int, unlimitedApps bool, uses func(Rule) bool) int {
	n := 0
	for i, r := range rules {
		if r.Disabled || (!unlimitedApps && i >= license.FreeApps) || !uses(r) {
			continue
		}
		if i == idx {
			return n
		}
		n++
	}
	return -1
}

// slotsInUse is how many rules in force, other than the one at except, use the feature.
func slotsInUse(rules []Rule, except int, unlimitedApps bool, uses func(Rule) bool) int {
	n := 0
	for i, r := range rules {
		if i != except && !r.Disabled && (unlimitedApps || i < license.FreeApps) && uses(r) {
			n++
		}
	}
	return n
}

// SpeedSlot is the place of rule idx among the rules with a speed limit. The free edition honours the first
// license.FreeSpeedLimitApps places only.
func SpeedSlot(rules []Rule, idx int, unlimitedApps bool) int {
	return slotOf(rules, idx, unlimitedApps, Rule.usesSpeedLimit)
}

// CapSlot is the place of rule idx among the rules with a data cap. The free edition honours the first
// license.FreeCapApps places only.
func CapSlot(rules []Rule, idx int, unlimitedApps bool) int {
	return slotOf(rules, idx, unlimitedApps, Rule.usesCap)
}

// SpeedSlotsInUse and CapSlotsInUse count the other rules that hold a place, to tell whether one is free.
func SpeedSlotsInUse(rules []Rule, except int, unlimitedApps bool) int {
	return slotsInUse(rules, except, unlimitedApps, Rule.usesSpeedLimit)
}
func CapSlotsInUse(rules []Rule, except int, unlimitedApps bool) int {
	return slotsInUse(rules, except, unlimitedApps, Rule.usesCap)
}

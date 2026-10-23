package policy

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"routengine/internal/engine"
	"routengine/internal/license"
	"routengine/internal/usage"
)

func asFree(t *testing.T) { t.Helper(); t.Cleanup(license.SetForTest(nil)) }
func asPro(t *testing.T) {
	t.Helper()
	t.Cleanup(license.SetForTest(&license.License{Plan: "pro", Features: license.ProFeatures}))
}

func premiumRule(exe string) Rule {
	return Rule{
		Exe: exe, Adapter: "WiFi",
		DNS: []string{"1.1.1.1"}, LimitDown: 4, LimitUp: 1, CapGB: 2, CapPer: "day",
	}
}

func TestTheFreeEditionIgnoresProSettingsButLimitsOneApp(t *testing.T) {
	asFree(t)
	rule := premiumRule(`C:\free\only-free.exe`)
	r := newTestRouter(t, map[uint16]string{1: rule.Exe}, rule)
	rt := r.Route(flow(1))
	if len(rt.DNS) != 0 || rt.Fastest || rt.Deny != "" {
		t.Fatalf("free: DNS must not apply: %+v", rt)
	}
	if rt.Adapter != "WiFi" {
		t.Fatalf("the plain routing still works for everyone: %+v", rt)
	}
	if d, u := rt.Meter.Limits(); d != 4*engine.BytesPerMbit || u != 1*engine.BytesPerMbit {
		t.Fatalf("free: its one app keeps its speed limit: %v %v", d, u)
	}
}

func TestTheFreeEditionLimitsAndCapsEveryAppItRoutes(t *testing.T) {
	asFree(t)
	first := Rule{Exe: `C:/one/first.exe`, Adapter: "WiFi", LimitDown: 4}
	capped := Rule{Exe: `C:/one/second.exe`, Adapter: "WiFi", CapGB: 1}
	both := Rule{Exe: `C:/one/third.exe`, Adapter: "WiFi", LimitUp: 2, CapGB: 5}
	r := newTestRouter(t, map[uint16]string{1: first.Exe, 2: capped.Exe, 3: both.Exe}, first, capped, both)
	r.caps.Store(&capState{set: r.set.Load(), over: []bool{false, true, true}}) // the second and third have used their gigabytes

	if d, _ := r.Route(flow(1)).Meter.Limits(); d != 4*engine.BytesPerMbit {
		t.Fatalf("the first app with a speed limit keeps it: %v", d)
	}
	if rt := r.Route(flow(2)); rt.Deny != "data cap reached" || !rt.Meter.Denied() {
		t.Fatalf("an app with a data cap keeps it, even though another app holds a speed limit: %+v", rt)
	}
	rt := r.Route(flow(3))
	if _, u := rt.Meter.Limits(); u != 2*engine.BytesPerMbit {
		t.Fatalf("a third free app keeps its speed limit: %v", u)
	}
	if rt.Deny != "data cap reached" || !rt.Meter.Denied() {
		t.Fatalf("and its data cap: %+v", rt)
	}

	asPro(t)
	if _, u := r.Route(flow(3)).Meter.Limits(); u != 2*engine.BytesPerMbit {
		t.Fatalf("Pro: every app is limited: %v", u)
	}
}

func TestTheSameAppCanHoldTheSpeedLimitAndTheCap(t *testing.T) {
	asFree(t)
	rule := Rule{Exe: `C:/both/both.exe`, Adapter: "WiFi", LimitDown: 3, CapGB: 1}
	r := newTestRouter(t, map[uint16]string{1: rule.Exe}, rule)
	r.caps.Store(&capState{set: r.set.Load(), over: []bool{true}})
	rt := r.Route(flow(1))
	if d, _ := rt.Meter.Limits(); d != 3*engine.BytesPerMbit || rt.Deny != "data cap reached" {
		t.Fatalf("one app may have both: limit %v, %+v", d, rt)
	}
}

func TestSpeedAndCapPlacesAreCountedSeparatelyAndOnlyForRulesInForce(t *testing.T) {
	rules := []Rule{
		{Exe: "a.exe", Adapter: "W"},                               // sets nothing
		{Exe: "b.exe", Adapter: "W", LimitDown: 1, Disabled: true}, // not in force
		{Exe: "c.exe", Adapter: "W", CapGB: 1},                     // the first cap
		{Exe: "d.exe", Adapter: "W", LimitUp: 1},                   // beyond the free edition's three apps
		{Exe: "e.exe", Adapter: "W", LimitUp: 1, CapGB: 1},         // ditto
	}
	for idx, want := range map[int]int{0: -1, 1: -1, 2: -1, 3: -1, 4: -1} {
		if got := SpeedSlot(rules, idx, false); got != want {
			t.Errorf("free, speed, rule %d: slot %d, want %d", idx, got, want)
		}
	}
	for idx, want := range map[int]int{0: -1, 1: -1, 2: 0, 3: -1, 4: -1} {
		if got := CapSlot(rules, idx, false); got != want {
			t.Errorf("free, cap, rule %d: slot %d, want %d", idx, got, want)
		}
	}
	for idx, want := range map[int]int{3: 0, 4: 1} {
		if got := SpeedSlot(rules, idx, true); got != want {
			t.Errorf("unlimited apps, speed, rule %d: slot %d, want %d", idx, got, want)
		}
	}
	if got := CapSlot(rules, 4, true); got != 1 {
		t.Errorf("unlimited apps, cap, rule 4: slot %d, want 1", got)
	}
	if n := SpeedSlotsInUse(rules, -1, false); n != 0 {
		t.Errorf("no speed limit is in force for the free edition, got %d", n)
	}
	if n := CapSlotsInUse(rules, -1, false); n != 1 {
		t.Errorf("one cap is in force, got %d", n)
	}
	if n := CapSlotsInUse(rules, 2, false); n != 0 {
		t.Errorf("apart from c.exe no cap is in force, got %d", n)
	}
	if n := SpeedSlotsInUse(rules, -1, true); n != 2 {
		t.Errorf("with unlimited apps two speed limits are in force, got %d", n)
	}
}

func TestAProEditionAppliesEveryPremiumSetting(t *testing.T) {
	asPro(t)
	rule := premiumRule(`C:\pro\only-pro.exe`)
	rule.Adapter, rule.Balance, rule.Fastest = "", []string{"WiFi", "Ethernet 4"}, true
	r := newTestRouter(t, map[uint16]string{1: rule.Exe}, rule)
	rt := r.Route(flow(1))
	want := []string{"1.1.1.1"}
	if got := []string{rt.DNS[0].String()}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DNS: %v", rt.DNS)
	}
	if !rt.Fastest || !reflect.DeepEqual(rt.Pool, []string{"WiFi", "Ethernet 4"}) {
		t.Fatalf("fastest over the pool: %+v", rt)
	}
	d, u := rt.Meter.Limits()
	if d != 4*engine.BytesPerMbit || u != 1*engine.BytesPerMbit {
		t.Fatalf("limits in bytes/s: %v %v", d, u)
	}
}

func TestFastestWithMergeUsesAllInternetAdapters(t *testing.T) {
	asPro(t)
	rule := Rule{Exe: `C:\pro\merge-fast.exe`, Adapter: Merge, Fastest: true}
	r := newTestRouter(t, map[uint16]string{1: rule.Exe}, rule)
	if rt := r.Route(flow(1)); !rt.Fastest || rt.Adapter != "" || len(rt.Pool) != 0 {
		t.Fatalf("merge + fastest = fastest of all: %+v", rt)
	}
}

func TestTheFreeEditionHonoursOnlyItsFirstFiveApps(t *testing.T) {
	var rules []Rule
	owners := map[uint16]string{}
	for i := 0; i < 7; i++ {
		exe := `C:\five\app` + string(rune('a'+i)) + `.exe`
		rules = append(rules, Rule{Exe: exe, Adapter: "WiFi"})
		owners[uint16(i+1)] = exe
	}
	asFree(t)
	r := newTestRouter(t, owners, rules...)
	for i := 0; i < 7; i++ {
		rt := bare(r.Route(flow(uint16(i + 1))))
		if i < license.FreeApps && rt.Adapter != "WiFi" {
			t.Errorf("app %d is inside the free allowance: %+v", i+1, rt)
		}
		if i >= license.FreeApps && rt.Adapter != "" {
			t.Errorf("app %d is beyond the free allowance and must be inert: %+v", i+1, rt)
		}
	}
	asPro(t)
	if rt := bare(r.Route(flow(7))); rt.Adapter != "WiFi" {
		t.Fatalf("Pro has no limit on apps: %+v", rt)
	}
}

func TestACappedAppIsRefused(t *testing.T) {
	holder := Rule{Exe: `C:capholder.exe`, Adapter: "WiFi", CapGB: 1}
	rule := Rule{Exe: `C:\cap\capped.exe`, Adapter: "WiFi", CapGB: 1}
	r := newTestRouter(t, map[uint16]string{1: rule.Exe}, holder, rule)
	set := r.set.Load()
	r.caps.Store(&capState{set: set, over: []bool{false, true}}) // it has used its gigabyte

	asFree(t)
	if rt := r.Route(flow(1)); rt.Deny != "data cap reached" {
		t.Fatalf("the free edition caps data too: %+v", rt)
	}
	asPro(t)
	rt := r.Route(flow(1))
	if rt.Deny != "data cap reached" || !rt.Meter.Denied() {
		t.Fatalf("over the cap: new flows are refused and open ones cut: %+v denied=%v", rt, rt.Meter.Denied())
	}
	r.caps.Store(&capState{set: set, over: []bool{false, false}}) // the day rolled over
	if rt := r.Route(flow(1)); rt.Deny != "" || rt.Meter.Denied() {
		t.Fatalf("under the cap again: %+v", rt)
	}
}

func TestCapsAreWorkedOutFromTheUsageHistory(t *testing.T) {
	asPro(t)
	l, _ := usage.Open(filepath.Join(t.TempDir(), "usage.json"))
	engine.Apps.SetLedger(l)
	defer engine.Apps.SetLedger(nil)

	day := Rule{Exe: `C:\usage\daycap.exe`, Adapter: "WiFi", CapGB: 1, CapPer: "day"}
	month := Rule{Exe: `C:\usage\monthcap.exe`, Adapter: "WiFi", CapGB: 5}
	none := Rule{Exe: `C:\usage\nocap.exe`, Adapter: "WiFi"}
	r := newTestRouter(t, map[uint16]string{}, day, month, none)

	gib := int64(1 << 30)
	l.AddApp(day.Exe, "daycap.exe", "WiFi", gib/2, gib/2+1) // a hair over 1 GiB today
	l.AddApp(month.Exe, "monthcap.exe", "WiFi", 2*gib, 0)   // 2 of 5 GiB this month
	l.AddApp(none.Exe, "nocap.exe", "WiFi", 100*gib, 0)     // a lot, but it has no cap
	r.refreshCaps()
	set := r.set.Load()
	if !r.capped(set, 0) || r.capped(set, 1) || r.capped(set, 2) {
		t.Fatalf("only the day-capped app is over: %v %v %v", r.capped(set, 0), r.capped(set, 1), r.capped(set, 2))
	}
	l.AddApp(month.Exe, "monthcap.exe", "WiFi", 3*gib, 0) // now exactly its 5 GiB
	r.refreshCaps()
	if !r.capped(set, 1) {
		t.Fatal("reaching the allowance counts as over")
	}
	// A reloaded rule set must not inherit the old verdicts by index.
	other, _ := NewSet([]Rule{none})
	if r.capped(other, 0) {
		t.Fatal("verdicts belong to the rule set they were computed for")
	}
}

func TestSyncMetersFollowsRulesAndLicenseForRunningApps(t *testing.T) {
	holder := Rule{Exe: `C:\sync\holder.exe`, Adapter: "WiFi", LimitDown: 1}
	rule := Rule{Exe: `C:\sync\running.exe`, Adapter: "WiFi", LimitDown: 8}
	r := newTestRouter(t, map[uint16]string{}, holder, rule)
	m := engine.Apps.For(rule.Exe)

	asPro(t)
	r.refreshCaps()
	r.syncMeters()
	if d, _ := m.Limits(); d != 8*engine.BytesPerMbit {
		t.Fatalf("a running app picks up its limit without a new connection: %v", d)
	}
	asFree(t)
	r.syncMeters()
	if d, _ := m.Limits(); d != 8*engine.BytesPerMbit {
		t.Fatalf("and the free edition keeps it too: %v", d)
	}
	asPro(t)
	r.set.Store(mustSet(t)) // the rule is deleted
	r.syncMeters()
	if d, _ := m.Limits(); d != 0 {
		t.Fatalf("or when the rule is deleted: %v", d)
	}
}

func TestPremiumSettingsAreValidated(t *testing.T) {
	bad := map[string]Rule{
		"dns not an address":        {Exe: "a.exe", Adapter: "W", DNS: []string{"dns.google"}},
		"dns unspecified":           {Exe: "a.exe", Adapter: "W", DNS: []string{"0.0.0.0"}},
		"too many dns":              {Exe: "a.exe", Adapter: "W", DNS: []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "9.9.9.9"}},
		"negative limit":            {Exe: "a.exe", Adapter: "W", LimitDown: -1},
		"absurd cap":                {Exe: "a.exe", Adapter: "W", CapGB: 1e12},
		"unknown cap period":        {Exe: "a.exe", Adapter: "W", CapGB: 1, CapPer: "week"},
		"fastest on one adapter":    {Exe: "a.exe", Adapter: "W", Fastest: true},
		"fastest on one pool entry": {Exe: "a.exe", Balance: []string{"W"}, Fastest: true},
	}
	for name, r := range bad {
		if _, err := NewSet([]Rule{r}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	good := []Rule{
		{Exe: "a.exe", Adapter: "W", DNS: []string{"1.1.1.1", "2606:4700:4700::1111"}, LimitDown: 2.5, LimitUp: 0.5, CapGB: 10, CapPer: "month"},
		{Exe: "b.exe", Balance: []string{"W", "E"}, Fastest: true},
		{Exe: "c.exe", Adapter: Merge, Fastest: true},
	}
	if _, err := NewSet(good); err != nil {
		t.Fatalf("valid premium settings: %v", err)
	}
	if !good[0].UsesPremium() || (Rule{Exe: "x", Adapter: "W"}).UsesPremium() {
		t.Fatal("UsesPremium tells premium rules from plain ones")
	}
	if a := good[0].DNSAddrs(); len(a) != 2 || !strings.Contains(a[1].String(), ":") {
		t.Fatalf("DNSAddrs: %v", a)
	}
}

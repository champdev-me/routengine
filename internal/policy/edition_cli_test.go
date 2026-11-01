//go:build nogui

package policy

import (
	"strings"
	"testing"

	"routengine/internal/engine"
)

// The command line has the free features on any number of apps, with no license: every rule routes, limits and caps.
func TestTheCommandLineLimitsAndCapsEveryAppWithNoAppLimit(t *testing.T) {
	var rules []Rule
	owners := map[uint16]string{}
	for i := 1; i <= 6; i++ { // twice the free window app's three
		exe := `C:/cli/app` + string(rune('0'+i)) + `.exe`
		rules = append(rules, Rule{Exe: exe, Adapter: "WiFi", LimitDown: float64(i), CapGB: 1})
		owners[uint16(i)] = exe
	}
	r := newTestRouter(t, owners, rules...)
	r.caps.Store(&capState{set: r.set.Load(), over: []bool{false, false, false, false, false, true}}) // the sixth used its gigabyte

	for i := 1; i <= 5; i++ {
		rt := r.Route(flow(uint16(i)))
		if rt.Adapter != "WiFi" {
			t.Fatalf("app %d is routed by its rule: %+v", i, rt)
		}
		if d, _ := rt.Meter.Limits(); d != float64(i)*engine.BytesPerMbit {
			t.Fatalf("app %d keeps its speed limit: %v", i, d)
		}
	}
	if rt := r.Route(flow(6)); rt.Deny != "data cap reached" || !rt.Meter.Denied() {
		t.Fatalf("the sixth app's data cap holds: %+v", rt)
	}
}

// A rules file written for the window app, with Pro settings, still loads in the command line. They are kept and
// never applied, as the free window app treats them, so copying a rules file over cannot stop the service.
func TestTheCommandLineKeepsProSettingsButNeverAppliesThem(t *testing.T) {
	rule := Rule{Exe: `C:/cli/pro.exe`, Balance: []string{"WiFi", "Ethernet"}, Fastest: true,
		DNS: []string{"1.1.1.1", "not an address"}, AdapterLimits: map[string]engine.Speed{"WiFi": {Down: 5, Up: 1}}}
	set, err := newSet(File{Rules: []Rule{rule}, AdapterLimits: map[string]engine.Speed{"Ethernet": engine.Both(20)}})
	if err != nil {
		t.Fatalf("a rules file with Pro settings must load: %v", err)
	}
	if len(set.AdapterLimits) != 0 {
		t.Fatalf("no adapter is capped in the command line: %v", set.AdapterLimits)
	}
	r := newTestRouter(t, map[uint16]string{1: rule.Exe}, rule)
	rt := r.Route(flow(1))
	if rt.Fastest || len(rt.DNS) != 0 {
		t.Fatalf("no fastest adapter and no DNS of its own: %+v", rt)
	}
	if strings.Join(rt.Pool, ",") != "WiFi,Ethernet" {
		t.Fatalf("its adapters are used as usual: %+v", rt)
	}
	if got := rt.Meter.AdapterLimit("WiFi"); got.Limits() {
		t.Fatalf("no cap per adapter: %v", got)
	}
}

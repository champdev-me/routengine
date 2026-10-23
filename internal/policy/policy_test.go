package policy

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	M "github.com/xjasonlyu/tun2socks/v2/metadata"

	"routengine/internal/engine"
	"routengine/internal/procmap"
)

func mustSet(t *testing.T, rules ...Rule) *Set {
	t.Helper()
	s, err := NewSet(rules)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMatchByNameOrFullPath(t *testing.T) {
	s := mustSet(t,
		Rule{Exe: "Chrome.exe", Adapter: "WiFi"},
		Rule{Exe: `C:\Games\Dota\dota2.exe`, Adapter: "Ethernet"},
	)
	for exe, want := range map[string]string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`: "WiFi", // bare name matches in any folder, any case
		`D:\portable\CHROME.EXE`:                                "WiFi",
		`c:\games\dota\DOTA2.exe`:                               "Ethernet",
		`D:\Games\Dota\dota2.exe`:                               "", // a full path only matches that exact file
		`C:\Windows\notepad.exe`:                                "",
		"":                                                      "",
	} {
		got, ok := s.Match(exe)
		if got.Adapter != want || ok != (want != "") {
			t.Errorf("%q: got %q ok=%v, want %q", exe, got.Adapter, ok, want)
		}
	}
}

func TestFirstEnabledRuleWins(t *testing.T) {
	s := mustSet(t,
		Rule{Exe: "a.exe", Adapter: "Off", Disabled: true},
		Rule{Exe: "a.exe", Adapter: "First"},
		Rule{Exe: "a.exe", Adapter: "Second"},
	)
	if r, _ := s.Match("a.exe"); r.Adapter != "First" {
		t.Fatalf("got %q, want First", r.Adapter)
	}
}

func TestInvalidRulesAreRejected(t *testing.T) {
	for name, r := range map[string]Rule{
		"no exe":      {Adapter: "WiFi"},
		"no adapter":  {Exe: "a.exe"},
		"bad on_down": {Exe: "a.exe", Adapter: "WiFi", OnDown: "maybe"},
	} {
		if _, err := NewSet([]Rule{r}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestLoadRejectsTypos(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rules.json")
	os.WriteFile(p, []byte(`{"rules":[{"exe":"a.exe","adaptor":"WiFi"}]}`), 0o644)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "adaptor") {
		t.Fatalf("a misspelt field must be an error, got %v", err)
	}
}

func newTestRouter(t *testing.T, owners map[uint16]string, rules ...Rule) *Router {
	t.Helper()
	r := &Router{
		lastLog: map[string]time.Time{},
		logf:    func(string, ...any) {},
		lookup: func(tcp bool, src netip.AddrPort) (procmap.Proc, bool) {
			exe, ok := owners[src.Port()]
			return procmap.Proc{PID: uint32(src.Port()), Exe: exe}, ok
		},
	}
	r.set.Store(mustSet(t, rules...))
	return r
}

func flow(port uint16) *M.Metadata {
	return &M.Metadata{
		Network: M.TCP,
		SrcIP:   netip.MustParseAddr("198.18.0.1"), SrcPort: port,
		DstIP: netip.MustParseAddr("93.184.216.34"), DstPort: 443,
	}
}

func TestTwoAppsGetDifferentAdapters(t *testing.T) {
	r := newTestRouter(t,
		map[uint16]string{1000: `C:\a\chrome.exe`, 2000: `C:\b\steam.exe`, 3000: `C:\c\notepad.exe`},
		Rule{Exe: "chrome.exe", Adapter: "WiFi"},
		Rule{Exe: "steam.exe", Adapter: "Ethernet", OnDown: "merge"},
	)
	if rt := bare(r.Route(flow(1000))); !reflect.DeepEqual(rt, engine.Route{Adapter: "WiFi", OnDown: engine.Block}) {
		t.Errorf("chrome: %+v", rt)
	}
	if rt := bare(r.Route(flow(2000))); !reflect.DeepEqual(rt, engine.Route{Adapter: "Ethernet", OnDown: engine.Merge}) {
		t.Errorf("steam: %+v", rt)
	}
	if rt := bare(r.Route(flow(3000))); !reflect.DeepEqual(rt, engine.Route{}) {
		t.Errorf("an app without a rule must stay balanced, got %+v", rt)
	}
	if rt := bare(r.Route(flow(4000))); !reflect.DeepEqual(rt, engine.Route{}) {
		t.Errorf("an unknown owner must stay balanced, got %+v", rt)
	}
}

func TestMergeRuleBalances(t *testing.T) {
	r := newTestRouter(t, map[uint16]string{1: "a.exe"}, Rule{Exe: "a.exe", Adapter: Merge})
	if rt := bare(r.Route(flow(1))); !reflect.DeepEqual(rt, engine.Route{}) {
		t.Fatalf("got %+v", rt)
	}
}

func TestFailoverListReachesTheRoute(t *testing.T) {
	r := newTestRouter(t, map[uint16]string{1: "a.exe"}, Rule{Exe: "a.exe", Adapter: "Tether", Failover: []string{"WiFi", "Ethernet"}})
	want := engine.Route{Adapter: "Tether", Failover: []string{"WiFi", "Ethernet"}, OnDown: engine.Block}
	if rt := bare(r.Route(flow(1))); !reflect.DeepEqual(rt, want) {
		t.Fatalf("got %+v, want %+v", rt, want)
	}
}

func TestBalanceRuleReachesTheRoute(t *testing.T) {
	r := newTestRouter(t, map[uint16]string{1: "a.exe"}, Rule{Exe: "a.exe", Balance: []string{"WiFi", "Ethernet 4"}})
	want := engine.Route{Pool: []string{"WiFi", "Ethernet 4"}, OnDown: engine.Block}
	if rt := bare(r.Route(flow(1))); !reflect.DeepEqual(rt, want) {
		t.Fatalf("got %+v, want %+v", rt, want)
	}
}

func TestBadBalanceRulesAreRejected(t *testing.T) {
	for name, r := range map[string]Rule{
		"adapter and balance": {Exe: "a.exe", Adapter: "WiFi", Balance: []string{"WiFi", "Eth"}},
		"one adapter only":    {Exe: "a.exe", Balance: []string{"WiFi"}},
		"failover too":        {Exe: "a.exe", Balance: []string{"WiFi", "Eth"}, Failover: []string{"X"}},
		"repeated adapter":    {Exe: "a.exe", Balance: []string{"WiFi", "wifi"}},
		"merge inside":        {Exe: "a.exe", Balance: []string{"WiFi", "merge"}},
		"empty name":          {Exe: "a.exe", Balance: []string{"WiFi", " "}},
		"neither":             {Exe: "a.exe"},
	} {
		if _, err := NewSet([]Rule{r}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestBadFailoverListsAreRejected(t *testing.T) {
	for name, fo := range map[string][]string{
		"empty entry":        {""},
		"merge in failover":  {"WiFi", "merge"},
		"the primary again":  {"wifi"},
		"the same one twice": {"Ethernet", "ethernet"},
	} {
		if _, err := NewSet([]Rule{{Exe: "a.exe", Adapter: "WiFi", Failover: fo}}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestEditedRulesApplyAndBadEditsKeepTheOldOnes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rules.json")
	os.WriteFile(p, []byte(`{"rules":[{"exe":"a.exe","adapter":"One"}]}`), 0o644)
	var logged []string
	r, err := Open(p, func(f string, a ...any) { logged = append(logged, f) })
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := r.set.Load().Match("a.exe"); m.Adapter != "One" {
		t.Fatalf("got %+v", m)
	}

	os.WriteFile(p, []byte(`{"rules":[{"exe":"a.exe","adapter":"Two"}]}`), 0o644)
	waitFor(t, func() bool { m, _ := r.set.Load().Match("a.exe"); return m.Adapter == "Two" })

	os.WriteFile(p, []byte(`{not json`), 0o644)
	time.Sleep(1500 * time.Millisecond)
	if m, _ := r.set.Load().Match("a.exe"); m.Adapter != "Two" {
		t.Fatalf("a broken edit replaced the working rules: %+v", m)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 40; i++ {
		if ok() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("timed out")
}

// bare drops the per-app meter, so a Route can be compared with what the rule alone says.
func bare(rt engine.Route) engine.Route {
	rt.Meter = nil
	return rt
}

func TestSaveAndReloadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "rules.json")
	if got, err := LoadRules(p); err != nil || len(got) != 0 {
		t.Fatalf("a missing file is an empty list, got %v (err %v)", got, err)
	}
	rules := []Rule{
		{Exe: "brave.exe", Adapter: "Ethernet 4", Failover: []string{"WiFi"}},
		{Exe: `C:\Games\x.exe`, Balance: []string{"WiFi", "Ethernet 4"}, OnDown: "merge"},
	}
	if err := SaveRules(p, rules); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRules(p)
	if err != nil || !reflect.DeepEqual(got, rules) {
		t.Fatalf("got %+v (err %v)", got, err)
	}
	if err := SaveRules(p, []Rule{{Exe: "a.exe"}}); err == nil {
		t.Fatal("an invalid rule must not be written")
	}
	if got, _ := LoadRules(p); !reflect.DeepEqual(got, rules) {
		t.Fatal("a refused save must leave the old file alone")
	}
}

func TestMatchIndexFindsTheRule(t *testing.T) {
	s := mustSet(t, Rule{Exe: "a.exe", Adapter: "One", Disabled: true}, Rule{Exe: "a.exe", Adapter: "Two"}, Rule{Exe: "b.exe", Adapter: "Three"})
	if i, ok := s.MatchIndex(`C:\x\A.exe`); !ok || i != 1 {
		t.Fatalf("got %d ok=%v, want rule 1", i, ok)
	}
	if _, ok := s.MatchIndex("c.exe"); ok {
		t.Fatal("no rule for c.exe")
	}
}

func TestDescribe(t *testing.T) {
	for want, r := range map[string]Rule{
		"WiFi":                        {Adapter: "WiFi"},
		"Ethernet 4, backup WiFi":     {Adapter: "Ethernet 4", Failover: []string{"WiFi"}},
		"WiFi + Ethernet 4, together": {Balance: []string{"WiFi", "Ethernet 4"}},
		"any adapter":                 {Adapter: Merge},
	} {
		if got := r.Describe(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestResolveFailoverUsesTheDefaultAdapter(t *testing.T) {
	for _, c := range []struct {
		name         string
		list         []string
		adapter, def string
		want         []string
	}{
		{"the default token becomes the default adapter", []string{"default"}, "Ethernet 4", "WiFi", []string{"WiFi"}},
		{"order is kept", []string{"Tether", "default", "Cable"}, "Ethernet 4", "WiFi", []string{"Tether", "WiFi", "Cable"}},
		{"no default set: nothing to fall back to", []string{"default"}, "Ethernet 4", "", nil},
		{"an app already on the default has no further fallback", []string{"default"}, "WiFi", "WiFi", nil},
		{"a repeat is dropped", []string{"WiFi", "default"}, "Ethernet 4", "wifi", []string{"WiFi"}},
		{"nothing listed", nil, "Ethernet 4", "WiFi", nil},
	} {
		if got := ResolveFailover(c.list, c.adapter, c.def); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRouteFailsOverToTheDefaultAdapter(t *testing.T) {
	r := newTestRouter(t, map[uint16]string{1: "a.exe", 2: "b.exe"},
		Rule{Exe: "a.exe", Adapter: "Tether", Failover: []string{"default"}},
		Rule{Exe: "b.exe", Adapter: "Tether"}, // no failover asked for: strict
	)
	r.set.Store(mustSetFile(t, File{DefaultAdapter: "WiFi", Rules: []Rule{
		{Exe: "a.exe", Adapter: "Tether", Failover: []string{"default"}},
		{Exe: "b.exe", Adapter: "Tether"},
	}}))
	if rt := bare(r.Route(flow(1))); !reflect.DeepEqual(rt, engine.Route{Adapter: "Tether", Failover: []string{"WiFi"}, OnDown: engine.Block}) {
		t.Errorf("a: %+v", rt)
	}
	if rt := bare(r.Route(flow(2))); !reflect.DeepEqual(rt, engine.Route{Adapter: "Tether", OnDown: engine.Block}) {
		t.Errorf("an app that did not ask for the default stays strict: %+v", rt)
	}
}

func mustSetFile(t *testing.T, f File) *Set {
	t.Helper()
	s, err := newSet(f)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDefaultAdapterPersistsAndSurvivesRuleEdits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rules.json")
	if err := SaveFile(p, File{DefaultAdapter: "WiFi", Rules: []Rule{{Exe: "a.exe", Adapter: "Tether", Failover: []string{"default"}}}}); err != nil {
		t.Fatal(err)
	}
	set, err := Load(p)
	if err != nil || set.Default != "WiFi" || set.Len() != 1 {
		t.Fatalf("default after reload: %+v (err %v)", set, err)
	}
	// Editing the rules alone must not lose the default.
	if err := SaveRules(p, []Rule{{Exe: "b.exe", Adapter: "Tether"}}); err != nil {
		t.Fatal(err)
	}
	if f, _ := LoadFile(p); f.DefaultAdapter != "WiFi" || len(f.Rules) != 1 || f.Rules[0].Exe != "b.exe" {
		t.Fatalf("after SaveRules: %+v", f)
	}
	// Clearing it removes the setting.
	f, _ := LoadFile(p)
	f.DefaultAdapter = ""
	SaveFile(p, f)
	if got, _ := LoadFile(p); got.DefaultAdapter != "" {
		t.Fatalf("cleared default is still %q", got.DefaultAdapter)
	}
	if err := SaveFile(p, File{DefaultAdapter: "merge"}); err == nil {
		t.Fatal(`"merge" is not an adapter`)
	}
}

func TestDefaultTokenIsAcceptedInFailoverOnce(t *testing.T) {
	if _, err := NewSet([]Rule{{Exe: "a.exe", Adapter: "Tether", Failover: []string{"default", "WiFi"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSet([]Rule{{Exe: "a.exe", Adapter: "Tether", Failover: []string{"default", "Default"}}}); err == nil {
		t.Fatal("listing the default twice is a mistake")
	}
	if got := (Rule{Adapter: "Tether", Failover: []string{"default"}}).Describe(); got != "Tether, backup default adapter" {
		t.Fatalf("describe: %q", got)
	}
}

func TestFailoverToDefaultUsesTheAutomaticOneWhenNoneIsChosen(t *testing.T) {
	r := newTestRouter(t, map[uint16]string{1: "a.exe", 2: "b.exe"},
		Rule{Exe: "a.exe", Adapter: "Ethernet 4", Failover: []string{DefaultToken}},
		Rule{Exe: "b.exe", Adapter: "Ethernet 4"}) // strict: no failover at all
	asked := 0
	r.Auto = func() string { asked++; return "WiFi" }

	rt := bare(r.Route(flow(1)))
	if !reflect.DeepEqual(rt.Failover, []string{"WiFi"}) {
		t.Fatalf("failover = %v, want the automatic default", rt.Failover)
	}
	if rt := bare(r.Route(flow(2))); len(rt.Failover) != 0 {
		t.Fatalf("a strict rule must stay strict: %v", rt.Failover)
	}
	if asked != 1 {
		t.Fatalf("the automatic default was asked for %d times; only the rule that falls back to it needs it", asked)
	}

	// A default the user chose wins, and the automatic one is not even asked.
	set, _ := newSet(File{DefaultAdapter: "Ethernet", Rules: []Rule{{Exe: "a.exe", Adapter: "Ethernet 4", Failover: []string{DefaultToken}}}})
	r.set.Store(set)
	asked = 0
	if rt := bare(r.Route(flow(1))); !reflect.DeepEqual(rt.Failover, []string{"Ethernet"}) || asked != 0 {
		t.Fatalf("chosen default: failover %v, asked %d", rt.Failover, asked)
	}
}

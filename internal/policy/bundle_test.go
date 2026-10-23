package policy

import "testing"

func TestMacAppRuleMatchesItsHelpers(t *testing.T) {
	s := mustSet(t,
		Rule{Exe: "Google Chrome.app", Adapter: "WiFi"},
		Rule{Exe: "/Applications/Safari.app", Adapter: "Ethernet"},
		Rule{Exe: "curl", Adapter: "WiFi"},
	)
	for exe, want := range map[string]string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome":                                                                         "WiFi",
		"/Applications/Google Chrome.app/Contents/Frameworks/G.framework/Helpers/Google Chrome Helper.app/Contents/MacOS/Google Chrome Helper": "WiFi",
		"/Users/me/Apps/google chrome.APP/Contents/MacOS/Google Chrome":                                                                        "WiFi",
		"/Applications/Safari.app/Contents/MacOS/Safari":                                                                                       "Ethernet",
		"/Users/me/Safari.app/Contents/MacOS/Safari":                                                                                           "", // a full path is that exact app
		"/usr/bin/curl": "WiFi",
		"/Applications/Other.app/Contents/MacOS/Other": "",
	} {
		got, _ := s.Match(exe)
		if got.Adapter != want {
			t.Errorf("%q -> %q, want %q", exe, got.Adapter, want)
		}
	}
}

//go:build nogui

package main

import (
	"strings"
	"testing"
)

// The command line has no license and nothing to buy, so it offers none of the commands that go with them.
func TestTheCommandLineOffersNoLicenseCommands(t *testing.T) {
	for _, cmd := range []string{"license", "machine-code", "limit"} {
		if proCommand(cmd, nil) {
			t.Fatalf("%q must not exist in the command line", cmd)
		}
		if strings.Contains(usage, "routengine "+cmd) {
			t.Fatalf("the usage text must not list %q", cmd)
		}
	}
	if startLicense() != "" {
		t.Fatal("nothing can block Start: there is no license to tamper with")
	}
}

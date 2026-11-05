package platform

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The passwordless rule names only the root-owned copy, with exactly the two launches the app makes, and sudo accepts it.
func TestTheStartupRuleIsNarrowAndValid(t *testing.T) {
	rule := startupRuleText("ada")
	want := "ada ALL=(root) NOPASSWD: " + startupExe + " app, " + startupExe + " app -minimized\n"
	if !strings.HasSuffix(rule, want) {
		t.Fatalf("rule:\n%s", rule)
	}
	if strings.Contains(rule, "/Applications") || strings.Contains(rule, "*") {
		t.Fatal("the rule must name only the copy the user cannot change, and no wildcard")
	}
	f, err := os.CreateTemp(t.TempDir(), "rule")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(rule)
	f.Close()
	if out, err := exec.Command("/usr/sbin/visudo", "-cf", f.Name()).CombinedOutput(); err != nil {
		t.Fatalf("visudo refuses the rule: %v %s", err, out)
	}
	if err := writeStartupRule("ev il"); err == nil {
		t.Fatal("an account name that would change the rule must be refused")
	}
}

// Only the launches the rule allows are tried without a password; any other flag goes through the password prompt.
func TestOnlyTheAllowedLaunchesSkipThePassword(t *testing.T) {
	for _, args := range [][]string{{"app", "-log", "/etc/x"}, {"app", "-start"}, {"run"}, {"app", "-minimized", "x"}, nil} {
		if err := startupRelaunch(args); err == nil || !strings.Contains(err.Error(), "not a launch the rule allows") {
			t.Errorf("%v: %v", args, err)
		}
	}
	// Allowed arguments, but no copy set up here: it says so instead of running anything.
	if err := startupRelaunch([]string{"app"}); err == nil {
		t.Fatal("without the copy and the rule it must not start anything")
	}
}

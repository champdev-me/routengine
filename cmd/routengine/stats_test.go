package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"routengine/internal/telemetry"
)

func TestStatsCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	run := func(arg string) (string, error) {
		var out bytes.Buffer
		err := statsCommand(path, []string{arg}, &out)
		return out.String(), err
	}

	if out, _ := run("status"); !strings.Contains(out, "not asked") {
		t.Fatalf("fresh: %q", out)
	}
	if out, err := run("on"); err != nil || !strings.Contains(out, "ON") || !telemetry.Open(path).Enabled() {
		t.Fatalf("on: %q %v", out, err)
	}
	// An unticked installer box must not undo an answer the user already gave.
	if _, err := run("off-if-new"); err != nil || !telemetry.Open(path).Enabled() {
		t.Fatalf("off-if-new changed an existing answer: %v", err)
	}
	if out, err := run("off"); err != nil || !strings.Contains(out, "OFF") || telemetry.Open(path).Enabled() {
		t.Fatalf("off: %q %v", out, err)
	}

	fresh := filepath.Join(t.TempDir(), "telemetry.json")
	if err := statsCommand(fresh, []string{"off-if-new"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if st := telemetry.Open(fresh); !st.Asked() || st.Enabled() {
		t.Fatal("a first install with the box unticked counts as an answer of no")
	}

	if _, err := run("maybe"); err == nil {
		t.Fatal("an unknown word must be an error")
	}
}

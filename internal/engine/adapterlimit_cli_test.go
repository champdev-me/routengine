//go:build nogui

package engine

import (
	"bytes"
	"testing"
)

// The command line has no speed limit per adapter: a cap set on the scheduler holds nothing back.
func TestTheCommandLineHoldsNothingBackPerAdapter(t *testing.T) {
	sl := &Slot{}
	sl.setLimit(Both(1))
	if sl.limitSpeed().Limits() || sl.limit.Load() != nil {
		t.Fatal("no cap is installed")
	}
	var out bytes.Buffer
	p := bytes.Repeat([]byte{1}, 1<<20)
	if n, err := sl.writePaced(&out, p, true); n != len(p) || err != nil || out.Len() != len(p) {
		t.Fatalf("a write goes straight through: %d %v", n, err)
	}
	m := &AppMeter{}
	m.SetAdapterLimits(map[string]Speed{"WiFi": Both(1)})
	if m.AdapterLimit("WiFi").Limits() {
		t.Fatal("and an app has no cap of its own per adapter")
	}
}

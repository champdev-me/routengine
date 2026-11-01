package engine

import (
	"encoding/json"
	"testing"
)

// A rules file written before 1.2.1 holds one number per adapter: it still reads, as the same cap both ways, and a
// cap that is the same both ways is still written as one number, so an older RoutEngine can read the file.
func TestASpeedCapReadsAndWritesBothForms(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Speed
		out  string
	}{
		{`50`, Both(50), `50`},
		{`7.5`, Both(7.5), `7.5`},
		{`{"down": 50, "up": 10}`, Speed{Down: 50, Up: 10}, `{"down":50,"up":10}`},
		{`{"down": 20}`, Speed{Down: 20}, `{"down":20}`},
		{`{"up": 3}`, Speed{Up: 3}, `{"up":3}`},
		{`{"down": 5, "up": 5}`, Both(5), `5`},
		{`0`, Speed{}, `0`},
	} {
		var s Speed
		if err := json.Unmarshal([]byte(c.in), &s); err != nil || s != c.want {
			t.Fatalf("%s read as %+v (%v), want %+v", c.in, s, err, c.want)
		}
		if b, err := json.Marshal(s); err != nil || string(b) != c.out {
			t.Fatalf("%+v written as %s (%v), want %s", s, b, err, c.out)
		}
	}
	for _, bad := range []string{`"fast"`, `{"download": 50}`, `[50, 10]`, `{"down": "50"}`} {
		var s Speed
		if err := json.Unmarshal([]byte(bad), &s); err == nil {
			t.Fatalf("%s must be refused, read as %+v", bad, s)
		}
	}
}

func TestASpeedCapSaysWhatItLimits(t *testing.T) {
	for s, want := range map[Speed]string{
		Both(50):           "50 Mbit/s",
		{Down: 50, Up: 10}: "↓50 ↑10 Mbit/s",
		{Down: 50}:         "↓50 Mbit/s",
		{Up: 2.5}:          "↑2.5 Mbit/s",
		{}:                 "unlimited",
	} {
		if got := s.String(); got != want {
			t.Fatalf("%+v says %q, want %q", s, got, want)
		}
	}
	if err := (Speed{Down: -1}).Check(); err == nil {
		t.Fatal("a negative speed must be refused")
	}
	if err := (Speed{Up: LimitMaxMbps + 1}).Check(); err == nil {
		t.Fatal("a speed above the maximum must be refused")
	}
	if err := (Speed{Down: 10}).Check(); err != nil {
		t.Fatal(err)
	}
}

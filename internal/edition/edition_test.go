package edition

import "testing"

func TestTheCommandLineHasEveryFreeFeatureOnAnyNumberOfApps(t *testing.T) {
	f := CommandLine
	if !f.UnlimitedApps || !f.SpeedLimit || !f.DataCap {
		t.Fatalf("the command line limits and caps every app, with no app limit: %+v", f)
	}
	for _, slot := range []int{-1, 0, 2, 50} {
		g := f.ForSlots(slot, slot)
		if !g.SpeedLimit || !g.DataCap {
			t.Fatalf("slot %d: no app loses its limit or cap: %+v", slot, g)
		}
	}
}

func TestSlotsGiveTheFirstAppsTheirLimitsWhenTheFeatureIsOff(t *testing.T) {
	free := Features{SpeedLimitApps: FreeApps, CapApps: FreeApps}
	if g := free.ForSlots(0, 2); !g.SpeedLimit || !g.DataCap {
		t.Fatalf("within the first places: %+v", g)
	}
	if g := free.ForSlots(FreeApps, -1); g.SpeedLimit || g.DataCap {
		t.Fatalf("past the places, or holding none: %+v", g)
	}
}

func TestTheSourceCanBeReplacedByALicense(t *testing.T) {
	defer SetSource(func() Features { return CommandLine })
	SetSource(func() Features { return Features{SpeedLimitApps: 1} })
	if Allowed().UnlimitedApps {
		t.Fatal("Allowed must ask the source that was set")
	}
}

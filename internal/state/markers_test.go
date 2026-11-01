package state

import "testing"

// An installer asks RoutEngine to quit and then ends it by force. If it was ended before it read the "quit",
// the marker stays, and the copy the installer starts next must not read it and quit at once.
func TestALeftoverQuitDoesNotStopTheNextInstance(t *testing.T) {
	dir := t.TempDir()
	for _, m := range []string{"quit", "off", "on", "open"} {
		if err := SendMarker(dir, m); err != nil {
			t.Fatal(err)
		}
	}
	ClearMarkers(dir)
	for _, m := range []string{"quit", "off", "on", "open"} {
		if TakeMarker(dir, m) {
			t.Fatalf("%q was left for the next instance", m)
		}
	}
	ClearMarkers(t.TempDir()) // nothing to clear, no ctl folder: no harm

	if err := SendMarker(dir, "quit"); err != nil || !TakeMarker(dir, "quit") {
		t.Fatalf("a marker sent after the start still arrives: %v", err)
	}
}

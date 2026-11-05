package procmap

import (
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// The lookup once read only the first eighth of the processes (proc_listallpids counts processes, not bytes), so an app
// started long before RoutEngine, such as a browser, was never found and its speed limit never applied.
func TestEveryProcessIsListed(t *testing.T) {
	all := pids()
	if !slices.Contains(all, os.Getpid()) || !slices.Contains(all, 1) { // 1 is launchd: the oldest process there is
		t.Fatalf("the list of %d processes misses this one (%d) or launchd", len(all), os.Getpid())
	}
	if len(all) < 50 {
		t.Fatalf("only %d processes listed", len(all))
	}
}

func TestRunningListsThisProgramOnce(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	self, _ = filepath.EvalSymlinks(self) // /var is /private/var here, and the system reports the second
	n := 0
	for _, p := range Running() {
		if p == self {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("this test program (%s) is listed %d times, want once", self, n)
	}
}

// A new flow starts a search of every process, so a miss must stay cheap enough for a browser opening dozens at once.
func TestAMissIsFastEnough(t *testing.T) {
	start := time.Now()
	const tries = 20
	for range tries {
		Lookup(true, netip.MustParseAddrPort("127.0.0.1:1"))
	}
	per := time.Since(start) / tries
	t.Logf("a search of all processes takes %v", per)
	if per > 150*time.Millisecond {
		t.Fatalf("a search takes %v", per)
	}
}

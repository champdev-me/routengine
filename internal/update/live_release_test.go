//go:build live

package update

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"routengine/internal/endpoints"
)

// TestLiveRelease runs the program's own update check against the published release: the manifest must verify against
// the public key compiled into the program, and the installer it names must download with a matching checksum. It is
// step 2 of docs/releasing-windows.md. It needs the network, so it only builds with -tags live, and
// scripts\verify-release.ps1 runs it with ROUTENGINE_LIVE_VERSION set to the version that was just published.
func TestLiveRelease(t *testing.T) {
	want := os.Getenv("ROUTENGINE_LIVE_VERSION")
	if want == "" {
		t.Skip("set ROUTENGINE_LIVE_VERSION to the version that was just published")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	hc := &http.Client{Timeout: 3 * time.Minute}

	// The server counts an update check as an install by its user agent. "release-check" is not a version, so this
	// request is counted with the browsers and scripts ("other"), not as an install of any release.
	m, err := fetch(ctx, hc, endpoints.Update, "release-check")
	if err != nil {
		t.Fatalf("the manifest does not verify against the key compiled into the program: %v", err)
	}
	if m.Version != want {
		t.Fatalf("the published manifest says %s, expected %s", m.Version, want)
	}
	if !Newer("0.0.1", m.Version) {
		t.Fatalf("%s is not read as a newer version than 0.0.1", m.Version)
	}
	t.Logf("manifest verified: version %s", m.Version)

	p, err := download(ctx, hc, m, t.TempDir())
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	t.Logf("installer downloaded and its checksum matched the manifest: %s", p)
}

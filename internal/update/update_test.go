package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		cur, cand string
		want      bool
	}{
		{"0.8.0", "0.9.0", true},
		{"0.9.0", "0.9.0", false},
		{"0.9.1", "0.9.0", false},
		{"0.9.9", "0.10.0", true}, // numbers, not text
		{"1.0.0", "0.99.99", false},
		{"v0.8.0", "0.8.1", true},
		{"dev", "9.9.9", false}, // a development build is never updated
		{"0.8.0", "latest", false},
		{"0.8.0", "", false},
	} {
		if got := Newer(c.cur, c.cand); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cur, c.cand, got, c.want)
		}
	}
}

// withKey makes the program trust a key the test holds.
func withKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	p, priv, _ := ed25519.GenerateKey(rand.Reader)
	old := pub
	pub = p
	t.Cleanup(func() { pub = old })
	return priv
}

func installerBytes() ([]byte, string) {
	b := []byte("MZ pretend installer")
	s := sha256.Sum256(b)
	return b, hex.EncodeToString(s[:])
}

func TestManifestIsOnlyTrustedWhenSignedAndOnTheSite(t *testing.T) {
	priv := withKey(t)
	_, sum := installerBytes()
	good := Sign(priv, Manifest{Version: "0.9.0", URL: "https://routengine.net/download/RoutEngine-setup-0.9.0.exe", SHA256: sum})
	if err := good.verify(); err != nil {
		t.Fatalf("a signed manifest on the site must be accepted: %v", err)
	}
	for name, m := range map[string]Manifest{
		"changed version": func() Manifest { m := good; m.Version = "9.9.9"; return m }(),
		"changed url":     func() Manifest { m := good; m.URL = "https://routengine.net/download/evil.exe"; return m }(),
		"changed sha":     func() Manifest { m := good; m.SHA256 = hex.EncodeToString(make([]byte, 32)); return m }(),
		"no signature":    func() Manifest { m := good; m.Sig = ""; return m }(),
		"another key":     Sign(func() ed25519.PrivateKey { _, k, _ := ed25519.GenerateKey(rand.Reader); return k }(), Manifest{Version: "0.9.0", URL: good.URL, SHA256: sum}),
		"plain http":      Sign(priv, Manifest{Version: "0.9.0", URL: "http://routengine.net/x.exe", SHA256: sum}),
		"another site":    Sign(priv, Manifest{Version: "0.9.0", URL: "https://routengine.net.evil.com/x.exe", SHA256: sum}),
		"look-alike site": Sign(priv, Manifest{Version: "0.9.0", URL: "https://notroutengine.net/x.exe", SHA256: sum}),
		"bad version":     Sign(priv, Manifest{Version: "latest", URL: good.URL, SHA256: sum}),
		"bad checksum":    Sign(priv, Manifest{Version: "0.9.0", URL: good.URL, SHA256: "abc"}),
	} {
		if err := m.verify(); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
	if err := Sign(priv, Manifest{Version: "0.9.0", URL: "https://downloads.routengine.net/x.exe", SHA256: sum}).verify(); err != nil {
		t.Errorf("a subdomain of the site is fine: %v", err)
	}
}

// release serves a manifest and its installer, and lets the test trust the plain-http test address.
func release(t *testing.T, version string, corrupt bool) (*httptest.Server, *Updater) {
	t.Helper()
	priv := withKey(t)
	oldAllowed := allowedURL
	allowedURL = func(string) bool { return true }
	t.Cleanup(func() { allowedURL = oldAllowed })

	body, sum := installerBytes()
	if corrupt {
		body = []byte("something else")
	}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/latest.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Sign(priv, Manifest{Version: version, URL: srv.URL + "/setup.exe", SHA256: sum, Notes: "faster"}))
	})
	mux.HandleFunc("/setup.exe", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &Updater{Current: "0.8.0", URL: srv.URL + "/latest.json", Dir: t.TempDir(), HC: srv.Client()}
}

func TestANewerReleaseIsDownloadedAndChecked(t *testing.T) {
	_, u := release(t, "0.9.0", false)
	if err := u.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	i := u.Info()
	if i.Version != "0.9.0" || !i.Downloaded || i.Notes != "faster" || i.Err != "" {
		t.Fatalf("info = %+v", i)
	}
	b, err := os.ReadFile(u.path)
	if err != nil || string(b) != "MZ pretend installer" {
		t.Fatalf("the installer on disk: %q %v", b, err)
	}
}

func TestSameOrOlderReleaseIsIgnored(t *testing.T) {
	_, u := release(t, "0.8.0", false)
	if err := u.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if i := u.Info(); i.Version != "" || i.Downloaded {
		t.Fatalf("info = %+v", i)
	}
}

func TestAnInstallerThatDoesNotMatchItsChecksumIsNeverKept(t *testing.T) {
	_, u := release(t, "0.9.0", true)
	if err := u.CheckNow(context.Background()); err == nil {
		t.Fatal("a corrupted or swapped installer must be an error")
	}
	if i := u.Info(); i.Downloaded || i.Err == "" {
		t.Fatalf("info = %+v", i)
	}
	if err := u.Apply(); err == nil {
		t.Fatal("there is nothing safe to install")
	}
	if ents, _ := os.ReadDir(u.Dir); len(ents) != 0 {
		t.Fatalf("a bad download must not stay on disk: %v", ents)
	}
}

func TestApplyStartsTheInstallerAndThenQuits(t *testing.T) {
	_, u := release(t, "0.9.0", false)
	var launched string
	var quit atomic.Bool
	u.Launch = func(p string) error { launched = p; return nil }
	u.Quit = func() { quit.Store(true) }
	if err := u.Apply(); err == nil {
		t.Fatal("nothing downloaded yet")
	}
	u.CheckNow(context.Background())
	if err := u.Apply(); err != nil || launched != u.path {
		t.Fatalf("apply: %v launched %q", err, launched)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !quit.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !quit.Load() {
		t.Fatal("the program must quit after starting the installer")
	}
}

func TestAutomaticInstallWaitsForPermissionAndForRoutingToBeOff(t *testing.T) {
	_, u := release(t, "0.9.0", false)
	var launches atomic.Int32
	var auto, idle atomic.Bool
	u.Launch = func(string) error { launches.Add(1); return nil }
	u.Auto, u.Idle = auto.Load, idle.Load
	u.First, u.Poll, u.Every = time.Millisecond, 5*time.Millisecond, time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go u.Run(ctx)

	waitFor := func(cond func() bool) bool {
		for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
			if cond() {
				return true
			}
		}
		return false
	}
	if !waitFor(func() bool { return u.Info().Downloaded }) {
		t.Fatal("the update was never downloaded")
	}
	time.Sleep(60 * time.Millisecond)
	if launches.Load() != 0 {
		t.Fatal("must not install while the user has not allowed it")
	}
	auto.Store(true)
	time.Sleep(60 * time.Millisecond)
	if launches.Load() != 0 {
		t.Fatal("must not install while routing is on")
	}
	idle.Store(true)
	if !waitFor(func() bool { return launches.Load() == 1 }) {
		t.Fatal("must install once allowed and idle")
	}
	time.Sleep(60 * time.Millisecond)
	if launches.Load() != 1 {
		t.Fatalf("installed %d times", launches.Load())
	}
}

func TestAVersionTheLicenseDoesNotCoverIsAnnouncedButNeverFetched(t *testing.T) {
	_, u := release(t, "2.0.0", false)
	u.Current = "1.4.0"
	u.Covered = func(v string) bool { return v[0] == '1' } // a license for 1.x
	if err := u.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	i := u.Info()
	if i.Version != "2.0.0" || !i.NeedsUpgrade || i.Downloaded {
		t.Fatalf("info = %+v", i)
	}
	if ents, _ := os.ReadDir(u.Dir); len(ents) != 0 {
		t.Fatalf("nothing may be downloaded for a version that is not covered: %v", ents)
	}
	if err := u.Apply(); err == nil {
		t.Fatal("there is nothing to install")
	}
	u.Covered = nil // the license was upgraded
	if err := u.CheckNow(context.Background()); err != nil || !u.Info().Downloaded || u.Info().NeedsUpgrade {
		t.Fatalf("after the upgrade: %+v %v", u.Info(), err)
	}
}

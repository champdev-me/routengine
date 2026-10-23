// Package update finds, downloads and installs new releases. The program asks one fixed address
// (internal/endpoints) for a small signed file that names the newest installer. Nothing is installed
// unless the file's signature checks against the release key built into the program and the downloaded
// installer matches the checksum the file gives, so a hijacked web host cannot push a program of its own.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Manifest is the file served at the update address: the newest version and its installer.
type Manifest struct {
	Version string `json:"version"`         // such as "0.9.0"
	URL     string `json:"url"`             // the installer; https, on routengine.net
	SHA256  string `json:"sha256"`          // of the installer, in hex
	Notes   string `json:"notes,omitempty"` // what is new, in a sentence or two
	Sig     string `json:"sig"`             // the release key's signature over the three fields above, base64url
}

var pub = mustKey(PublicKeyHex)

func mustKey(h string) ed25519.PublicKey {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("update: the embedded public key is malformed")
	}
	return b
}

func signedText(version, url, sum string) []byte {
	return []byte("routengine-update\n" + version + "\n" + url + "\n" + sum)
}

// Sign makes the signature of a manifest; it is what `licensegen release` does.
func Sign(priv ed25519.PrivateKey, m Manifest) Manifest {
	m.Sig = base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, signedText(m.Version, m.URL, m.SHA256)))
	return m
}

// allowedURL says whether an installer may be fetched from u: https, on routengine.net or a subdomain.
// Tests replace it.
var allowedURL = func(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" {
		return false
	}
	h := strings.ToLower(p.Hostname())
	return h == "routengine.net" || strings.HasSuffix(h, ".routengine.net")
}

var errUntrusted = errors.New("the update file could not be trusted")

// verify checks that the manifest was signed by the release key and is well formed.
func (m Manifest) verify() error {
	sig, err := base64.RawURLEncoding.DecodeString(m.Sig)
	if err != nil || !ed25519.Verify(pub, signedText(m.Version, m.URL, m.SHA256), sig) {
		return errUntrusted
	}
	if _, ok := parseVersion(m.Version); !ok || !allowedURL(m.URL) {
		return errUntrusted
	}
	if b, err := hex.DecodeString(m.SHA256); err != nil || len(b) != sha256.Size {
		return errUntrusted
	}
	return nil
}

func parseVersion(s string) (v [3]int, ok bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// Newer reports whether candidate is a later version than current. A version that is not like 1.2.3
// (a development build, say) is never newer and is never updated.
func Newer(current, candidate string) bool {
	c, ok1 := parseVersion(current)
	n, ok2 := parseVersion(candidate)
	if !ok1 || !ok2 {
		return false
	}
	for i := range c {
		if n[i] != c[i] {
			return n[i] > c[i]
		}
	}
	return false
}

// userAgent is what the update check says about itself: the version, system and CPU, such as
// "RoutEngine/1.1.1 (linux; arm64)". The server counts installs from these requests, without an ID.
func userAgent(app string) string {
	return "RoutEngine/" + app + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
}

// fetch reads and verifies the manifest at addr.
func fetch(ctx context.Context, hc *http.Client, addr, app string) (Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return Manifest{}, err
	}
	req.Header.Set("User-Agent", userAgent(app))
	resp, err := hc.Do(req)
	if err != nil {
		return Manifest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("the update address answered %s", resp.Status)
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&m); err != nil {
		return Manifest{}, errUntrusted
	}
	return m, m.verify()
}

const maxInstaller = 300 << 20

// download fetches the installer named by m into dir and returns its path, after checking its checksum.
func download(ctx context.Context, hc *http.Client, m Manifest, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	final := filepath.Join(dir, "RoutEngine-setup-"+m.Version+".exe")
	if sum, err := fileSum(final); err == nil && sum == strings.ToLower(m.SHA256) {
		return final, nil // downloaded before
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the download answered %s", resp.Status)
	}
	part := final + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxInstaller+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxInstaller {
		err = errors.New("the download is larger than any installer should be")
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != strings.ToLower(m.SHA256) {
		err = errors.New("the downloaded installer does not match its checksum")
	}
	if err != nil {
		os.Remove(part)
		return "", err
	}
	os.Remove(final)
	return final, os.Rename(part, final)
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Info is what the window shows about updates.
type Info struct {
	Version      string // a newer version, "" if there is none
	Notes        string
	Downloaded   bool // its installer is on this PC and checked
	NeedsUpgrade bool // the installed license does not cover it, so it is not downloaded
	Err          string
	Checked      time.Time
}

// Updater keeps the program up to date. Set the fields, then run Run in its own goroutine.
type Updater struct {
	Current string                    // the running version
	URL     string                    // where the manifest is (internal/endpoints.Update)
	Dir     string                    // where installers are downloaded to
	HC      *http.Client              // nil: one with a generous timeout
	Auto    func() bool               // may an update be installed without asking?
	Idle    func() bool               // is this a good moment, that is, is routing off?
	Covered func(version string) bool // does the installed license cover that version? nil means yes
	Quit    func()                    // leave the program once the installer has started
	Changed func()                    // what the window shows changed
	Launch  func(installer string) error

	// Timings; the defaults are for real use.
	First, Every, Poll time.Duration

	mu   sync.Mutex
	info Info
	path string
	done bool
}

func (u *Updater) hc() *http.Client {
	if u.HC != nil {
		return u.HC
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (u *Updater) changed() {
	if u.Changed != nil {
		u.Changed()
	}
}

// Info is the current state.
func (u *Updater) Info() Info {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.info
}

// CheckNow looks for a newer version, and downloads it if there is one.
func (u *Updater) CheckNow(ctx context.Context) error {
	m, err := fetch(ctx, u.hc(), u.URL, u.Current)
	u.mu.Lock()
	u.info.Checked = time.Now()
	if err != nil {
		u.info.Err = err.Error()
		u.mu.Unlock()
		u.changed()
		return err
	}
	u.info.Err = ""
	if !Newer(u.Current, m.Version) {
		u.info.Version, u.info.Notes, u.info.Downloaded, u.info.NeedsUpgrade, u.path = "", "", false, false, ""
		u.mu.Unlock()
		u.changed()
		return nil
	}
	u.info.Version, u.info.Notes = m.Version, m.Notes
	u.info.Downloaded = false
	if u.Covered != nil && !u.Covered(m.Version) {
		// A new major version the license does not cover: say so, and neither download nor install it.
		u.info.NeedsUpgrade, u.path = true, ""
		u.mu.Unlock()
		u.changed()
		return nil
	}
	u.info.NeedsUpgrade = false
	u.mu.Unlock()
	u.changed()

	path, err := download(ctx, u.hc(), m, u.Dir)
	u.mu.Lock()
	if err != nil {
		u.info.Err = err.Error()
	} else {
		u.path, u.info.Downloaded = path, true
	}
	u.mu.Unlock()
	u.changed()
	return err
}

// Apply starts the downloaded installer, which replaces the program and starts it again, and then quits.
func (u *Updater) Apply() error {
	u.mu.Lock()
	path, ok := u.path, u.info.Downloaded
	if u.done {
		u.mu.Unlock()
		return nil
	}
	u.mu.Unlock()
	if !ok {
		return errors.New("no update has been downloaded yet")
	}
	launch := u.Launch
	if launch == nil {
		launch = launchInstaller
	}
	if err := launch(path); err != nil {
		u.mu.Lock()
		u.info.Err = err.Error()
		u.mu.Unlock()
		u.changed()
		return err
	}
	u.mu.Lock()
	u.done = true
	u.mu.Unlock()
	if u.Quit != nil {
		go func() {
			time.Sleep(time.Second) // the installer closes the program too; this makes it a clean exit
			u.Quit()
		}()
	}
	return nil
}

// Run checks for updates a little after start and then about once a day, and installs one by itself when
// the user allows it and nothing is being routed. It returns when ctx ends.
func (u *Updater) Run(ctx context.Context) {
	first, every, poll := u.First, u.Every, u.Poll
	if first == 0 {
		first = 45 * time.Second
	}
	if every == 0 {
		every = 24 * time.Hour
	}
	if poll == 0 {
		poll = 5 * time.Minute
	}
	wait := first
	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = poll
		if time.Since(last) >= every {
			last = time.Now()
			u.CheckNow(ctx)
		}
		if u.Info().Downloaded && u.Auto != nil && u.Auto() && (u.Idle == nil || u.Idle()) {
			u.Apply()
		}
	}
}

// IsRelease reports whether v is a released version such as "0.9.0", as opposed to a development build.
func IsRelease(v string) bool {
	_, ok := parseVersion(v)
	return ok
}

// Package license decides which features are unlocked. A license is a short text key signed with a
// private key that only the seller holds; the program carries the matching public key and checks the
// signature offline, so there is no account and no server.
//
// A key is bound to one or more PCs by their machine codes (see MachineCode), so a key copied to
// another computer does nothing. A bulk license lists many codes. The checks belong in the engine, not
// the window: the window only shows what the engine allows.
package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"routengine/internal/confdir"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// FreeApps is how many apps the free edition can have in its list.
const FreeApps = 3

// The free edition may give a speed limit to FreeSpeedLimitApps apps and a data cap to FreeCapApps apps, at one time.
// They are separate allowances: the first apps in the list that use a speed limit hold the first, and the first
// that use a cap hold the second (the same app can hold both). Both equal FreeApps: every free app can have both.
const (
	FreeSpeedLimitApps = FreeApps
	FreeCapApps        = FreeApps
)

// MaxMachines is the most PCs one key can list. Larger customers get several keys.
const MaxMachines = 50

// Features is what an edition unlocks.
type Features struct {
	DNS            bool // choose the DNS server an app uses
	SpeedLimit     bool // cap an app's download and upload speed
	DataCap        bool // stop an app after a data allowance
	Fastest        bool // let an app use whichever adapter answers fastest
	MonthlyHistory bool // data used this month, not only today
	UnlimitedApps  bool // more than FreeApps apps in the list
	SpeedLimitApps int  // how many apps may have a speed limit even where SpeedLimit is off
	CapApps        int  // how many apps may have a data cap even where DataCap is off
}

// FreeFeatures is the free edition: routing, failover, daily data used, FreeApps apps, a speed limit on
// FreeSpeedLimitApps of them and a data cap on FreeCapApps.
var FreeFeatures = Features{SpeedLimitApps: FreeSpeedLimitApps, CapApps: FreeCapApps}

// ProFeatures is everything.
var ProFeatures = Features{DNS: true, SpeedLimit: true, DataCap: true, Fastest: true, MonthlyHistory: true, UnlimitedApps: true}

// License is a verified license.
type License struct {
	ID       string
	Name     string
	Email    string
	Plan     string // "pro"
	Machines []string
	Issued   time.Time
	Expires  time.Time // zero: never
	MaxMajor int       // the newest major version it covers (1 means 1.x and earlier); 0: every version
	Features Features
}

// Free is what runs when there is no valid license.
func Free() License { return License{Plan: "free", Features: FreeFeatures} }

// Paid reports whether this is more than the free edition.
func (l License) Paid() bool { return l.Plan != "" && l.Plan != "free" }

// Seats is how many PCs the license covers; 0 means it is not tied to any (only a seller's test key is).
func (l License) Seats() int { return len(l.Machines) }

type payload struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Email string   `json:"email,omitempty"`
	Plan  string   `json:"plan"`
	Iat   int64    `json:"iat"`
	Exp   int64    `json:"exp,omitempty"`
	M     []string `json:"m,omitempty"`  // machine codes this license is valid on
	MV    int      `json:"mv,omitempty"` // the newest major version it covers; 0: every version
}

const prefix = "RE1."

var (
	// ErrBadKey is a key that is not a RoutEngine license, or has been altered.
	ErrBadKey = errors.New("this is not a valid RoutEngine license key")
	// ErrExpired is a genuine license whose time is up.
	ErrExpired = errors.New("this license has expired")
	// ErrWrongMachine is a genuine license issued for other PCs.
	ErrWrongMachine = errors.New("this license was issued for a different PC")
)

func featuresFor(plan string) (Features, bool) {
	switch plan {
	case "pro":
		return ProFeatures, true
	}
	return Features{}, false
}

// Sign makes a license key valid on the given machines (none: any PC, which only a seller's own test
// key should be). It is used by the seller's tool, never by the program.
func Sign(priv ed25519.PrivateKey, name, email, plan string, machines []string, issued, expires time.Time) (string, error) {
	return SignLimited(priv, name, email, plan, machines, issued, expires, 0)
}

// SignLimited is Sign for a key that covers only versions up to a major version: 1 is a "lifetime for 1.x"
// license, which keeps working with every 1.x release and stops at 2.0. 0 means every version.
func SignLimited(priv ed25519.PrivateKey, name, email, plan string, machines []string, issued, expires time.Time, maxMajor int) (string, error) {
	if maxMajor < 0 {
		return "", fmt.Errorf("the newest major version cannot be negative")
	}
	if _, ok := featuresFor(plan); !ok {
		return "", fmt.Errorf("unknown plan %q", plan)
	}
	if len(machines) > MaxMachines {
		return "", fmt.Errorf("one key lists at most %d PCs; issue several keys", MaxMachines)
	}
	p := payload{ID: randomID(), Name: name, Email: email, Plan: plan, Iat: issued.Unix(), MV: maxMajor}
	seen := map[string]bool{}
	for _, m := range machines {
		c := NormalizeCode(m)
		if len(c) != 14 {
			return "", fmt.Errorf("%q is not a machine code such as 7QK4-M2XD-9PTA", m)
		}
		if !seen[c] {
			seen[c] = true
			p.M = append(p.M, c)
		}
	}
	if !expires.IsZero() {
		p.Exp = expires.Unix()
	}
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	return prefix + enc.EncodeToString(body) + "." + enc.EncodeToString(ed25519.Sign(priv, body)), nil
}

// Verify checks key against pub, the time now and this PC's machine code.
func Verify(pub ed25519.PublicKey, key string, now time.Time, machine string) (License, error) {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, prefix) {
		return License{}, ErrBadKey
	}
	parts := strings.Split(strings.TrimPrefix(key, prefix), ".")
	if len(parts) != 2 {
		return License{}, ErrBadKey
	}
	enc := base64.RawURLEncoding
	body, err1 := enc.DecodeString(parts[0])
	sig, err2 := enc.DecodeString(parts[1])
	if err1 != nil || err2 != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, body, sig) {
		return License{}, ErrBadKey
	}
	var p payload
	if json.Unmarshal(body, &p) != nil {
		return License{}, ErrBadKey
	}
	feats, ok := featuresFor(p.Plan)
	if !ok {
		return License{}, ErrBadKey
	}
	l := License{ID: p.ID, Name: p.Name, Email: p.Email, Plan: p.Plan, Machines: p.M, Issued: time.Unix(p.Iat, 0), MaxMajor: p.MV, Features: feats}
	if len(p.M) > 0 {
		here := NormalizeCode(machine)
		found := false
		for _, m := range p.M {
			if m == here {
				found = true
			}
		}
		if !found {
			return l, ErrWrongMachine
		}
	}
	if p.Exp != 0 {
		l.Expires = time.Unix(p.Exp, 0)
		if !now.Before(l.Expires) {
			return l, ErrExpired
		}
	}
	return l, nil
}

// ---------------------------------------------------------------- what the program holds

// recheckEvery is how often the key is verified again while the program runs. The result of an
// earlier check is never trusted for long: patching it in memory only helps until the next one.
const recheckEvery = 30 * time.Second

type held struct {
	l   License
	raw string    // the key, so it can be verified again
	at  time.Time // when it last was
}

var (
	pub     = mustKey(PublicKeyHex)
	cur     atomic.Pointer[held]
	machine = sync.OnceValue(MachineCode)
)

func mustKey(h string) ed25519.PublicKey {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("license: the embedded public key is malformed")
	}
	return b
}

// Current is the license in force now: the installed one while it is valid, otherwise Free.
func Current() License {
	h := cur.Load()
	if h == nil {
		return Free()
	}
	if h.raw != "" && time.Since(h.at) > recheckEvery {
		l, err := Verify(pub, h.raw, time.Now(), machine())
		if err != nil {
			cur.Store(nil)
			return Free()
		}
		h = &held{l: l, raw: h.raw, at: time.Now()}
		cur.Store(h)
	}
	if !h.l.Expires.IsZero() && !time.Now().Before(h.l.Expires) {
		return Free()
	}
	if !covers(h.l, int(appMajor.Load())) {
		return Free() // a license for 1.x does not unlock 2.0
	}
	if denialNow() != "" {
		return Free() // the licensing server has not confirmed this license (see online.go)
	}
	return h.l
}

// Allowed is shorthand for Current().Features.
func Allowed() Features { return Current().Features }

// DefaultPath is where the key is kept: next to the rules, in the user's config folder.
func DefaultPath() string {
	return filepath.Join(confdir.Dir(), "license.key")
}

// Load reads the key file and makes it the current license. A missing file is the free edition.
// A bad, expired or foreign key also leaves the free edition, and the error says why.
func Load(path string) (License, error) {
	clearFlag(flagKeyEdited) // looked at afresh each time
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cur.Store(nil)
		lease.Store(nil)
		return Free(), nil
	}
	if err != nil {
		cur.Store(nil)
		lease.Store(nil)
		return Free(), err
	}
	if strings.TrimSpace(string(b)) == "" { // nothing in it: the same as no key, not a change
		cur.Store(nil)
		lease.Store(nil)
		return Free(), nil
	}
	l, err := set(string(b))
	if err == nil {
		attachLease(path, l)
	} else if errors.Is(err, ErrBadKey) {
		markTamper(TamperKeyEdited, "") // a saved key that no longer verifies was changed or damaged
	}
	return l, err
}

// Install checks key, keeps it in the file at path and makes it the current license.
func Install(path, key string) (License, error) {
	l, err := set(key)
	if err != nil {
		return Free(), err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return l, err
	}
	tmp := path + ".tmp" // whole or not at all: a crash half way must not leave a key that looks edited
	if err := os.WriteFile(tmp, []byte(strings.TrimSpace(key)+"\n"), 0o600); err != nil {
		return l, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return l, err
	}
	clearFlag(flagKeyEdited) // a key that verifies puts a damaged one right
	forgetReported()
	attachLease(path, l)
	RequestCheck() // a new key is verified at once, not tomorrow
	return l, nil
}

// Remove forgets the license and returns to the free edition.
func Remove(path string) error {
	tamperFlags.Store(0) // no license, nothing left to have been changed
	forgetReported()
	cur.Store(nil)
	lease.Store(nil)
	os.Remove(leaseFileFor(path))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func set(key string) (License, error) {
	key = strings.TrimSpace(key)
	l, err := Verify(pub, key, time.Now(), machine())
	if err != nil {
		cur.Store(nil)
		lease.Store(nil)
		return Free(), err
	}
	cur.Store(&held{l: l, raw: key, at: time.Now()})
	return l, nil
}

// SetForTest replaces the current license and returns a function that puts the old one back.
func SetForTest(l *License) (restore func()) {
	old := cur.Load()
	if l == nil {
		cur.Store(nil)
	} else {
		cur.Store(&held{l: *l})
	}
	return func() { cur.Store(old) }
}

func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------- versions

var appMajor atomic.Int32

// majorOf is the major version of "1.2.3" (or "v1.2.3"); 0 for anything else, such as a development build.
func majorOf(v string) int {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return 0
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// SetAppVersion tells the license which version of RoutEngine is running, so a key that covers only up to a
// major version can be held to it. A development build is never limited.
func SetAppVersion(v string) { appMajor.Store(int32(majorOf(v))) }

// covers says whether l works with a program of this major version. 0 is "unknown" and is never refused.
func covers(l License, major int) bool { return l.MaxMajor == 0 || major == 0 || major <= l.MaxMajor }

// CoversVersion reports whether the installed license would unlock version v. With no Pro license there is
// nothing to withhold, so the answer is yes.
func CoversVersion(v string) bool {
	h := cur.Load()
	return h == nil || !h.l.Paid() || covers(h.l, majorOf(v))
}

// versionDenial is why the installed license does not cover the running version, or "".
func versionDenial() string {
	h := cur.Load()
	if h == nil || covers(h.l, int(appMajor.Load())) {
		return ""
	}
	return fmt.Sprintf("This license covers RoutEngine up to version %d.x, and this is version %d. Upgrade to keep Pro.", h.l.MaxMajor, appMajor.Load())
}

// ForSlots is f for an app that holds place speed among the apps with a speed limit and place cap among the apps with a
// data cap (0 is the first; -1 is none). An edition that grants a feature outright is unchanged; the free edition
// grants a speed limit to its first SpeedLimitApps places and a data cap to its first CapApps places.
func (f Features) ForSlots(speed, cap int) Features {
	if speed >= 0 && speed < f.SpeedLimitApps {
		f.SpeedLimit = true
	}
	if cap >= 0 && cap < f.CapApps {
		f.DataCap = true
	}
	return f
}

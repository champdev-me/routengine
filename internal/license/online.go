package license

// Online verification. A Pro license is checked against the licensing server when the program starts and
// about once a day after that. Pro turns on only after the server has said "ok" once (so a key that was made
// by anyone else never gets a free first period), and it pauses when the last "ok" is older than LeaseLength,
// a week. This is a condition of the Pro license (license agreement, section 6.3) and the program has no
// setting to turn it off. The free edition sends nothing.
//
// The program sends a Request: the license key, this PC's machine code, the program version, the SHA-256
// of the program's own file and a random value. The server answers with an Envelope holding a signed
// Verdict that repeats the random value, so an old answer cannot be replayed and a fake server without the
// signing key cannot say "ok". What the program does with a verdict:
//
//	ok        Pro is on, for LeaseLength from the verdict
//	revoked   back to the free edition (the license was withdrawn, for example after a refund)
//	invalid   back to the free edition (the key is not valid for this PC)
//	tampered  back to the free edition (the program file is not one the Licensor released)
//
// If the server cannot be reached nothing changes while the last good verdict is younger than LeaseLength;
// after that Pro waits until a check succeeds. Before the first verdict Pro is off. A clock set back to stretch
// a lease is noticed too (see clockSetBack). SERVER.md describes the exchange for whoever runs the server.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"routengine/internal/endpoints"
)

// What the server may say.
const (
	StatusOK       = "ok"
	StatusRevoked  = "revoked"
	StatusInvalid  = "invalid"
	StatusTampered = "tampered"
)

const (
	// LeaseLength is how long an "ok" verdict keeps Pro on without hearing from the server again: the program
	// must reach the server at least once a week. It checks every day, so a week offline is what it takes.
	LeaseLength = 7 * 24 * time.Hour

	checkEvery = 24 * time.Hour
	retryAfter = time.Hour

	// clockSlack is how far the clock may jump back (a time sync, a wrong battery clock) before it is taken for
	// an attempt to stretch a lease.
	clockSlack = 2 * time.Hour
)

// Request is exactly what the program sends to the licensing server.
type Request struct {
	V       int    `json:"v"`
	Key     string `json:"key"`     // the license key
	Machine string `json:"machine"` // this PC's machine code
	App     string `json:"app"`     // the program version
	Exe     string `json:"exe"`     // SHA-256 of the program file, to spot a modified copy
	Nonce   string `json:"nonce"`   // random; the verdict must repeat it
}

// Verdict is the server's decision.
type Verdict struct {
	Status  string `json:"status"`
	License string `json:"license"` // the key's ID
	Machine string `json:"machine"`
	Nonce   string `json:"nonce"`
	Issued  int64  `json:"issued"`          // unix seconds, when the server made it
	Until   int64  `json:"until,omitempty"` // unix seconds, when an "ok" runs out; 0 means Issued + LeaseLength
	Message string `json:"message,omitempty"`
}

// Envelope is what travels: the verdict's JSON and the server's signature over exactly those bytes, both
// base64url, so nothing can change in between.
type Envelope struct {
	Verdict string `json:"verdict"`
	Sig     string `json:"sig"`
}

// SignVerdict is what the server does to make an answer.
func SignVerdict(priv ed25519.PrivateKey, v Verdict) (Envelope, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return Envelope{}, err
	}
	enc := base64.RawURLEncoding
	return Envelope{Verdict: enc.EncodeToString(body), Sig: enc.EncodeToString(ed25519.Sign(priv, body))}, nil
}

var (
	serverPub = mustKey(ServerPublicKeyHex)

	// Logf, if set, is told when a check could not be made.
	Logf = func(format string, a ...any) {}

	// ErrAnswer is a reply that is not a verdict this program can trust.
	ErrAnswer = errors.New("the licensing server's answer could not be trusted")
)

func openVerdict(env Envelope) (Verdict, error) {
	enc := base64.RawURLEncoding
	body, err1 := enc.DecodeString(env.Verdict)
	sig, err2 := enc.DecodeString(env.Sig)
	if err1 != nil || err2 != nil || !ed25519.Verify(serverPub, body, sig) {
		return Verdict{}, ErrAnswer
	}
	var v Verdict
	if json.Unmarshal(body, &v) != nil {
		return Verdict{}, ErrAnswer
	}
	return v, nil
}

// ---------------------------------------------------------------- what the program remembers

// leaseState is what is known about the license's standing. It is replaced as a whole, never edited.
type leaseState struct {
	checked time.Time // when the last verdict arrived
	verdict *Verdict  // the last verified one, nil before the first
	env     *Envelope // the same, as received, to keep in the file
}

type leaseFile struct {
	KeyID   string    `json:"key_id"`
	Checked time.Time `json:"checked,omitempty"`
	Seen    time.Time `json:"seen,omitempty"` // the latest time the program noticed, to catch a clock set back
	Verdict *Envelope `json:"verdict,omitempty"`
}

var (
	lease     atomic.Pointer[leaseState]
	leasePath atomic.Value // string

	// seen is the latest moment this program has noticed, in unix nanoseconds, kept across restarts in the lease
	// file. A lease is measured against the clock, so a clock set back would stretch it; now < seen says it was.
	seen atomic.Int64
	// checkFailed is whether the last attempt to reach the server failed, which decides what the window says
	// while a license has no verdict yet.
	checkFailed atomic.Bool
)

// noteTime records that it is now.
func noteTime(now time.Time) {
	for {
		old := seen.Load()
		if now.UnixNano() <= old || seen.CompareAndSwap(old, now.UnixNano()) {
			return
		}
	}
}

// clockSetBack is whether the clock is behind a moment the program has already seen.
func clockSetBack(now time.Time) bool {
	s := seen.Load()
	return s != 0 && now.UnixNano() < s-int64(clockSlack)
}

func leaseFileFor(keyPath string) string { return filepath.Join(filepath.Dir(keyPath), "lease.json") }

// attachLease starts tracking l's standing, from the lease file if it belongs to the same license. The
// file's verdict is checked again here, so editing the file cannot make a license look better.
func attachLease(keyPath string, l License) {
	clearFlag(flagLeaseEdited) // looked at afresh each time
	path := leaseFileFor(keyPath)
	leasePath.Store(path)
	st := &leaseState{}
	var lf leaseFile
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &lf) == nil && lf.KeyID == l.ID {
		if !lf.Seen.IsZero() {
			noteTime(lf.Seen)
		}
		st.checked = lf.Checked
		if lf.Verdict != nil {
			v, err := openVerdict(*lf.Verdict)
			switch {
			case err != nil:
				markTamper(TamperLeaseEdited, l.ID) // not signed by the server: the file was edited
			case v.License == l.ID && NormalizeCode(v.Machine) == NormalizeCode(machine()):
				st.verdict, st.env = &v, lf.Verdict
				// A genuine answer for another license or PC (a disk image on new hardware, say) is just not used.
			}
		}
	}
	lease.Store(st)
	saveLease(l.ID, st)
}

func saveLease(keyID string, st *leaseState) {
	path, _ := leasePath.Load().(string)
	if path == "" {
		return
	}
	var sn time.Time
	if n := seen.Load(); n != 0 {
		sn = time.Unix(0, n)
	}
	b, err := json.MarshalIndent(leaseFile{KeyID: keyID, Checked: st.checked, Seen: sn, Verdict: st.env}, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0o600) == nil {
			os.Rename(tmp, path)
		}
	}
}

func dropLease() {
	lease.Store(nil)
	if path, _ := leasePath.Load().(string); path != "" {
		os.Remove(path)
	}
}

// denialAt is why the license in hand may not be used at time now, or "" if it may.
func denialAt(now time.Time) string {
	st := lease.Load()
	if st == nil {
		return ""
	}
	v := st.verdict
	if v == nil { // Pro waits for the server's first "ok"
		if checkFailed.Load() {
			return "RoutEngine could not reach the licensing server. Connect to the internet to turn on Pro."
		}
		return "Checking this license online to turn on Pro…"
	}
	switch v.Status {
	case StatusOK:
		if now.After(leaseEnd(v)) {
			return "RoutEngine could not verify this license online since " + time.Unix(v.Issued, 0).Format("2 January 2006") + ". Connect to the internet so it can check again."
		}
		return ""
	case StatusRevoked:
		return "This license has been revoked."
	case StatusInvalid:
		return "The licensing server says this license is not valid for this PC."
	case StatusTampered:
		return msgModified
	}
	return "The licensing server did not accept this license."
}

// leaseEnd is when an "ok" verdict runs out.
func leaseEnd(v *Verdict) time.Time {
	if v.Until > 0 {
		return time.Unix(v.Until, 0)
	}
	return time.Unix(v.Issued, 0).Add(LeaseLength)
}

// denialNow is denialAt for the real clock, which must also not have been set back since the program last
// looked: a lease is only as good as the clock it is measured with.
func denialNow() string {
	now := time.Now()
	why := denialAt(now)
	if why == "" && lease.Load() != nil && clockSetBack(now) {
		why = msgClockSetBack
		reportTamper(TamperClockSetBack, currentID())
	}
	noteTime(now)
	return why
}

// Denial is why an installed license is not in force, or "" if it is (or there is none). The window shows it.
func Denial() string {
	if cur.Load() == nil {
		return ""
	}
	if why := versionDenial(); why != "" {
		return why
	}
	return denialNow()
}

// Installed is whether a license key is held, in force or not.
func Installed() bool {
	h := cur.Load()
	return h != nil && h.raw != ""
}

// LeaseLeft is how long Pro would go on working if the licensing server could not be reached from now on:
// the time until the last good verdict runs out. ok is false when no good verdict is in force.
func LeaseLeft() (left time.Duration, ok bool) {
	st := lease.Load()
	if st == nil || st.verdict == nil || st.verdict.Status != StatusOK {
		return 0, false
	}
	left = time.Until(leaseEnd(st.verdict))
	return left, left > 0
}

// VerifiedAt is when the licensing server last said yes to the installed license, or the zero time.
func VerifiedAt() time.Time {
	st := lease.Load()
	if st == nil || st.verdict == nil || st.verdict.Status != StatusOK {
		return time.Time{}
	}
	return time.Unix(st.verdict.Issued, 0)
}

// ---------------------------------------------------------------- the check

// exeHash is the SHA-256 of the running program file, worked out once.
var exeHash = sync.OnceValue(func() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
})

// CheckOnce asks the licensing server at url about the installed license and records the answer. For the
// free edition there is nothing to ask and nothing is sent. An error means no usable answer arrived
// (no connection, an error page, a reply that is not signed): that never takes Pro away by itself.
func CheckOnce(ctx context.Context, hc *http.Client, url, app string) error {
	err := checkOnce(ctx, hc, url, app)
	checkFailed.Store(err != nil)
	return err
}

func checkOnce(ctx context.Context, hc *http.Client, url, app string) error {
	h := cur.Load()
	if h == nil || h.raw == "" {
		return nil
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	req := Request{V: 1, Key: h.raw, Machine: machine(), App: app, Exe: exeHash(), Nonce: hex.EncodeToString(nonce[:])}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("User-Agent", "RoutEngine/"+app)
	resp, err := hc.Do(hr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the licensing server answered %s", resp.Status)
	}
	var env Envelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&env); err != nil {
		return ErrAnswer
	}
	v, err := openVerdict(env)
	if err != nil {
		return err
	}
	if v.Nonce != req.Nonce || v.License != h.l.ID || NormalizeCode(v.Machine) != NormalizeCode(req.Machine) {
		return ErrAnswer // an old answer, or one about another license or PC
	}
	now := time.Now()
	seen.Store(now.UnixNano()) // the server has just confirmed what time it is: a set-back clock is forgiven
	clearFlag(flagLeaseEdited) // and it has made a fresh answer to keep, which replaces an edited one
	st := &leaseState{checked: now, verdict: &v, env: &env}
	lease.Store(st)
	saveLease(h.l.ID, st)
	return nil
}

var recheck = make(chan struct{}, 1)

// RequestCheck asks the background checker to verify now, for a key that was just entered.
func RequestCheck() {
	select {
	case recheck <- struct{}{}:
	default:
	}
}

// RunChecks verifies the installed license when the program starts and about once a day after, and
// retries within the hour when the server cannot be reached. changed is called after every attempt.
// It returns when ctx ends.
func RunChecks(ctx context.Context, app string, changed func()) {
	hc := &http.Client{Timeout: 20 * time.Second}
	wait := 20 * time.Second // a moment after start, so the check never slows starting
	if Denial() != "" {
		wait = 2 * time.Second // Pro is waiting for this check: do not make it wait longer
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-recheck:
		}
		if err := CheckOnce(ctx, hc, endpoints.Check, app); err != nil {
			Logf("license: could not verify online: %v\n", err)
			wait = retryAfter
		} else {
			wait = checkEvery
		}
		if err := SendTamper(ctx, hc, endpoints.Tamper, app); err != nil { // after the check, which is what clears a block
			Logf("license: could not send a report: %v\n", err)
		}
		if h, st := cur.Load(), lease.Load(); h != nil && st != nil {
			noteTime(time.Now())
			saveLease(h.l.ID, st) // keeps the latest time seen on disk, hour by hour while the server cannot be reached
		}
		if changed != nil {
			changed()
		}
	}
}

// SetServerKeyForTest replaces the key the server's answers are checked against.
func SetServerKeyForTest(k ed25519.PublicKey) (restore func()) {
	old := serverPub
	serverPub = k
	return func() { serverPub = old }
}

// InstallVerdict stores a signed verdict next to the key file at keyPath, as if the server had just
// sent it. The seller's tool uses it to give a test PC a lease before a server exists.
func InstallVerdict(keyPath string, l License, env Envelope) error {
	if _, err := openVerdict(env); err != nil {
		return err
	}
	b, err := json.MarshalIndent(leaseFile{KeyID: l.ID, Checked: time.Now(), Seen: time.Now(), Verdict: &env}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(leaseFileFor(keyPath), b, 0o600)
}

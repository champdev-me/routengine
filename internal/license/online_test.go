package license

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// serverWith starts a fake licensing server that signs whatever status the test wants, and points the
// program at its public key. It echoes the nonce unless told not to.
type fakeServer struct {
	*httptest.Server
	priv     ed25519.PrivateKey
	status   string
	badNonce bool
	got      []Request
	raw      []map[string]any
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	pubK, priv, _ := ed25519.GenerateKey(rand.Reader)
	restore := SetServerKeyForTest(pubK)
	t.Cleanup(restore)
	f := &fakeServer{priv: priv, status: StatusOK}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		var raw map[string]any
		var b strings.Builder
		buf := make([]byte, 8192)
		n, _ := r.Body.Read(buf)
		b.Write(buf[:n])
		json.Unmarshal([]byte(b.String()), &req)
		json.Unmarshal([]byte(b.String()), &raw)
		f.got, f.raw = append(f.got, req), append(f.raw, raw)
		nonce := req.Nonce
		if f.badNonce {
			nonce = "an old one"
		}
		env, _ := SignVerdict(f.priv, Verdict{Status: f.status, License: "abc123", Machine: req.Machine, Nonce: nonce, Issued: time.Now().Unix()})
		json.NewEncoder(w).Encode(env)
	}))
	t.Cleanup(f.Close)
	return f
}

// holdPro makes a Pro license the one in hand, without going through a signed key.
func holdPro(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "license.key")
	l := License{ID: "abc123", Name: "Ada", Plan: "pro", Features: ProFeatures}
	cur.Store(&held{l: l, raw: "RE1.test.key", at: time.Now()})
	checkFailed.Store(false)
	resetTamper()
	attachLease(keyPath, l)
	t.Cleanup(func() { cur.Store(nil); lease.Store(nil); checkFailed.Store(false); seen.Store(0); resetTamper() })
	return keyPath
}

func TestGoodVerdictKeepsProAndIsRemembered(t *testing.T) {
	srv := newFakeServer(t)
	keyPath := holdPro(t)
	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0"); err != nil {
		t.Fatal(err)
	}
	if !Current().Paid() || Denial() != "" || VerifiedAt().IsZero() {
		t.Fatalf("paid=%v denial=%q verified=%v", Current().Paid(), Denial(), VerifiedAt())
	}
	// A restart reads the verdict from the file and checks its signature again.
	lease.Store(nil)
	attachLease(keyPath, License{ID: "abc123", Plan: "pro"})
	if VerifiedAt().IsZero() {
		t.Fatal("the verdict must survive a restart")
	}
}

func TestRequestHoldsOnlyWhatTheAgreementSays(t *testing.T) {
	srv := newFakeServer(t)
	holdPro(t)
	CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0")
	if len(srv.raw) != 1 {
		t.Fatalf("%d requests", len(srv.raw))
	}
	allowed := map[string]bool{"v": true, "key": true, "machine": true, "app": true, "exe": true, "nonce": true}
	for k := range srv.raw[0] {
		if !allowed[k] {
			t.Errorf("the request carries %q, which section 6.3 of the agreement does not list", k)
		}
	}
	r := srv.got[0]
	if r.Key != "RE1.test.key" || r.App != "0.9.0" || len(r.Nonce) != 32 || r.Machine != machine() || len(r.Exe) != 64 {
		t.Errorf("request = %+v", r)
	}
}

func TestRevokedTamperedAndInvalidWithdrawPro(t *testing.T) {
	for status, words := range map[string]string{StatusRevoked: "revoked", StatusTampered: "modified", StatusInvalid: "not valid"} {
		srv := newFakeServer(t)
		holdPro(t)
		srv.status = status
		if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0"); err != nil {
			t.Fatal(err)
		}
		if Current().Paid() {
			t.Errorf("%s: Pro must be withdrawn", status)
		}
		if d := Denial(); !strings.Contains(d, words) {
			t.Errorf("%s: denial %q should say %q", status, d, words)
		}
		srv.status = StatusOK // and a later good answer gives it back
		CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0")
		if !Current().Paid() {
			t.Errorf("%s: a later ok must restore Pro", status)
		}
	}
}

func TestAnswersThatCannotBeTrustedChangeNothing(t *testing.T) {
	srv := newFakeServer(t)
	holdPro(t)
	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0"); err != nil {
		t.Fatal(err)
	}
	at := VerifiedAt()

	srv.badNonce = true // an old "ok" replayed
	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0"); err == nil {
		t.Fatal("a replayed answer must be refused")
	}
	srv.badNonce = false

	// A different server (a hosts-file trick) cannot sign for the real one.
	other := newFakeServer(t)
	restore := SetServerKeyForTest(mustKey(ServerPublicKeyHex))
	defer restore()
	if err := CheckOnce(context.Background(), other.Client(), other.URL, "0.9.0"); err == nil {
		t.Fatal("an answer signed with another key must be refused")
	}

	// Nor can a plain error page.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", 500) }))
	defer bad.Close()
	if err := CheckOnce(context.Background(), bad.Client(), bad.URL, "0.9.0"); err == nil {
		t.Fatal("a 500 is not an answer")
	}
	if VerifiedAt() != at || !Current().Paid() {
		t.Fatal("none of those may change the standing: the earlier good verdict still holds")
	}
}

// A key nobody has confirmed gets no free first days: Pro turns on at the server's first "ok", not before.
func TestProWaitsForTheFirstOnlineOk(t *testing.T) {
	srv := newFakeServer(t)
	holdPro(t)
	if Current().Paid() || !strings.Contains(Denial(), "Checking this license online") {
		t.Fatalf("before any check Pro is off and says it is checking: denial %q", Denial())
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 503) }))
	defer bad.Close()
	if err := CheckOnce(context.Background(), bad.Client(), bad.URL, "0.9.0"); err == nil {
		t.Fatal("a 503 is not an answer")
	}
	if Current().Paid() || !strings.Contains(Denial(), "could not reach the licensing server") {
		t.Fatalf("offline at first use: Pro stays off and says to connect: denial %q", Denial())
	}

	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0"); err != nil {
		t.Fatal(err)
	}
	if !Current().Paid() || Denial() != "" {
		t.Fatalf("the first ok turns Pro on: paid=%v denial=%q", Current().Paid(), Denial())
	}
}

// Pro must reach the server at least once a week.
func TestALeaseLastsAWeek(t *testing.T) {
	if LeaseLength != 7*24*time.Hour {
		t.Fatalf("LeaseLength is %v: the license is verified every week", LeaseLength)
	}
	holdPro(t)
	now := time.Now()
	withVerdict := func(age time.Duration) {
		lease.Store(&leaseState{verdict: &Verdict{Status: StatusOK, License: "abc123", Issued: now.Add(-age).Unix()}})
	}

	withVerdict(6 * 24 * time.Hour)
	if !Current().Paid() {
		t.Fatal("6 days after a good verdict Pro still works")
	}
	if left, ok := LeaseLeft(); !ok || left < 23*time.Hour || left > 25*time.Hour {
		t.Fatalf("a day is left of the week: %v %v", left, ok)
	}
	withVerdict(8 * 24 * time.Hour)
	if Current().Paid() || !strings.Contains(Denial(), "could not verify") {
		t.Fatalf("8 days after the last verdict Pro must wait, denial %q", Denial())
	}
	if _, ok := LeaseLeft(); ok {
		t.Fatal("no lease is left after it has run out")
	}

	// A server that names its own end time is believed.
	lease.Store(&leaseState{verdict: &Verdict{Status: StatusOK, License: "abc123", Issued: now.Unix(), Until: now.Add(-time.Hour).Unix()}})
	if Current().Paid() {
		t.Fatal("an explicit until in the past ends the lease")
	}
}

// Setting the clock back would stretch a lease, so it is noticed, and one online check puts it right.
func TestAClockSetBackDoesNotStretchTheLease(t *testing.T) {
	srv := newFakeServer(t)
	keyPath := holdPro(t)
	t.Cleanup(func() { seen.Store(0) })
	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0"); err != nil {
		t.Fatal(err)
	}
	if !Current().Paid() {
		t.Fatal("Pro is on after the check")
	}

	seen.Store(time.Now().Add(time.Hour).UnixNano()) // an hour ahead of the clock: within the slack
	if !Current().Paid() {
		t.Fatal("a small step back (a time sync) must not cost Pro")
	}
	seen.Store(time.Now().Add(3 * time.Hour).UnixNano())
	if Current().Paid() || !strings.Contains(Denial(), "clock") {
		t.Fatalf("a clock 3 hours behind what was seen: Pro waits, denial %q", Denial())
	}

	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0"); err != nil {
		t.Fatal(err)
	}
	if !Current().Paid() || Denial() != "" {
		t.Fatal("the server confirming the time puts it right")
	}

	// The latest time seen is in the lease file, so restarting does not forget it.
	seen.Store(0)
	attachLease(keyPath, License{ID: "abc123", Plan: "pro"})
	if seen.Load() == 0 {
		t.Fatal("the time seen must survive a restart")
	}
}

func TestAnotherLicensesLeaseIsNotInherited(t *testing.T) {
	srv := newFakeServer(t)
	keyPath := holdPro(t)
	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0"); err != nil {
		t.Fatal(err)
	}
	attachLease(keyPath, License{ID: "someone-else", Plan: "pro"}) // the lease file is for abc123
	if lease.Load().verdict != nil {
		t.Fatal("a different license must not inherit another's verdict")
	}
}

func TestEditingTheLeaseFileDoesNotHelp(t *testing.T) {
	srv := newFakeServer(t)
	keyPath := holdPro(t)
	srv.status = StatusRevoked
	CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0")

	// The user rewrites the saved verdict to say "ok": the signature no longer fits.
	path := leaseFileFor(keyPath)
	b, _ := os.ReadFile(path)
	var lf leaseFile
	json.Unmarshal(b, &lf)
	forged, _ := SignVerdict(ed25519.NewKeyFromSeed(make([]byte, 32)), Verdict{Status: StatusOK, License: "abc123", Machine: machine(), Issued: time.Now().Unix()})
	lf.Verdict = &forged
	b, _ = json.Marshal(lf)
	os.WriteFile(path, b, 0o600)

	lease.Store(nil)
	attachLease(keyPath, License{ID: "abc123", Plan: "pro"})
	if !VerifiedAt().IsZero() {
		t.Fatal("a verdict signed by anyone but the server must be ignored")
	}
}

func TestFreeEditionSendsNothing(t *testing.T) {
	srv := newFakeServer(t)
	cur.Store(nil)
	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "0.9.0"); err != nil {
		t.Fatal(err)
	}
	if len(srv.got) != 0 {
		t.Fatal("the free edition must not contact the licensing server")
	}
}

package license

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// resetTamper forgets what was found, queued and sent.
func resetTamper() {
	tamperMu.Lock()
	tamperSent, tamperQueue = map[string]bool{}, nil
	tamperMu.Unlock()
	tamperFlags.Store(0)
}

func queued() []waiting {
	tamperMu.Lock()
	defer tamperMu.Unlock()
	return append([]waiting(nil), tamperQueue...)
}

// editLease replaces the saved server answer with one that says "ok" but was not signed by the server.
func editLease(t *testing.T, keyPath string) {
	t.Helper()
	path := leaseFileFor(keyPath)
	b, _ := os.ReadFile(path)
	var lf leaseFile
	json.Unmarshal(b, &lf)
	forged, _ := SignVerdict(ed25519.NewKeyFromSeed(make([]byte, 32)), Verdict{Status: StatusOK, License: "abc123", Machine: machine(), Issued: time.Now().Unix()})
	lf.Verdict = &forged
	b, _ = json.Marshal(lf)
	os.WriteFile(path, b, 0o600)
}

func TestAnEditedLeaseFileIsReportedOnce(t *testing.T) {
	srv := newFakeServer(t)
	keyPath := holdPro(t)
	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if len(queued()) != 0 {
		t.Fatalf("an honest lease reports nothing: %+v", queued())
	}
	editLease(t, keyPath)
	lease.Store(nil)
	attachLease(keyPath, License{ID: "abc123", Plan: "pro"})
	attachLease(keyPath, License{ID: "abc123", Plan: "pro"}) // a second look must not queue a second report
	if q := queued(); len(q) != 1 || q[0].kind != TamperLeaseEdited || q[0].license != "abc123" {
		t.Fatalf("queued = %+v", q)
	}
}

// A genuine answer that belongs to another PC (a disk image on new hardware) is not a change somebody made.
func TestAGenuineAnswerForAnotherPCIsNotTampering(t *testing.T) {
	srv := newFakeServer(t)
	keyPath := holdPro(t)
	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	path := leaseFileFor(keyPath)
	b, _ := os.ReadFile(path)
	var lf leaseFile
	json.Unmarshal(b, &lf)
	other, _ := SignVerdict(srv.priv, Verdict{Status: StatusOK, License: "abc123", Machine: "AAAA-BBBB-CCCC", Issued: time.Now().Unix()})
	lf.Verdict = &other // signed by the real server, for a different PC
	b, _ = json.Marshal(lf)
	os.WriteFile(path, b, 0o600)

	lease.Store(nil)
	attachLease(keyPath, License{ID: "abc123", Plan: "pro"})
	if len(queued()) != 0 || Tampered() != "" {
		t.Fatalf("nothing was tampered with: %+v %q", queued(), Tampered())
	}
	if !VerifiedAt().IsZero() {
		t.Fatal("but another PC's answer is not used")
	}
}

// A clock that is behind pauses Pro and is reported, but it does not block Start: it has many innocent causes.
func TestAClockSetBackIsReportedButDoesNotBlockStart(t *testing.T) {
	srv := newFakeServer(t)
	holdPro(t)
	if err := CheckOnce(context.Background(), srv.Client(), srv.URL, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	seen.Store(time.Now().Add(3 * time.Hour).UnixNano())
	if Current().Paid() {
		t.Fatal("a clock set back pauses Pro")
	}
	if q := queued(); len(q) != 1 || q[0].kind != TamperClockSetBack || q[0].license != "abc123" {
		t.Fatalf("queued = %+v", q)
	}
	if why := Tampered(); why != "" {
		t.Fatalf("but routing may still start: %q", why)
	}
}

func TestASavedKeyThatNoLongerVerifiesIsReported(t *testing.T) {
	resetTamper()
	t.Cleanup(func() { cur.Store(nil); lease.Store(nil); resetTamper() })
	dir := t.TempDir()

	if _, err := Load(filepath.Join(dir, "missing.key")); err != nil || len(queued()) != 0 {
		t.Fatalf("no key file is the free edition, and reports nothing: %v %+v", err, queued())
	}
	empty := filepath.Join(dir, "empty.key")
	os.WriteFile(empty, []byte(" \n"), 0o600) // what a crash half way through a write can leave
	if l, err := Load(empty); err != nil || l.Paid() || len(queued()) != 0 || Tampered() != "" {
		t.Fatalf("an empty file is no key, not a change: %v %+v %q", err, queued(), Tampered())
	}
	if _, err := Install(filepath.Join(dir, "typed.key"), "RE1.garbage.garbage"); err == nil || len(queued()) != 0 {
		t.Fatalf("a wrong key typed in is a mistake, not tampering: %v %+v", err, queued())
	}
	path := filepath.Join(dir, "license.key")
	os.WriteFile(path, []byte("RE1.garbage.garbage\n"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("the edited key must not load")
	}
	if q := queued(); len(q) != 1 || q[0].kind != TamperKeyEdited || q[0].license != "" {
		t.Fatalf("queued = %+v", q)
	}
}

func TestTheKeyIsWrittenWholeOrNotAtAll(t *testing.T) {
	pubT, priv := keys(t)
	oldPub, oldMachine := pub, machine
	pub, machine = pubT, func() string { return "7QK4-M2XD-9PTA" }
	t.Cleanup(func() { pub, machine = oldPub, oldMachine; cur.Store(nil); lease.Store(nil); resetTamper() })

	key, _ := Sign(priv, "Ada", "", "pro", []string{"7QK4-M2XD-9PTA"}, time.Now(), time.Time{})
	path := filepath.Join(t.TempDir(), "license.key")
	if _, err := Install(path, key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("no half-written file is left behind: %v", err)
	}
	if b, _ := os.ReadFile(path); strings.TrimSpace(string(b)) != key {
		t.Fatal("the key file holds the key")
	}
}

func TestTamperReportsAreSentOnceAndHoldOnlyWhatTheAgreementSays(t *testing.T) {
	resetTamper()
	t.Cleanup(resetTamper)
	var got []map[string]any
	status := 503
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status/100 != 2 {
			http.Error(w, "no", status)
			return
		}
		var m map[string]any
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &m)
		got = append(got, m)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	send := func() error { return SendTamper(context.Background(), srv.Client(), srv.URL, "1.0.0") }

	if err := send(); err != nil || len(got) != 0 {
		t.Fatalf("with nothing waiting no request is made: %v %d", err, len(got))
	}
	reportTamper(TamperClockSetBack, "abc123")
	reportTamper(TamperClockSetBack, "abc123") // the same kind in the same run is one report
	if err := send(); err == nil || len(queued()) != 1 {
		t.Fatalf("a server that is down keeps the report for later: %v %+v", err, queued())
	}
	status = 429
	if err := send(); err == nil || len(queued()) != 1 {
		t.Fatalf("being told to slow down keeps it too: %v %+v", err, queued())
	}
	status = 204
	if err := send(); err != nil || len(queued()) != 0 {
		t.Fatalf("sent: %v %+v", err, queued())
	}
	if err := send(); err != nil || len(got) != 1 {
		t.Fatalf("nothing is sent twice: %v, %d bodies", err, len(got))
	}
	allowed := map[string]bool{"v": true, "kind": true, "license": true, "machine": true, "app": true, "exe": true}
	for k := range got[0] {
		if !allowed[k] {
			t.Errorf("the report carries %q, which section 6.3 of the agreement does not list", k)
		}
	}
	if got[0]["kind"] != TamperClockSetBack || got[0]["license"] != "abc123" || got[0]["app"] != "1.0.0" || got[0]["v"] != float64(1) ||
		got[0]["machine"] != machine() || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got[0]["exe"].(string)) {
		t.Errorf("report = %v", got[0])
	}
}

// A report the server will never accept must not hold up the ones behind it.
func TestAReportTheServerRefusesForGoodIsDroppedAndTheNextOneGoes(t *testing.T) {
	resetTamper()
	t.Cleanup(resetTamper)
	var kinds []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &m)
		if m["kind"] == TamperLeaseEdited {
			http.Error(w, "bad request", 400)
			return
		}
		kinds = append(kinds, m["kind"].(string))
		w.WriteHeader(204)
	}))
	defer srv.Close()
	reportTamper(TamperLeaseEdited, "abc123")
	reportTamper(TamperKeyEdited, "")
	if err := SendTamper(context.Background(), srv.Client(), srv.URL, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if len(queued()) != 0 || len(kinds) != 1 || kinds[0] != TamperKeyEdited {
		t.Fatalf("the refused one is dropped and the other is sent: queued=%+v sent=%v", queued(), kinds)
	}
}

func TestTheFreeEditionHasNothingToReport(t *testing.T) {
	resetTamper()
	t.Cleanup(resetTamper)
	cur.Store(nil)
	lease.Store(nil)
	seen.Store(0)
	denialNow()
	if len(queued()) != 0 || Tampered() != "" {
		t.Fatalf("no license, no report: %+v", queued())
	}
}

// Routing must not start while a change is found, and each kind clears itself once it is put right.
func TestTamperedBlocksStartUntilItIsPutRight(t *testing.T) {
	if Tampered() != "" {
		t.Fatalf("the free edition has nothing to tamper with: %q", Tampered())
	}

	// A saved key that was changed: blocked until it is removed, or a good key replaces it.
	resetTamper()
	t.Cleanup(func() { cur.Store(nil); lease.Store(nil); resetTamper() })
	path := filepath.Join(t.TempDir(), "license.key")
	os.WriteFile(path, []byte("RE1.garbage.garbage\n"), 0o600)
	Load(path)
	if why := Tampered(); why != msgKeyEdited {
		t.Fatalf("an edited key blocks Start: %q", why)
	}
	tamperMu.Lock()
	tamperQueue = nil // as if the server had taken the report
	tamperMu.Unlock()
	if err := Remove(path); err != nil || Tampered() != "" {
		t.Fatalf("removing the license puts it right: %v %q", err, Tampered())
	}
	reportTamper(TamperKeyEdited, "") // a new problem after that is reportable again
	reportTamper(TamperKeyEdited, "") // but once only
	if len(queued()) != 1 {
		t.Fatalf("reportable again after the license was put right, and not twice: %+v", queued())
	}
	resetTamper()

	// The saved answer of the server was edited: blocked until the next successful check.
	srv := newFakeServer(t)
	keyPath := holdPro(t)
	ctx := context.Background()
	if err := CheckOnce(ctx, srv.Client(), srv.URL, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	editLease(t, keyPath)
	lease.Store(nil)
	attachLease(keyPath, License{ID: "abc123", Plan: "pro"})
	if why := Tampered(); why != msgLeaseEdited {
		t.Fatalf("an edited lease blocks Start: %q", why)
	}
	if err := CheckOnce(ctx, srv.Client(), srv.URL, "1.0.0"); err != nil || Tampered() != "" {
		t.Fatalf("a fresh answer from the server puts it right: %v %q", err, Tampered())
	}

	// The server says this copy of the program is not a release.
	srv.status = StatusTampered
	CheckOnce(ctx, srv.Client(), srv.URL, "1.0.0")
	if why := Tampered(); why != msgModified {
		t.Fatalf("a modified copy blocks Start: %q", why)
	}
	srv.status = StatusOK
	CheckOnce(ctx, srv.Client(), srv.URL, "1.0.0")
	if Tampered() != "" {
		t.Fatalf("a release is fine again: %q", Tampered())
	}
}

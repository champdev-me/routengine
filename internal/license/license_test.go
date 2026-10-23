package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	pcA = "7QK4-M2XD-9PTA"
	pcB = "ABCD-EFGH-2345"
)

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestASignedKeyVerifiesAndUnlocksEverything(t *testing.T) {
	pub, priv := keys(t)
	now := time.Now()
	key, err := Sign(priv, "Ada", "ada@example.com", "pro", []string{pcA}, now, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	l, err := Verify(pub, key, now, pcA)
	if err != nil || l.Name != "Ada" || l.Email != "ada@example.com" || !l.Paid() || l.Features != ProFeatures || l.Seats() != 1 {
		t.Fatalf("got %+v, %v", l, err)
	}
	if !l.Expires.IsZero() {
		t.Fatal("no expiry was asked for")
	}
}

func TestAKeyOnlyWorksOnThePCsItWasIssuedFor(t *testing.T) {
	pub, priv := keys(t)
	key, _ := Sign(priv, "Acme", "", "pro", []string{pcA, "abcd efgh 2345"}, time.Now(), time.Time{}) // codes are normalised
	for _, pc := range []string{pcA, pcB, "abcd-efgh-2345"} {
		if _, err := Verify(pub, key, time.Now(), pc); err != nil {
			t.Errorf("%s is covered: %v", pc, err)
		}
	}
	if l, err := Verify(pub, key, time.Now(), "ZZZZ-ZZZZ-ZZZZ"); err != ErrWrongMachine || l.Paid() && false {
		t.Fatalf("another PC must be refused, got %v", err)
	}
	if _, err := Verify(pub, key, time.Now(), ""); err != ErrWrongMachine {
		t.Fatalf("a PC with no machine code is not covered either: %v", err)
	}
	// An unbound key (a seller's test key) works anywhere.
	open, _ := Sign(priv, "Test", "", "pro", nil, time.Now(), time.Time{})
	if _, err := Verify(pub, open, time.Now(), "ZZZZ-ZZZZ-ZZZZ"); err != nil {
		t.Fatalf("an unbound key works on any PC: %v", err)
	}
}

func TestSigningRefusesBadInput(t *testing.T) {
	_, priv := keys(t)
	if _, err := Sign(priv, "x", "", "gold", nil, time.Now(), time.Time{}); err == nil {
		t.Fatal("unknown plans cannot be issued")
	}
	if _, err := Sign(priv, "x", "", "pro", []string{"not-a-code"}, time.Now(), time.Time{}); err == nil {
		t.Fatal("a malformed machine code must be refused")
	}
	many := make([]string, MaxMachines+1)
	for i := range many {
		many[i] = codeFor(string(rune('a' + i)))
	}
	if _, err := Sign(priv, "x", "", "pro", many, time.Now(), time.Time{}); err == nil {
		t.Fatal("one key lists a bounded number of PCs")
	}
}

func TestTamperedWrongKeyAndGarbageAreRefused(t *testing.T) {
	pub, priv := keys(t)
	other, _ := keys(t)
	key, _ := Sign(priv, "Ada", "", "pro", []string{pcA}, time.Now(), time.Time{})

	if _, err := Verify(other, key, time.Now(), pcA); err != ErrBadKey {
		t.Fatalf("a key signed by someone else must be refused, got %v", err)
	}
	for name, bad := range map[string]string{
		"empty":       "",
		"no prefix":   strings.TrimPrefix(key, prefix),
		"truncated":   key[:len(key)-6],
		"no dot":      prefix + "abcdef",
		"not base64":  prefix + "!!!.???",
		"extra field": key + ".x",
	} {
		if _, err := Verify(pub, bad, time.Now(), pcA); err != ErrBadKey {
			t.Errorf("%s: want ErrBadKey, got %v", name, err)
		}
	}
	parts := strings.Split(strings.TrimPrefix(key, prefix), ".")
	flipped := prefix + strings.Replace(parts[0], parts[0][:3], "AAA", 1) + "." + parts[1]
	if flipped != key {
		if _, err := Verify(pub, flipped, time.Now(), pcA); err == nil {
			t.Error("an altered license must not verify")
		}
	}
}

func TestExpiry(t *testing.T) {
	pub, priv := keys(t)
	start := time.Now()
	key, _ := Sign(priv, "Ada", "", "pro", []string{pcA}, start, start.AddDate(0, 0, 30))
	if _, err := Verify(pub, key, start.AddDate(0, 0, 29), pcA); err != nil {
		t.Fatalf("still valid on day 29: %v", err)
	}
	if _, err := Verify(pub, key, start.AddDate(0, 0, 31), pcA); err != ErrExpired {
		t.Fatalf("expired on day 31: %v", err)
	}
}

func TestMachineCodeIsStableShortAndOneWay(t *testing.T) {
	a, b := MachineCode(), MachineCode()
	if a != b {
		t.Fatalf("the code must not change between calls: %s vs %s", a, b)
	}
	if !regexp.MustCompile(`^[A-Z2-7]{4}-[A-Z2-7]{4}-[A-Z2-7]{4}$`).MatchString(a) {
		t.Fatalf("code format: %q", a)
	}
	if codeFor("one PC") == codeFor("another PC") || codeFor("one PC") != codeFor("one PC") {
		t.Fatal("different PCs get different codes, the same PC the same one")
	}
	if codeFor("") != "" {
		t.Fatal("a PC that cannot be identified has no code, so no license can be bound to it")
	}
	if NormalizeCode(" abcd efgh-2345 ") != "ABCD-EFGH-2345" {
		t.Fatalf("normalised: %q", NormalizeCode(" abcd efgh-2345 "))
	}
}

func TestTheProgramStartsFreeAndCurrentFollowsTheInstalledKey(t *testing.T) {
	restore := SetForTest(nil)
	defer restore()
	if c := Current(); c.Paid() || c.Features != FreeFeatures {
		t.Fatalf("no license: %+v", c)
	}
	l := License{Plan: "pro", Features: ProFeatures, Expires: time.Now().Add(time.Hour)}
	SetForTest(&l)
	if !Current().Paid() || !Allowed().DNS {
		t.Fatal("an installed license unlocks its features")
	}
	l.Expires = time.Now().Add(-time.Second)
	SetForTest(&l)
	if Current().Paid() {
		t.Fatal("a license that runs out while the program is open stops counting")
	}
}

func TestPatchingTheCachedLicenseInMemoryDoesNotLast(t *testing.T) {
	restore := SetForTest(nil)
	defer restore()
	// Someone writes a Pro license straight into memory, with a key that does not check out. It works
	// until the program next verifies the key it holds, which is soon.
	cur.Store(&held{l: License{Plan: "pro", Features: ProFeatures}, raw: "RE1.forged.forged", at: time.Now().Add(-time.Minute)})
	if Current().Paid() {
		t.Fatal("a forged cached license must fall back to free at the next re-check")
	}
	if cur.Load() != nil {
		t.Fatal("and it is dropped")
	}
}

func TestInstallLoadAndRemoveUseTheKeyFile(t *testing.T) {
	restore := SetForTest(nil)
	defer restore()
	path := filepath.Join(t.TempDir(), "sub", "license.key")

	if l, err := Load(path); err != nil || l.Paid() {
		t.Fatalf("a missing file is the free edition: %+v %v", l, err)
	}
	if _, err := Install(path, "RE1.garbage.garbage"); err == nil {
		t.Fatal("a bad key must not be installed")
	}
	if Current().Paid() {
		t.Fatal("a refused key leaves the free edition")
	}

	// The program checks against its embedded key and this PC's code; swap both for the test.
	pubT, priv := keys(t)
	oldPub, oldMachine := pub, machine
	pub, machine = pubT, func() string { return pcA }
	defer func() { pub, machine = oldPub, oldMachine }()

	elsewhere, _ := Sign(priv, "Ada", "", "pro", []string{pcB}, time.Now(), time.Time{})
	if _, err := Install(path, elsewhere); err != ErrWrongMachine || Current().Paid() {
		t.Fatalf("a key for another PC is refused and leaves the free edition: %v", err)
	}
	key, _ := Sign(priv, "Ada", "", "pro", []string{pcA}, time.Now(), time.Time{})
	l, err := Install(path, key)
	if err != nil || !l.Paid() {
		t.Fatalf("install: %v %+v", err, l)
	}
	if Current().Paid() {
		t.Fatal("a new key is not Pro until the licensing server has said ok")
	}
	lease.Store(&leaseState{verdict: &Verdict{Status: StatusOK, License: l.ID, Issued: time.Now().Unix()}}) // its answer
	if !Current().Paid() {
		t.Fatal("after the server's ok the key is Pro")
	}
	cur.Store(nil) // a restart
	if l, err := Load(path); err != nil || !l.Paid() {
		t.Fatalf("load after restart: %+v %v", l, err)
	}
	if err := Remove(path); err != nil || Current().Paid() {
		t.Fatalf("remove: %v paid=%v", err, Current().Paid())
	}
}

func TestTheEmbeddedPublicKeyIsWellFormed(t *testing.T) {
	if len(pub) != ed25519.PublicKeySize {
		t.Fatal("the embedded key must be a 32-byte ed25519 key")
	}
}

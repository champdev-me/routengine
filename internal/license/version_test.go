package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestAKeyLimitedToAMajorVersionStopsAtTheNextOne(t *testing.T) {
	pubK, priv, _ := ed25519.GenerateKey(rand.Reader)
	key, err := SignLimited(priv, "Ada", "", "pro", nil, time.Now(), time.Time{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	l, err := Verify(pubK, key, time.Now(), "AAAA-BBBB-CCCC")
	if err != nil || l.MaxMajor != 1 {
		t.Fatalf("verify: %+v %v", l, err)
	}
	if _, err := SignLimited(priv, "Ada", "", "pro", nil, time.Now(), time.Time{}, -1); err == nil {
		t.Fatal("a negative version is nonsense")
	}

	defer SetAppVersion("")
	cur.Store(&held{l: l, raw: ""})
	defer cur.Store(nil)

	SetAppVersion("1.9.4")
	if !Current().Paid() || Denial() != "" {
		t.Fatalf("1.x license on 1.9.4: paid=%v denial=%q", Current().Paid(), Denial())
	}
	SetAppVersion("2.0.0")
	if Current().Paid() {
		t.Fatal("a 1.x license must not unlock 2.0")
	}
	if d := Denial(); d == "" || !contains(d, "version 1.x") {
		t.Fatalf("the reason must say what the license covers: %q", d)
	}
	SetAppVersion("dev")
	if !Current().Paid() {
		t.Fatal("a development build is never limited")
	}

	if !CoversVersion("1.12.0") || CoversVersion("2.0.0") || !CoversVersion("v1.0.0") {
		t.Fatal("CoversVersion must follow the key")
	}
}

func TestAKeyWithNoLimitCoversEveryVersion(t *testing.T) {
	cur.Store(&held{l: License{Plan: "pro", Features: ProFeatures}})
	defer cur.Store(nil)
	if !CoversVersion("9.0.0") || !covers(License{Plan: "pro"}, 9) {
		t.Fatal("no limit means every version")
	}
	cur.Store(nil)
	if !CoversVersion("9.0.0") {
		t.Fatal("with no Pro license there is nothing to withhold")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

package license

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The licensing server (server/, in Bun) and this program are written in different languages. These tests check
// that they agree: the verdicts the server signs open here, and the whole exchange works against a running server.

// bunVerdicts is testdata/bun-verdict.json, written by server/test/interop.test.ts with a throwaway server key.
type bunVerdicts struct {
	ServerPublicKey string   `json:"serverPublicKey"`
	License         string   `json:"license"`
	Machine         string   `json:"machine"`
	Nonce           string   `json:"nonce"`
	OK              Envelope `json:"ok"`
	Refused         Envelope `json:"refused"`
}

func TestVerdictsFromTheBunServerOpenHere(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "bun-verdict.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f bunVerdicts
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	pubK, err := hex.DecodeString(f.ServerPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	defer SetServerKeyForTest(ed25519.PublicKey(pubK))()

	v, err := openVerdict(f.OK)
	if err != nil {
		t.Fatalf("the server's ok does not open: %v", err)
	}
	if v.Status != StatusOK || v.License != f.License || v.Nonce != f.Nonce || NormalizeCode(v.Machine) != NormalizeCode(f.Machine) || v.Issued == 0 || v.Until != 0 {
		t.Fatalf("ok verdict = %+v", v)
	}
	r, err := openVerdict(f.Refused)
	if err != nil || r.Status != StatusInvalid || r.Message == "" || r.Nonce != f.Nonce {
		t.Fatalf("refusal = %+v, %v", r, err)
	}

	// Changing a single character of a signed verdict breaks it.
	forged := f.OK
	forged.Verdict = forged.Verdict[:len(forged.Verdict)-2] + map[bool]string{true: "AA", false: "BB"}[!strings.HasSuffix(forged.Verdict, "AA")]
	if _, err := openVerdict(forged); err == nil {
		t.Fatal("an altered verdict must not open")
	}
	// And it does not open under the program's real server key: only the real server can say ok.
	restore := SetServerKeyForTest(mustKey(ServerPublicKeyHex))
	defer restore()
	if _, err := openVerdict(f.OK); err == nil {
		t.Fatal("a verdict signed by another key must not open")
	}
}

// TestAgainstARunningServer is the whole chain with the real keys compiled into the program: this program's
// license check against a server started from server/ with the real server key. It is skipped unless
// ROUTENGINE_E2E_URL (the server's address) is set; scripts\e2e-server.ps1 sets everything up and runs it.
func TestAgainstARunningServer(t *testing.T) {
	base, keyFile, admin := os.Getenv("ROUTENGINE_E2E_URL"), os.Getenv("ROUTENGINE_E2E_KEYFILE"), os.Getenv("ROUTENGINE_E2E_ADMIN")
	if base == "" || keyFile == "" || admin == "" {
		t.Skip("no running server: see scripts/e2e-server.ps1")
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(raw))
	call := func(method, path, body string) string {
		req, _ := http.NewRequest(method, base+path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer "+admin)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("%s %s: %s %s", method, path, resp.Status, out)
		}
		return string(out)
	}

	path := filepath.Join(t.TempDir(), "license.key")
	l, err := Install(path, key) // against the program's own public key and this PC's machine code
	if err != nil {
		t.Fatalf("the key does not install here: %v", err)
	}
	t.Cleanup(func() { cur.Store(nil); lease.Store(nil); checkFailed.Store(false); seen.Store(0) })
	ask := func(app string) error {
		return CheckOnce(context.Background(), http.DefaultClient, base+"/api/v1/check", app)
	}

	if err := ask("e2e"); err != nil {
		t.Fatalf("a check against a server that has not listed this license: %v", err)
	}
	if Current().Paid() || !strings.Contains(Denial(), "not valid") {
		t.Fatalf("a key not on the issued list must not turn Pro on: paid=%v denial=%q", Current().Paid(), Denial())
	}

	call("POST", "/api/admin/licenses/import", key)
	call("POST", "/api/admin/releases", fmt.Sprintf(`{"app":"e2e","sha256":%q,"note":"go test"}`, exeHash()))
	if err := ask("e2e"); err != nil {
		t.Fatal(err)
	}
	if !Current().Paid() || Denial() != "" || VerifiedAt().IsZero() {
		t.Fatalf("a listed license with a registered program file: paid=%v denial=%q", Current().Paid(), Denial())
	}
	if left, ok := LeaseLeft(); !ok || left < 6*24*time.Hour || left > 7*24*time.Hour+time.Minute {
		t.Fatalf("the lease should be a week: %v %v", left, ok)
	}

	if err := ask("e2e-never-registered"); err != nil {
		t.Fatal(err)
	}
	if Current().Paid() || !strings.Contains(Denial(), "modified") {
		t.Fatalf("a program file that is not a release is tampered: paid=%v denial=%q", Current().Paid(), Denial())
	}

	call("POST", "/api/admin/licenses/"+l.ID+"/revoke", `{"reason":"e2e"}`)
	ask("e2e")
	if Current().Paid() || !strings.Contains(Denial(), "revoked") {
		t.Fatalf("revoked: paid=%v denial=%q", Current().Paid(), Denial())
	}
	call("POST", "/api/admin/licenses/"+l.ID+"/unrevoke", `{}`)
	ask("e2e")
	if !Current().Paid() {
		t.Fatalf("un-revoked: denial=%q", Denial())
	}
}

// TestAKeyMadeInTheAdminBoardVerifies: the admin board signs keys in the browser with its own JavaScript (server/admin).
// testdata/board-key.json is one of them, made with a throwaway key by server/test/board-ui.test.ts. The program must
// read it exactly as it reads one made by licensegen.
func TestAKeyMadeInTheAdminBoardVerifies(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "board-key.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		LicensePublicKey string `json:"licensePublicKey"`
		Machine          string `json:"machine"`
		Name             string `json:"name"`
		Email            string `json:"email"`
		MaxMajor         int    `json:"maxMajor"`
		ID               string `json:"id"`
		Key              string `json:"key"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	pubK, err := hex.DecodeString(f.LicensePublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pub := ed25519.PublicKey(pubK)

	l, err := Verify(pub, f.Key, time.Now(), f.Machine)
	if err != nil {
		t.Fatalf("a key made in the board does not verify: %v", err)
	}
	if l.ID != f.ID || l.Name != f.Name || l.Email != f.Email || l.Plan != "pro" || l.MaxMajor != f.MaxMajor || !l.Paid() || l.Seats() != 1 {
		t.Fatalf("read as %+v", l)
	}
	if !l.Expires.IsZero() {
		t.Fatalf("a lifetime key has no expiry: %v", l.Expires)
	}
	if _, err := Verify(pub, f.Key, time.Now(), "AAAA-BBBB-CCCC"); err != ErrWrongMachine {
		t.Fatalf("it works only on its PC: %v", err)
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if _, err := Verify(other, f.Key, time.Now(), f.Machine); err != ErrBadKey {
		t.Fatalf("under another public key it must be refused: %v", err)
	}
}

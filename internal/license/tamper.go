package license

// Tamper reports. When the program finds that something it relies on for the license has been changed, it tells the
// licensing server, which shows the seller. This is part of the license verification the Pro license requires (license
// agreement, section 6.3), so it concerns only an installed Pro license: the free edition sends nothing.
//
// A report holds the kind of change, the key's ID (never the key), this PC's machine code, the program version and the
// SHA-256 of the program file. It is sent once per kind for each run of the program, with the next check, and kept until
// the server has taken it. SERVER.md describes what the server does with it.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
)

// What can be reported.
const (
	TamperLeaseEdited  = "lease_edited"   // the saved answer of the licensing server was changed, or is another PC's
	TamperClockSetBack = "clock_set_back" // the PC's clock was moved back, which would stretch a lease
	TamperKeyEdited    = "key_edited"     // the saved license key no longer verifies
)

// TamperReport is exactly what the program sends to the licensing server about a change it found.
type TamperReport struct {
	V       int    `json:"v"`
	Kind    string `json:"kind"`
	License string `json:"license,omitempty"` // the key's ID, when it can be read
	Machine string `json:"machine"`           // this PC's machine code
	App     string `json:"app"`               // the program version
	Exe     string `json:"exe"`               // SHA-256 of the program file
}

// What the window says when routing is blocked, and why.
const (
	msgKeyEdited    = "The saved license key was changed or damaged. Remove it in Settings to use the free edition, or enter your key again."
	msgLeaseEdited  = "RoutEngine found that its saved license data was changed. Connect to the internet so it can check the license again."
	msgClockSetBack = "The clock on this PC was set back. Connect to the internet so RoutEngine can check this license again."
	msgModified     = "This copy of RoutEngine has been modified. Install it again from routengine.net."
)

// Flags for the two changes that are found once and stay until something fixes them. A clock set back and a program the
// server calls modified are worked out afresh each time Tampered is asked.
const (
	flagLeaseEdited uint32 = 1 << iota
	flagKeyEdited
)

var tamperFlags atomic.Uint32

func setFlag(f uint32) {
	for {
		o := tamperFlags.Load()
		if tamperFlags.CompareAndSwap(o, o|f) {
			return
		}
	}
}

func clearFlag(f uint32) {
	for {
		o := tamperFlags.Load()
		if tamperFlags.CompareAndSwap(o, o&^f) {
			return
		}
	}
}

// markTamper notes that a change was found, so routing is blocked, and reports it.
func markTamper(kind, licenseID string) {
	switch kind {
	case TamperLeaseEdited:
		setFlag(flagLeaseEdited)
	case TamperKeyEdited:
		setFlag(flagKeyEdited)
	}
	reportTamper(kind, licenseID)
}

// Tampered is why routing must not be started, or "" if nothing was found: the saved license key or the saved answer of
// the licensing server was changed, or the licensing server says this copy of the program is not a release. Each clears
// itself when it is put right: a successful check against the server, a valid key entered, or the license removed.
// A PC clock that is behind is reported but does not block: it has many innocent causes (a flat clock battery, a time
// sync), and it already pauses Pro until the server has confirmed the time.
func Tampered() string {
	f := tamperFlags.Load()
	switch {
	case f&flagKeyEdited != 0:
		return msgKeyEdited
	case f&flagLeaseEdited != 0:
		return msgLeaseEdited
	}
	if st := lease.Load(); st != nil && st.verdict != nil && st.verdict.Status == StatusTampered {
		return msgModified
	}
	return ""
}

// waiting is what was found; the report itself is made when it is sent, on the checker's goroutine, because working out the
// program file's checksum the first time reads the whole file and must not happen while the window is drawing.
type waiting struct{ kind, license string }

var (
	tamperMu    sync.Mutex
	tamperSent  = map[string]bool{} // kinds already queued by this run, so each is reported once
	tamperQueue []waiting
)

// reportTamper queues a report and asks the background checker to send it soon.
func reportTamper(kind, licenseID string) {
	tamperMu.Lock()
	if tamperSent[kind] {
		tamperMu.Unlock()
		return
	}
	tamperSent[kind] = true
	for _, q := range tamperQueue { // one of this kind is already on its way
		if q.kind == kind {
			tamperMu.Unlock()
			return
		}
	}
	tamperQueue = append(tamperQueue, waiting{kind, licenseID})
	tamperMu.Unlock()
	RequestCheck()
}

// forgetReported lets a kind be reported again, once the license has been put right or replaced.
func forgetReported() {
	tamperMu.Lock()
	tamperSent = map[string]bool{}
	tamperMu.Unlock()
}

// currentID is the ID of the installed license, or "" if none is held.
func currentID() string {
	if h := cur.Load(); h != nil {
		return h.l.ID
	}
	return ""
}

// SendTamper sends the reports that are waiting to the licensing server at url. A report stays queued until the server
// has taken it, so a PC that is offline sends it when it can; one the server refuses for good (a 4xx) is dropped so it
// cannot hold up the others. With nothing waiting it does nothing and makes no request.
func SendTamper(ctx context.Context, hc *http.Client, url, app string) error {
	tamperMu.Lock()
	todo := append([]waiting(nil), tamperQueue...)
	tamperMu.Unlock()
	if len(todo) == 0 {
		return nil
	}
	for _, w := range todo {
		r := TamperReport{V: 1, Kind: w.kind, License: w.license, Machine: machine(), App: app, Exe: exeHash()}
		body, err := json.Marshal(r)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "RoutEngine/"+app)
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		resp.Body.Close()
		refused := resp.StatusCode/100 == 4 && resp.StatusCode != http.StatusTooManyRequests
		if resp.StatusCode/100 != 2 && !refused {
			return fmt.Errorf("the licensing server answered %s", resp.Status)
		}
		if refused {
			Logf("license: the server refused a report for good (%s): dropped\n", resp.Status)
		}
		tamperMu.Lock()
		for i, q := range tamperQueue {
			if q.kind == w.kind {
				tamperQueue = append(tamperQueue[:i], tamperQueue[i+1:]...)
				break
			}
		}
		tamperMu.Unlock()
	}
	return nil
}

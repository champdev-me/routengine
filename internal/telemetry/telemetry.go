// Package telemetry sends RoutEngine's optional, anonymous usage statistics. Nothing is sent until the
// user has agreed, and the user can change the answer at any time. What is sent is exactly Report.
package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"routengine/internal/confdir"
	"runtime"
	"sync"
	"time"

	"routengine/internal/endpoints"
)

// Logf, if set, is told about a report that could not be sent.
var Logf = func(format string, a ...any) {}

// Report is the whole of what is sent, at most once a day. There is nothing else in it: no app names,
// paths, addresses or traffic. It is the same text the license agreement describes.
type Report struct {
	V        int    `json:"v"`
	ID       string `json:"id"`                // random, made on this PC when the user agrees
	App      string `json:"app"`               // the version
	Edition  string `json:"edition"`           // "free" or "pro"
	License  string `json:"license,omitempty"` // the license key's ID (not the key), Pro only
	OS       string `json:"os"`
	OSBuild  string `json:"os_build,omitempty"`
	Arch     string `json:"arch"`
	Rules    int    `json:"rules"`    // apps with rules
	Adapters int    `json:"adapters"` // network adapters
	Routing  bool   `json:"routing"`  // whether routing is on
}

// Facts is what the program knows about itself when a report is made.
type Facts struct {
	App, Edition, License string
	Rules, Adapters       int
	Routing               bool
}

// ErrNotAgreed is returned when a report is asked for but the user has not agreed.
var ErrNotAgreed = errors.New("usage statistics are off")

type saved struct {
	Choice   string    `json:"choice,omitempty"` // "yes" or "no"; empty means not asked yet
	ID       string    `json:"id,omitempty"`
	LastSent time.Time `json:"last_sent"`
}

// State is the user's answer, the install ID and when a report last went out, kept in a small file.
type State struct {
	mu   sync.Mutex
	path string
	d    saved
}

// DefaultPath is where the answer is kept.
func DefaultPath() string {
	return filepath.Join(confdir.Dir(), "telemetry.json")
}

// Open reads the state. A missing or unreadable file means the user has not been asked yet.
func Open(path string) *State {
	s := &State{path: path}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &s.d)
	}
	return s
}

// Asked reports whether the user has answered.
func (s *State) Asked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Choice != ""
}

// Enabled reports whether the user agreed and this build can send.
func (s *State) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Choice == "yes"
}

// ID is the install ID once the user has agreed, and "" before.
func (s *State) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.ID
}

// Set records the user's answer. Agreeing makes the install ID on first use; declining keeps it, so that
// agreeing again later is still the same install.
func (s *State) Set(agree bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !agree {
		s.d.Choice = "no"
		return s.save()
	}
	if s.d.ID == "" {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return err
		}
		s.d.ID = hex.EncodeToString(b[:])
	}
	s.d.Choice = "yes"
	return s.save()
}

func (s *State) save() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// due reports whether a report should go out now: the user agreed and a day has passed.
func (s *State) due(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Choice == "yes" && now.Sub(s.d.LastSent) >= 20*time.Hour
}

// Run sends a report about once a day while the user agrees. It looks every few minutes, so agreeing in
// Settings takes effect without a restart, and it waits a little after start so it never slows startup.
// It returns when ctx ends.
func (s *State) Run(ctx context.Context, facts func() Facts) {
	wait := 30 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 5 * time.Minute
		if s.due(time.Now()) {
			if err := s.Send(ctx, facts()); err != nil && !errors.Is(err, ErrNotAgreed) {
				Logf("usage statistics not sent: %v\n", err)
			}
		}
	}
}

// Send makes one report to the statistics address now, if the user agreed.
func (s *State) Send(ctx context.Context, f Facts) error {
	return s.SendTo(ctx, &http.Client{Timeout: 15 * time.Second}, endpoints.Ping, f)
}

// SendTo posts one report to url. The time is only remembered once the server accepted it. The program
// itself only ever calls Send, which uses the fixed address; this is for tests.
func (s *State) SendTo(ctx context.Context, hc *http.Client, url string, f Facts) error {
	s.mu.Lock()
	if s.d.Choice != "yes" {
		s.mu.Unlock()
		return ErrNotAgreed
	}
	r := Report{V: 1, ID: s.d.ID, App: f.App, Edition: f.Edition, License: f.License, OS: runtime.GOOS,
		OSBuild: osBuild(), Arch: runtime.GOARCH, Rules: f.Rules, Adapters: f.Adapters, Routing: f.Routing}
	s.mu.Unlock()

	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "RoutEngine/"+f.App)
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("the statistics server answered %s", resp.Status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.LastSent = time.Now()
	return s.save()
}

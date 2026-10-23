package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func newState(t *testing.T) (*State, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "telemetry.json")
	return Open(p), p
}

func TestNothingIsSentUntilTheUserAgrees(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	s, _ := newState(t)

	if s.Asked() || s.Enabled() {
		t.Fatal("a new install must start unasked and off")
	}
	if err := s.SendTo(context.Background(), srv.Client(), srv.URL, Facts{App: "1.0.0"}); !errors.Is(err, ErrNotAgreed) {
		t.Fatalf("unasked: err = %v, want ErrNotAgreed", err)
	}
	if s.due(time.Now()) {
		t.Fatal("unasked: a report must never be due")
	}
	s.Set(false)
	if !s.Asked() || s.Enabled() || s.due(time.Now()) {
		t.Fatal("declined: asked but off")
	}
	if err := s.SendTo(context.Background(), srv.Client(), srv.URL, Facts{}); !errors.Is(err, ErrNotAgreed) {
		t.Fatalf("declined: err = %v, want ErrNotAgreed", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests reached the server without consent", hits.Load())
	}
}

func TestReportHoldsOnlyWhatTheAgreementSays(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	s, _ := newState(t)
	if err := s.Set(true); err != nil {
		t.Fatal(err)
	}
	f := Facts{App: "0.7.0", Edition: "pro", License: "a59c20601c340d14", Rules: 7, Adapters: 3, Routing: true}
	if err := s.SendTo(context.Background(), srv.Client(), srv.URL, f); err != nil {
		t.Fatal(err)
	}

	allowed := map[string]bool{"v": true, "id": true, "app": true, "edition": true, "license": true, "os": true,
		"os_build": true, "arch": true, "rules": true, "adapters": true, "routing": true}
	for k := range got {
		if !allowed[k] {
			t.Errorf("the report carries %q, which the agreement does not list", k)
		}
	}
	if id, _ := got["id"].(string); len(id) != 32 || id != s.ID() {
		t.Errorf("id = %q, want the 32-character install ID %q", id, s.ID())
	}
	if got["edition"] != "pro" || got["license"] != "a59c20601c340d14" || got["rules"] != 7.0 || got["adapters"] != 3.0 || got["routing"] != true || got["app"] != "0.7.0" {
		t.Errorf("report = %v", got)
	}
}

func TestAnswerAndIDSurviveARestart(t *testing.T) {
	s, path := newState(t)
	s.Set(true)
	id := s.ID()

	again := Open(path)
	if !again.Enabled() || again.ID() != id || id == "" {
		t.Fatalf("after a restart: enabled=%v id=%q want %q", again.Enabled(), again.ID(), id)
	}
	again.Set(false)
	if Open(path).Enabled() {
		t.Fatal("declining must stick")
	}
	again.Set(true)
	if again.ID() != id {
		t.Fatal("agreeing again must be the same install")
	}
}

func TestOnceADayAndOnlyAfterTheServerAccepted(t *testing.T) {
	status := http.StatusInternalServerError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	defer srv.Close()
	s, _ := newState(t)
	s.Set(true)

	if err := s.SendTo(context.Background(), srv.Client(), srv.URL, Facts{}); err == nil {
		t.Fatal("a 500 must be an error")
	}
	if !s.due(time.Now()) {
		t.Fatal("a failed send must be tried again")
	}
	status = http.StatusNoContent
	if err := s.SendTo(context.Background(), srv.Client(), srv.URL, Facts{}); err != nil {
		t.Fatal(err)
	}
	if s.due(time.Now().Add(time.Hour)) {
		t.Fatal("not due an hour after a report")
	}
	if !s.due(time.Now().Add(21 * time.Hour)) {
		t.Fatal("due a day later")
	}
}

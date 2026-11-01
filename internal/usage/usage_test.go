package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func at(s string) func() time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	return func() time.Time { return t }
}

func TestTodayAndMonthSplit(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "usage.json"))

	l.now = at("2026-10-03 10:00")
	l.AddApp(`C:\x\Brave.exe`, "Brave.exe", "Ethernet 4", 1000, 100)
	l.now = at("2026-10-21 09:00")
	l.AddApp(`c:\x\brave.EXE`, "Brave.exe", "Ethernet 4", 500, 50) // same app, any case
	l.AddApp(`C:\x\Brave.exe`, "Brave.exe", "WiFi", 200, 20)
	l.AddAdapter("WiFi", 900, 90)
	l.now = at("2026-09-30 23:00")
	l.AddApp(`C:\x\Brave.exe`, "Brave.exe", "WiFi", 7777, 7777) // last month: not in this report

	l.now = at("2026-10-21 12:00")
	rep := l.Report()
	if rep.Day != "2026-10-21" || len(rep.Apps) != 1 {
		t.Fatalf("one app expected: %+v", rep)
	}
	a := rep.Apps[0]
	if a.Today != (Bytes{700, 70}) || a.Month != (Bytes{1700, 170}) {
		t.Fatalf("today %+v month %+v", a.Today, a.Month)
	}
	if a.TodayVia["Ethernet 4"] != (Bytes{500, 50}) || a.TodayVia["WiFi"] != (Bytes{200, 20}) {
		t.Fatalf("today by adapter: %+v", a.TodayVia)
	}
	if len(rep.Adapters) != 1 || rep.Adapters[0].Today != (Bytes{900, 90}) {
		t.Fatalf("adapters: %+v", rep.Adapters)
	}
}

func TestPersistsAcrossRestarts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "usage.json")
	l, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	l.AddApp("a.exe", "a.exe", "WiFi", 10, 5)
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	l2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := l2.Report().Apps; len(got) != 1 || got[0].Today != (Bytes{10, 5}) {
		t.Fatalf("after reopening: %+v", got)
	}
	if got := ReadReport(p).Apps; len(got) != 1 {
		t.Fatalf("ReadReport: %+v", got)
	}
	if got := ReadReport(filepath.Join(t.TempDir(), "none.json")); len(got.Apps) != 0 {
		t.Fatal("a missing file is an empty report")
	}
}

func TestFlushOnlyWritesWhenSomethingChanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	l, _ := Open(p)
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("nothing changed, nothing should be written")
	}
	l.AddApp("a.exe", "a", "WiFi", 1, 1)
	l.Flush()
	if _, err := os.Stat(p); err != nil {
		t.Fatal("a change must be written")
	}
}

func TestCorruptFileIsSetAsideNotLost(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	os.WriteFile(p, []byte("{not json"), 0o644)
	l, err := Open(p)
	if err == nil {
		t.Fatal("a corrupt file should be reported")
	}
	if _, serr := os.Stat(p + ".bad"); serr != nil {
		t.Fatal("the unreadable file must be kept as .bad")
	}
	l.AddApp("a.exe", "a", "WiFi", 1, 1) // and the ledger still works
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestOldDaysArePruned(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	l, _ := Open(p)
	l.now = at("2024-01-01 10:00")
	l.AddApp("old.exe", "old", "WiFi", 1, 1)
	l.now = at("2026-10-21 10:00")
	l.AddApp("new.exe", "new", "WiFi", 1, 1)
	l.Flush()
	if len(l.days) != 1 {
		t.Fatalf("days kept: %d", len(l.days))
	}
}

func TestHistoryRowsAndTotalsForASpanOfDays(t *testing.T) {
	h := NewHistory([]Row{
		{Date: "2026-10-20", Exe: `C:\a\steam.exe`, Name: "steam.exe", Adapter: "WiFi", Bytes: Bytes{Down: 100, Up: 10}},
		{Date: "2026-10-21", Exe: `C:\a\steam.exe`, Name: "steam.exe", Adapter: "WiFi", Bytes: Bytes{Down: 50, Up: 5}},
		{Date: "2026-10-21", Exe: `C:\a\steam.exe`, Name: "steam.exe", Adapter: "Ethernet 4", Bytes: Bytes{Down: 25}},
		{Date: "2026-10-21", Exe: `C:\b\brave.exe`, Name: "brave.exe", Adapter: "WiFi", Bytes: Bytes{Down: 400, Up: 40}},
		{Date: "2026-10-22", Exe: `C:\b\brave.exe`, Name: "brave.exe", Adapter: "WiFi", Bytes: Bytes{Down: 1}},
	})

	rows := h.Rows("2026-10-21", "2026-10-21")
	if len(rows) != 3 || rows[0].Name != "brave.exe" || rows[1].Adapter != "Ethernet 4" || rows[2].Adapter != "WiFi" {
		t.Fatalf("one day, sorted by name then adapter: %+v", rows)
	}
	if n := len(h.Rows("2026-10-20", "2026-10-22")); n != 5 {
		t.Fatalf("the whole span: %d rows, want 5", n)
	}
	if n := len(h.Rows("2026-11-01", "2026-11-30")); n != 0 {
		t.Fatalf("a span with nothing in it: %d rows", n)
	}

	apps := h.Apps("2026-10-20", "2026-10-22")
	if len(apps) != 2 || apps[0].Name != "brave.exe" || apps[0].Total != (Bytes{Down: 401, Up: 40}) {
		t.Fatalf("busiest first: %+v", apps)
	}
	if s := apps[1]; s.Total != (Bytes{Down: 175, Up: 15}) || s.Via["WiFi"] != (Bytes{Down: 150, Up: 15}) || s.Via["Ethernet 4"] != (Bytes{Down: 25}) {
		t.Fatalf("steam: %+v", s)
	}
}

func TestReadHistoryFromTheLedgerFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	l, _ := Open(path)
	l.AddApp(`C:\a\steam.exe`, "steam.exe", "WiFi", 1000, 100)
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	today := time.Now().Format("2006-01-02")
	rows := ReadHistory(path).Rows(today, today)
	if len(rows) != 1 || rows[0].Down != 1000 || rows[0].Up != 100 || rows[0].Adapter != "WiFi" {
		t.Fatalf("rows: %+v", rows)
	}
	if n := len(ReadHistory(filepath.Join(t.TempDir(), "missing.json")).Rows("0000-01-01", "9999-12-31")); n != 0 {
		t.Fatal("a missing file is an empty history")
	}
}

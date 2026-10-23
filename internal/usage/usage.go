// Package usage keeps a ledger of how much data each application and each adapter has carried, per
// calendar day, so the window can show "today" and "this month" and keep them across restarts.
package usage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Bytes is traffic in both directions.
type Bytes struct{ Down, Up int64 }

func (b Bytes) Total() int64 { return b.Down + b.Up }

func (b Bytes) plus(o Bytes) Bytes { return Bytes{b.Down + o.Down, b.Up + o.Up} }

const (
	dayFormat = "2006-01-02"
	keepDays  = 400 // older days are dropped when the ledger is saved
)

type appRec struct {
	Exe  string           `json:"exe"`
	Name string           `json:"name"`
	Via  map[string]Bytes `json:"via,omitempty"` // by adapter
}

type dayRec struct {
	Apps     map[string]*appRec `json:"apps,omitempty"`     // by lower-case exe path; "" is traffic whose owner is unknown
	Adapters map[string]Bytes   `json:"adapters,omitempty"` // everything the adapter carried, whoever sent it
}

type file struct {
	Version int                `json:"version"`
	Days    map[string]*dayRec `json:"days"`
}

// Ledger is the usage record. It is safe for concurrent use.
type Ledger struct {
	mu    sync.Mutex
	path  string
	now   func() time.Time
	days  map[string]*dayRec
	dirty bool
}

// Open loads the ledger at path. A missing file is an empty ledger. A file that cannot be read is
// moved aside to path+".bad" so it is not lost, and the ledger starts empty; the error says so.
func Open(path string) (*Ledger, error) {
	l := &Ledger{path: path, now: time.Now, days: map[string]*dayRec{}}
	days, err := read(path)
	switch {
	case err == nil:
		l.days = days
	case errors.Is(err, os.ErrNotExist):
	default:
		os.Rename(path, path+".bad")
		return l, errors.New("usage history could not be read and was set aside as " + filepath.Base(path) + ".bad: " + err.Error())
	}
	return l, nil
}

func read(path string) (map[string]*dayRec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if f.Days == nil {
		f.Days = map[string]*dayRec{}
	}
	return f.Days, nil
}

func (l *Ledger) today() *dayRec {
	k := l.now().Format(dayFormat)
	d := l.days[k]
	if d == nil {
		d = &dayRec{}
		l.days[k] = d
	}
	return d
}

// AddApp records traffic an application sent through an adapter.
func (l *Ledger) AddApp(exe, name, adapter string, down, up int64) {
	if down <= 0 && up <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.today()
	if d.Apps == nil {
		d.Apps = map[string]*appRec{}
	}
	key := strings.ToLower(exe)
	a := d.Apps[key]
	if a == nil {
		a = &appRec{Exe: exe, Name: name, Via: map[string]Bytes{}}
		d.Apps[key] = a
	}
	if a.Via == nil {
		a.Via = map[string]Bytes{}
	}
	a.Via[adapter] = a.Via[adapter].plus(Bytes{down, up})
	l.dirty = true
}

// AddAdapter records everything an adapter carried, from the operating system's counters.
func (l *Ledger) AddAdapter(adapter string, down, up int64) {
	if down <= 0 && up <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.today()
	if d.Adapters == nil {
		d.Adapters = map[string]Bytes{}
	}
	d.Adapters[adapter] = d.Adapters[adapter].plus(Bytes{down, up})
	l.dirty = true
}

// Flush writes the ledger if anything changed, in one step (temporary file, then rename).
func (l *Ledger) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.dirty {
		return nil
	}
	cutoff := l.now().AddDate(0, 0, -keepDays).Format(dayFormat)
	for k := range l.days {
		if k < cutoff {
			delete(l.days, k)
		}
	}
	b, err := json.Marshal(file{Version: 1, Days: l.days})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return err
	}
	l.dirty = false
	return nil
}

// FlushEvery saves the ledger on a timer until the process ends.
func (l *Ledger) FlushEvery(d time.Duration) {
	go func() {
		for range time.Tick(d) {
			l.Flush()
		}
	}()
}

// AppUsage is one application's totals.
type AppUsage struct {
	Exe, Name string
	Today     Bytes
	Month     Bytes            // the calendar month so far, today included
	TodayVia  map[string]Bytes // today, by adapter
}

// AdapterUsage is what one adapter carried.
type AdapterUsage struct {
	Name         string
	Today, Month Bytes
}

// Report is the ledger boiled down for display: this month's applications and adapters.
type Report struct {
	Day      string // today's date, so a reader can tell the report is current
	Apps     []AppUsage
	Adapters []AdapterUsage
}

// Report summarises today and the calendar month so far.
func (l *Ledger) Report() Report {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	today, month := now.Format(dayFormat), now.Format("2006-01")
	rep := Report{Day: today}
	apps := map[string]*AppUsage{}
	ads := map[string]*AdapterUsage{}
	for day, d := range l.days {
		if !strings.HasPrefix(day, month) {
			continue
		}
		isToday := day == today
		for key, a := range d.Apps {
			u := apps[key]
			if u == nil {
				u = &AppUsage{Exe: a.Exe, Name: a.Name}
				apps[key] = u
			}
			for ad, b := range a.Via {
				u.Month = u.Month.plus(b)
				if isToday {
					u.Today = u.Today.plus(b)
					if u.TodayVia == nil {
						u.TodayVia = map[string]Bytes{}
					}
					u.TodayVia[ad] = u.TodayVia[ad].plus(b)
				}
			}
		}
		for name, b := range d.Adapters {
			u := ads[name]
			if u == nil {
				u = &AdapterUsage{Name: name}
				ads[name] = u
			}
			u.Month = u.Month.plus(b)
			if isToday {
				u.Today = u.Today.plus(b)
			}
		}
	}
	for _, u := range apps {
		rep.Apps = append(rep.Apps, *u)
	}
	for _, u := range ads {
		rep.Adapters = append(rep.Adapters, *u)
	}
	sort.Slice(rep.Apps, func(i, j int) bool { return rep.Apps[i].Month.Total() > rep.Apps[j].Month.Total() })
	sort.Slice(rep.Adapters, func(i, j int) bool { return rep.Adapters[i].Name < rep.Adapters[j].Name })
	return rep
}

// ReadReport summarises the ledger file at path without keeping it open, for a window that shows
// stored usage while the service is not running. A missing file is an empty report.
func ReadReport(path string) Report {
	l := &Ledger{path: path, now: time.Now, days: map[string]*dayRec{}}
	if days, err := read(path); err == nil {
		l.days = days
	}
	return l.Report()
}

// ---------------------------------------------------------------- history, for the usage page and its export

// Row is one day's traffic of one application through one adapter.
type Row struct {
	Date    string // 2006-01-02
	Exe     string
	Name    string
	Adapter string
	Bytes
}

// History is the stored days, for looking at any span of them.
type History struct{ days map[string]*dayRec }

// ReadHistory loads the ledger file at path without keeping it open. A missing or unreadable file is an empty history.
func ReadHistory(path string) *History {
	h := &History{days: map[string]*dayRec{}}
	if days, err := read(path); err == nil {
		h.days = days
	}
	return h
}

// NewHistory builds a history from rows, for demonstrations and tests.
func NewHistory(rows []Row) *History {
	h := &History{days: map[string]*dayRec{}}
	for _, r := range rows {
		d := h.days[r.Date]
		if d == nil {
			d = &dayRec{Apps: map[string]*appRec{}, Adapters: map[string]Bytes{}}
			h.days[r.Date] = d
		}
		key := strings.ToLower(r.Exe)
		a := d.Apps[key]
		if a == nil {
			a = &appRec{Exe: r.Exe, Name: r.Name, Via: map[string]Bytes{}}
			d.Apps[key] = a
		}
		a.Via[r.Adapter] = a.Via[r.Adapter].plus(r.Bytes)
		d.Adapters[r.Adapter] = d.Adapters[r.Adapter].plus(r.Bytes)
	}
	return h
}

// Rows returns every day × application × adapter between from and to (both "2006-01-02", inclusive),
// oldest day first.
func (h *History) Rows(from, to string) []Row {
	var rows []Row
	for day, d := range h.days {
		if day < from || day > to {
			continue
		}
		for _, a := range d.Apps {
			for ad, b := range a.Via {
				rows = append(rows, Row{Date: day, Exe: a.Exe, Name: a.Name, Adapter: ad, Bytes: b})
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch {
		case a.Date != b.Date:
			return a.Date < b.Date
		case a.Name != b.Name:
			return a.Name < b.Name
		}
		return a.Adapter < b.Adapter
	})
	return rows
}

// AppTotal is one application's traffic over a span.
type AppTotal struct {
	Exe, Name string
	Total     Bytes
	Via       map[string]Bytes // by adapter
}

// Apps totals each application between from and to, the busiest first.
func (h *History) Apps(from, to string) []AppTotal {
	by := map[string]*AppTotal{}
	for _, r := range h.Rows(from, to) {
		key := strings.ToLower(r.Exe)
		t := by[key]
		if t == nil {
			t = &AppTotal{Exe: r.Exe, Name: r.Name, Via: map[string]Bytes{}}
			by[key] = t
		}
		t.Total = t.Total.plus(r.Bytes)
		t.Via[r.Adapter] = t.Via[r.Adapter].plus(r.Bytes)
	}
	out := make([]AppTotal, 0, len(by))
	for _, t := range by {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := out[i].Total.Total(), out[j].Total.Total(); a != b {
			return a > b
		}
		return out[i].Name < out[j].Name
	})
	return out
}

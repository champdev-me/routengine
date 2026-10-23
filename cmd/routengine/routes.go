package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"routengine/internal/engine"
	"routengine/internal/license"
	"routengine/internal/logx"
	"routengine/internal/policy"
	"routengine/internal/tun"
	usagelog "routengine/internal/usage" // not "usage": main.go has a constant of that name
)

// UsagePath is where the data-used history lives, next to the rules.
func usagePath() string { return filepath.Join(filepath.Dir(policy.DefaultPath()), "usage.json") }

// attachRoutes turns on per-app routing, per-app speed counting and the data-used history for t. An
// explicit -rules file must exist and parse. The default file (policy.DefaultPath) may be missing:
// until someone creates it every app is balanced as before, and it is picked up when it appears.
func (c *common) attachRoutes(t *tun.Tun) error {
	path := c.rules
	if path == "" {
		path = policy.DefaultPath()
	} else if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("per-app rules: %w", err)
	}
	engine.RouteLog = func(f string, a ...any) { logx.Printf(f, a...) }
	r, err := policy.Open(path, func(f string, a ...any) { logx.Printf(f, a...) })
	if err != nil {
		return fmt.Errorf("per-app rules: %w", err)
	}
	r.Auto = func() string { return t.B.S.AutoDefault() } // the default adapter when the rules choose none
	t.Route = r.Route

	// A usage history that cannot be read must not stop routing: say so and start a fresh one.
	ledger, err := usagelog.Open(usagePath())
	if err != nil {
		logx.Printf("warning: %v\n", err)
	}
	engine.Apps.SetLedger(ledger)
	ledger.FlushEvery(10 * time.Second)
	go func() {
		var feed engine.LinkFeeder
		for range time.Tick(time.Second) {
			feed.Feed(t.B.S, ledger)
		}
	}()
	return nil
}

// gatedUsage hides what the license does not include: the free edition sees today's data, not the month's.
func gatedUsage(r usagelog.Report) usagelog.Report {
	if license.Allowed().MonthlyHistory {
		return r
	}
	for i := range r.Apps {
		r.Apps[i].Month = usagelog.Bytes{}
	}
	for i := range r.Adapters {
		r.Adapters[i].Month = usagelog.Bytes{}
	}
	return r
}

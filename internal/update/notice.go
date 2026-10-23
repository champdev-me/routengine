package update

import (
	"context"
	"net/http"
	"time"
)

// Notify tells the user, through logf, when a newer release exists. It does not download or install anything: it is
// for the command line and for the systems with no self-update. Like the Windows updater it asks the update address
// a little after start and then about once a day, and never for a development build. It returns when ctx ends.
func Notify(ctx context.Context, addr, current string, logf func(format string, a ...any)) {
	if !IsRelease(current) {
		return
	}
	hc := &http.Client{Timeout: 20 * time.Second}
	wait, told := 45*time.Second, ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		m, err := fetch(ctx, hc, addr, current)
		if err != nil {
			wait = time.Hour // no connection or a bad answer: try again soon, quietly
			continue
		}
		wait = 24 * time.Hour
		if Newer(current, m.Version) && m.Version != told {
			told = m.Version
			logf("RoutEngine %s is available (you have %s). %s Download: https://routengine.net\n", m.Version, current, m.Notes)
		}
	}
}

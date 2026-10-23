package engine

import (
	"context"
	"errors"
	"math"
	"sync/atomic"

	"golang.org/x/time/rate"
)

// ErrCapReached is what the connections of an app fail with once it has used up its data allowance.
var ErrCapReached = errors.New("data cap reached")

// BytesPerMbit converts megabits per second to bytes per second.
const BytesPerMbit = 125_000

// SetLimits caps the app's speed, in bytes per second each way; 0 or less means no limit. It applies at
// once to connections that are already open, because they all share the same limiter.
func (m *AppMeter) SetLimits(down, up float64) {
	setLimiter(&m.limIn, down)
	setLimiter(&m.limOut, up)
}

// Limits reports the current limits in bytes per second (0 is none).
func (m *AppMeter) Limits() (down, up float64) {
	lim := func(p *atomic.Pointer[rate.Limiter]) float64 {
		if l := p.Load(); l != nil {
			return float64(l.Limit())
		}
		return 0
	}
	return lim(&m.limIn), lim(&m.limOut)
}

// SetDenied stops the app's traffic (or lets it flow again): open connections fail on their next read
// or write, and new ones are refused.
func (m *AppMeter) SetDenied(on bool) { m.denied.Store(on) }

// Denied reports whether the app's traffic is stopped.
func (m *AppMeter) Denied() bool { return m.denied.Load() }

func setLimiter(p *atomic.Pointer[rate.Limiter], bytesPerSec float64) {
	if bytesPerSec <= 0 || math.IsNaN(bytesPerSec) {
		p.Store(nil)
		return
	}
	// A burst of a quarter second keeps the average at the limit while letting a request start quickly.
	burst := int(math.Max(bytesPerSec/4, 64<<10))
	if l := p.Load(); l != nil {
		l.SetLimit(rate.Limit(bytesPerSec))
		l.SetBurst(burst)
		return
	}
	p.Store(rate.NewLimiter(rate.Limit(bytesPerSec), burst))
}

// throttle waits until n bytes may pass the limiter, in pieces no bigger than its burst. It gives up
// early when ctx ends, which is when the connection closes.
func throttle(ctx context.Context, l *rate.Limiter, n int) {
	if ctx == nil {
		ctx = context.Background()
	}
	for n > 0 {
		k := min(n, l.Burst())
		if l.WaitN(ctx, k) != nil {
			return
		}
		n -= k
	}
}

package engine

import "routengine/internal/usage"

// LinkTotals returns each connected adapter's cumulative bytes (in, out) from the operating
// system's counters, as of the last Tick. Unlike the per-app numbers this includes traffic from
// apps that bypass RoutEngine.
func (s *Scheduler) LinkTotals() map[string][2]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][2]uint64, len(s.slots))
	for _, sl := range s.slots {
		if sl.present && sl.linkPrimed {
			out[sl.Name] = [2]uint64{sl.linkIn, sl.linkOut}
		}
	}
	return out
}

// LinkFeeder moves what each adapter carried into the usage history. Call Feed about once a second.
type LinkFeeder struct{ last map[string][2]uint64 }

// Feed adds the bytes each adapter carried since the previous call. The first call for an adapter
// only takes a baseline, and a counter that went backwards (the adapter reconnected) restarts it.
func (f *LinkFeeder) Feed(s *Scheduler, l *usage.Ledger) {
	if f.last == nil {
		f.last = map[string][2]uint64{}
	}
	for name, cur := range s.LinkTotals() {
		if prev, ok := f.last[name]; ok && cur[0] >= prev[0] && cur[1] >= prev[1] {
			l.AddAdapter(name, int64(cur[0]-prev[0]), int64(cur[1]-prev[1]))
		}
		f.last[name] = cur
	}
}

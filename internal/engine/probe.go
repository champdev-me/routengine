package engine

import (
	"net"
	"time"

	"routengine/internal/nics"
)

// Internet reachability probes: a TCP connect to a well-known anycast IP, bound to the NIC.
var probeTargets = [2][]string{
	Fam4: {"1.1.1.1:443", "8.8.8.8:443"},
	Fam6: {"[2606:4700:4700::1111]:443", "[2001:4860:4860::8888]:443"},
}

const (
	probeTimeout   = 2 * time.Second
	probeEveryUp   = 15 * time.Second
	probeEveryDown = 4 * time.Second
)

func probe(sl *Slot, f Family) bool {
	ip := sl.ip(f)
	if ip == nil {
		return false
	}
	network := "tcp4"
	if f == Fam6 {
		network = "tcp6"
	}
	for _, t := range probeTargets[f] {
		d := net.Dialer{LocalAddr: &net.TCPAddr{IP: ip}, Control: nics.ForceEgress(sl.Index, f == Fam6), Timeout: probeTimeout}
		start := time.Now()
		if c, err := d.Dial(network, t); err == nil {
			sl.noteRTT(f, time.Since(start))
			c.Close()
			return true
		}
	}
	return false
}

type probeJob struct {
	sl *Slot
	f  Family
}

// RunProbes checks every adapter's internet access per address family: new ones immediately,
// healthy ones occasionally, dead ones more often so they rejoin quickly. A tether with no data
// or a captive-portal Wi-Fi therefore never receives flows, and a recovered link is used again
// within seconds. Runs until stop is closed (nil = forever).
func (s *Scheduler) RunProbes(stop <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		for _, j := range s.probeDue() {
			go func() {
				defer j.sl.probing[j.f].Store(false)
				s.SetState(j.sl, j.f, probe(j.sl, j.f))
			}()
		}
	}
}

// probeDue returns the (slot, family) pairs that need a probe now and marks them in flight.
func (s *Scheduler) probeDue() []probeJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var due []probeJob
	for _, sl := range s.slots {
		if !sl.present {
			continue
		}
		if sl.kind != nics.KindInternet {
			// A VPN or a LAN-only adapter has no internet to check: a tailnet-only link would always look
			// "down". It counts as up while the adapter exists.
			for _, f := range []Family{Fam4, Fam6} {
				if sl.ip(f) != nil {
					sl.state[f] = stateUp
				}
			}
			continue
		}
		for _, f := range []Family{Fam4, Fam6} {
			if sl.ip(f) == nil || sl.probing[f].Load() {
				continue
			}
			every := probeEveryUp
			switch sl.state[f] {
			case stateUnknown:
				every = 0
			case stateDown:
				every = probeEveryDown
			}
			if now.Sub(sl.lastProbe[f]) >= every {
				sl.lastProbe[f] = now
				sl.probing[f].Store(true)
				due = append(due, probeJob{sl, f})
			}
		}
	}
	return due
}

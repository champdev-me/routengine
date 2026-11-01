package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// Speed is a speed cap in Mbit/s, one for each direction: Down is from the internet, Up is to it. 0 is no cap in
// that direction.
//
// In a rules file it is a number, the same cap both ways (how files written before 1.2.1 hold it, and how a cap
// that is the same both ways is still written, so an older RoutEngine can read it), or {"down": 50, "up": 10}
// where the two differ. A direction left out of the object has no cap.
type Speed struct {
	Down float64 `json:"down,omitempty"`
	Up   float64 `json:"up,omitempty"`
}

// Both is the same cap both ways.
func Both(mbitPerSec float64) Speed { return Speed{Down: mbitPerSec, Up: mbitPerSec} }

// Limits reports whether the speed caps either direction.
func (s Speed) Limits() bool { return s.Down > 0 || s.Up > 0 }

// String is the cap as the window and the log show it: "50 Mbit/s" both ways, "↓50 ↑10 Mbit/s" otherwise. A
// direction with no cap is left out: "↓50 Mbit/s" caps the download only.
func (s Speed) String() string {
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	switch {
	case !s.Limits():
		return "unlimited"
	case s.Down == s.Up:
		return num(s.Down) + " Mbit/s"
	case s.Up <= 0:
		return "↓" + num(s.Down) + " Mbit/s"
	case s.Down <= 0:
		return "↑" + num(s.Up) + " Mbit/s"
	}
	return "↓" + num(s.Down) + " ↑" + num(s.Up) + " Mbit/s"
}

// Check rejects a cap that is not a speed from 0 (no cap) to LimitMaxMbps in each direction.
func (s Speed) Check() error {
	for _, v := range []float64{s.Down, s.Up} {
		if v < 0 || v > LimitMaxMbps || math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("must be a speed in Mbit/s from 0 (no cap) up to %.0f", float64(LimitMaxMbps))
		}
	}
	return nil
}

func (s Speed) MarshalJSON() ([]byte, error) {
	if s.Down == s.Up {
		return json.Marshal(s.Down)
	}
	type plain Speed
	return json.Marshal(plain(s))
}

func (s *Speed) UnmarshalJSON(b []byte) error {
	var both float64
	if err := json.Unmarshal(b, &both); err == nil {
		*s = Both(both)
		return nil
	}
	type plain Speed
	var p plain
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields() // "download" for "down" must not silently become no cap
	if err := dec.Decode(&p); err != nil {
		return errors.New(`a speed cap is a number in Mbit/s (both ways) or {"down": 50, "up": 10}`)
	}
	*s = Speed(p)
	return nil
}

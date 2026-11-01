package engine

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// LimitMaxMbps is the largest speed cap accepted, in Mbit/s.
const LimitMaxMbps = 1e6

// ParseLimit reads a speed cap written by a person: "10", "7.5", "0", "off" or "unlimited". It returns
// 0 for no cap. Everything that reads a cap from a person uses it, so all accept the same spellings and
// reject the same nonsense.
func ParseLimit(s string) (float64, error) {
	t := strings.TrimSpace(s)
	switch strings.ToLower(t) {
	case "unlimited", "off", "none":
		return 0, nil
	}
	mbitPerSec, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(mbitPerSec) || math.IsInf(mbitPerSec, 0) {
		return 0, fmt.Errorf("enter a speed in Mbit/s, like 50 or 7.5 (0 for unlimited)")
	}
	if mbitPerSec < 0 || mbitPerSec > LimitMaxMbps {
		return 0, fmt.Errorf("the speed must be between 0 and %.0f Mbit/s", float64(LimitMaxMbps))
	}
	return mbitPerSec, nil
}

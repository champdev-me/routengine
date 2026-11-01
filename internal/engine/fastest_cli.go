//go:build nogui

package engine

// The command line has no fastest-adapter choice (a Pro feature of the window apps): flows are balanced as usual.
func (b *Balancer) fastestPick(Family, func(*Slot) bool) func(tried map[string]struct{}) *Slot {
	return nil
}

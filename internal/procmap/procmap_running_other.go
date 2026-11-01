//go:build !windows && !linux

package procmap

// Running lists the programs that are open now. It is not implemented on this platform yet, so the window knows only
// the programs whose traffic RoutEngine has seen.
func Running() []string { return nil }

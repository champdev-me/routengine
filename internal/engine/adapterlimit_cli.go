//go:build nogui

package engine

import "io"

// The command line has no speed limit per adapter (a Pro feature of the window apps, adapterlimit_pro.go): nothing is
// held back per adapter, and an app's speed limit (limit.go) is the only cap.

// limitPair is never installed here; it exists so the scheduler's field reads the same in both builds.
type limitPair struct{ chunkIn int }

func (sl *Slot) setLimit(Speed)                                          {}
func (sl *Slot) limitSpeed() Speed                                       { return Speed{} }
func (sl *Slot) throttle(bool, int)                                      {}
func (sl *Slot) throttlePacket(bool, int)                                {}
func (sl *Slot) writePaced(dst io.Writer, p []byte, _ bool) (int, error) { return dst.Write(p) }

// SetAdapterLimits and AdapterLimit are an app's own caps per adapter: none in the command line.
func (m *AppMeter) SetAdapterLimits(map[string]Speed) {}
func (m *AppMeter) AdapterLimit(string) Speed         { return Speed{} }

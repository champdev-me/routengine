// Package procmap finds which process owns a flow. In system-wide mode the app's socket is bound to
// the virtual adapter, so the flow's source address and port are that socket's local end, and the
// OS's connection tables name the owning process.
package procmap

import "strings"

// Bundle is the outermost .app folder in a program's path, such as "/Applications/Safari.app", or "". A
// macOS app is its whole folder: a browser's network traffic comes from helper programs inside it.
func Bundle(exe string) string {
	p := strings.ReplaceAll(exe, `\`, "/")
	if i := strings.Index(strings.ToLower(p), ".app/"); i >= 0 {
		return p[:i+4]
	}
	return ""
}

// Proc is a process that owns a socket.
type Proc struct {
	PID uint32
	Exe string // full image path; empty when the process could not be opened
}

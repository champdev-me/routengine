package procmap

// Running lists the full path of every program that is open now, once each. A program that was already open when
// RoutEngine started has no flow in the engine until it makes a new connection, so the window asks this to know it
// is open. Another user's programs are only visible to root; the ones that cannot be read are left out.
func Running() []string {
	seen := map[string]bool{}
	var out []string
	for _, pid := range pids() {
		if pid <= 0 {
			continue
		}
		if exe := pathOf(pid); exe != "" && !seen[exe] {
			seen[exe] = true
			out = append(out, exe)
		}
	}
	return out
}

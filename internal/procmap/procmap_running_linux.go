package procmap

import (
	"os"
	"strconv"
	"strings"
)

// Running lists the full image path of every program that is open now, once each. A program that was already
// open when RoutEngine started has no flow in the engine until it makes a new connection, so the window asks this
// to know it is open. Another user's programs are only visible to root; the ones that cannot be read are left out.
func Running() []string {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // not a process
		}
		exe, err := os.Readlink("/proc/" + e.Name() + "/exe")
		if err != nil || exe == "" {
			continue
		}
		exe = strings.TrimSuffix(exe, " (deleted)") // the file was replaced while the program ran
		if !seen[exe] {
			seen[exe] = true
			out = append(out, exe)
		}
	}
	return out
}

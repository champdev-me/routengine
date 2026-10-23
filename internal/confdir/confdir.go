// Package confdir says where RoutEngine keeps its files (rules, license, settings).
package confdir

import (
	"os"
	"path/filepath"
	"runtime"
)

// Dir is $ROUTENGINE_DIR if set. On Linux, root (sudo, or a systemd service) uses /etc/routengine, so
// `sudo routengine license add` and the service see the same files. On macOS, root uses the logged-in user's
// folder. Otherwise it is the user's config folder.
func Dir() string {
	if d := os.Getenv("ROUTENGINE_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return "/etc/routengine"
	}
	if d, ok := platformDir(); ok {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "routengine")
}

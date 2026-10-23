package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"routengine/internal/engine"
	"routengine/internal/usage"
)

// The elevated process can't be signalled from a normal shell, so control goes through files in
// a per-user directory: status.json (written once a second while on) and marker files
// ("on", "off", "quit") that the running instance consumes.

type Status struct {
	PID     int
	Mode    string
	V6      string
	Since   time.Time
	Updated time.Time
	NICs    []engine.SlotStats
	Apps    []engine.AppStats // per-application speeds, for the UI
	Usage   usage.Report      // data used today and this month, per app and per adapter
}

func DefaultDir() string {
	// The macOS tray runs as root but is commanded by the user, so its state can't live in a
	// user-owned directory (root would follow a planted symlink): a fixed root-owned path.
	if runtime.GOOS == "darwin" {
		return "/var/run/routengine"
	}
	// Same on Linux for the root service (systemd has no $HOME), which users command through ctl/.
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/run/routengine/status.json"); os.Geteuid() == 0 || err == nil {
			return "/run/routengine"
		}
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "routengine")
}

// Prepare creates the state dir, with markers going in a sticky world-writable ctl/ inside it, so
// a normal user can drop one without being able to touch anything else there.
func Prepare(dir string) error {
	ctl := filepath.Join(dir, "ctl")
	if err := os.MkdirAll(ctl, 0o755); err != nil {
		return err
	}
	return os.Chmod(ctl, os.ModeSticky|0o777)
}

func Write(dir string, st Status) {
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	tmp := filepath.Join(dir, "status.json.tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, filepath.Join(dir, "status.json"))
	}
}

func Clear(dir string) { os.Remove(filepath.Join(dir, "status.json")) }

// Read returns the status only if it is fresh, i.e. a live process is writing it.
func Read(dir string) (*Status, bool) {
	b, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		return nil, false
	}
	var st Status
	if json.Unmarshal(b, &st) != nil || time.Since(st.Updated) > 3*time.Second {
		return nil, false
	}
	return &st, true
}

func SendMarker(dir, name string) error {
	if err := os.MkdirAll(filepath.Join(dir, "ctl"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "ctl", name), nil, 0o666)
}

// TakeMarker consumes a marker, reporting whether it was there.
func TakeMarker(dir, name string) bool { return os.Remove(filepath.Join(dir, "ctl", name)) == nil }

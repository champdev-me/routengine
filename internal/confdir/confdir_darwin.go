package confdir

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
)

// consoleHome is the logged-in user's folder when this process runs as root (the app is started with an
// administrator prompt), so the app and a terminal command see the same rules and license key.
func consoleHome() (string, bool) {
	var st syscall.Stat_t
	if os.Geteuid() != 0 || syscall.Stat("/dev/console", &st) != nil || st.Uid == 0 {
		return "", false
	}
	u, err := user.LookupId(strconv.Itoa(int(st.Uid)))
	if err != nil {
		return "", false
	}
	return u.HomeDir, true
}

var own sync.Once

// platformDir is the console user's Application Support folder for root, created for that user so
// they can still write there.
func platformDir() (string, bool) {
	home, ok := consoleHome()
	if !ok {
		return "", false
	}
	dir := filepath.Join(home, "Library", "Application Support", "routengine")
	own.Do(func() {
		var st syscall.Stat_t
		if os.MkdirAll(dir, 0o755) == nil && syscall.Stat(home, &st) == nil {
			os.Chown(dir, int(st.Uid), int(st.Gid))
		}
	})
	return dir, true
}

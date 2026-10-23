package platform

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// IsAdmin reports whether this process may create the utun and its routes, i.e. runs as root.
func IsAdmin() bool { return os.Geteuid() == 0 }

// The single-instance lock is a flock on a root-created file: visible to every user, and
// released by the kernel when the process dies, so a crash never leaves a stale lock behind.
const lockPath = "/var/run/routengine.lock"

var lockFile *os.File

// SingleInstance is false if another RoutEngine (run or tray) already owns the utun.
// The file is intentionally kept open for the life of the process.
func SingleInstance() bool {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false
	}
	if unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		f.Close()
		return false
	}
	lockFile = f
	return true
}

// InstanceRunning reports whether a RoutEngine holds the lock; works for any user, since the
// file is world-readable and flock doesn't need write access.
func InstanceRunning() bool {
	f, err := os.Open(lockPath)
	if err != nil {
		return false
	}
	defer f.Close()
	if unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB) != nil {
		return true
	}
	unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return false
}

// RedirectStd points stdout/stderr (including the runtime's panic output) at f, for a process
// started with no terminal (LaunchAgent).
func RedirectStd(f *os.File) {
	os.Stdout, os.Stderr = f, f
	log.SetOutput(f) // the std logger captured the old stderr at init; the tray library logs through it
	unix.Dup2(int(f.Fd()), 1)
	unix.Dup2(int(f.Fd()), 2)
}

// Where `routengine install` puts the root-owned copy that sudoers allows: /usr/local/bin when only
// root can write there, else /opt/routengine/bin (Intel Macs, where Homebrew owns /usr/local/bin).
const (
	exePreferred = "/usr/local/bin/routengine"
	exeFallback  = "/opt/routengine/bin/routengine"
)

// InstalledExe is the root-owned copy `routengine install` made.
func InstalledExe() string {
	if _, err := os.Stat(exeFallback); err == nil {
		return exeFallback
	}
	return exePreferred
}

// RelaunchElevated replaces this process with itself run as root: sudo in a terminal, the
// administrator password dialog otherwise. It only returns on failure.
func RelaunchElevated(args []string, hidden bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if HasConsole() {
		return syscall.Exec("/usr/bin/sudo", append([]string{"sudo", exe}, args...), os.Environ())
	}
	cmd := shQuote(exe)
	for _, a := range args {
		cmd += " " + shQuote(a)
	}
	// Detached: the dialog returns at once and the root process keeps running without us.
	script := `do shell script "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(cmd+" >/dev/null 2>&1 &") + `" with administrator privileges`
	if out, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput(); err != nil {
		return fmt.Errorf("administrator prompt failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// RelaunchTray is RelaunchElevated for the tray, but first tries the passwordless sudoers rule
// that `sudo routengine install` sets up. That rule matches exactly `routengine tray`, so it only
// applies when the tray was started without flags.
func RelaunchTray(args []string, hidden bool) error {
	if len(args) == 1 && args[0] == "tray" {
		if exe := InstalledExe(); exec.Command("/usr/bin/sudo", "-n", "-l", exe, "tray").Run() == nil {
			return syscall.Exec("/usr/bin/sudo", []string{"sudo", "-n", exe, "tray"}, os.Environ())
		}
	}
	return RelaunchElevated(args, hidden)
}

func shQuote(s string) string { return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'` }

// HasConsole reports whether stderr is a terminal.
func HasConsole() bool {
	_, err := unix.IoctlGetTermios(2, unix.TIOCGETA)
	return err == nil
}

// ShowError reports a fatal error to a user with no terminal, as a notification (the error is
// in the log file either way).
func ShowError(msg string) {
	exec.Command("/usr/bin/osascript",
		"-e", "on run argv", "-e", `display notification (item 1 of argv) with title "RoutEngine"`, "-e", "end run",
		msg).Run()
}

// CheckDriver is a no-op: utun is built into macOS.
func CheckDriver() error { return nil }

// WaitForTray is a no-op: the menu bar exists as soon as the login session does.
func WaitForTray(max time.Duration) bool { return true }

package platform

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// IsAdmin reports whether this process may create the TUN and its routes, i.e. holds
// CAP_NET_ADMIN: as root, or through file capabilities (setcap, or NixOS's security.wrappers).
func IsAdmin() bool {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return os.Geteuid() == 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "CapEff:"); ok {
			caps, err := strconv.ParseUint(strings.TrimSpace(v), 16, 64)
			return err == nil && caps&(1<<unix.CAP_NET_ADMIN) != 0
		}
	}
	return os.Geteuid() == 0
}

// The single-instance lock is an abstract unix socket: visible to every user, and released by the
// kernel when the process dies, so a crash never leaves a stale lock behind.
const lockAddr = "@routengine"

var lock net.Listener

// SingleInstance is false if another RoutEngine (run or tray) already owns the TUN.
// The socket is intentionally kept for the life of the process.
func SingleInstance() bool {
	ln, err := net.Listen("unix", lockAddr)
	if err != nil {
		return false
	}
	lock = ln
	go func() { // InstanceRunning connects to check; answer by hanging up
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return true
}

// InstanceRunning reports whether a RoutEngine holds the single-instance lock.
func InstanceRunning() bool {
	c, err := net.DialTimeout("unix", lockAddr, time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// RedirectStd points stdout/stderr (including the runtime's panic output) at f, for a process
// started with no terminal (autostart, systemd).
func RedirectStd(f *os.File) {
	os.Stdout, os.Stderr = f, f
	log.SetOutput(f) // the std logger captured the old stderr at init; the tray library logs through it
	unix.Dup3(int(f.Fd()), 1, 0)
	unix.Dup3(int(f.Fd()), 2, 0)
}

// RelaunchElevated replaces this process with itself run as root: sudo in a terminal, pkexec
// (a graphical password prompt) otherwise. It only returns on failure.
func RelaunchElevated(args []string, hidden bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	tool := "pkexec"
	if HasConsole() {
		tool = "sudo"
	}
	path, err := exec.LookPath(tool)
	if err != nil {
		return fmt.Errorf("%s not found; run RoutEngine as root or give it CAP_NET_ADMIN (see README)", tool)
	}
	return syscall.Exec(path, append([]string{tool, exe}, args...), os.Environ())
}

// RelaunchTray is RelaunchElevated for the tray. On Linux the tray icon is a D-Bus service in the
// user's desktop session, which a process restarted as root can't reach, so the tray has to run
// as the user with CAP_NET_ADMIN granted to the binary instead.
func RelaunchTray(args []string, hidden bool) error {
	exe, _ := os.Executable()
	if strings.HasPrefix(exe, "/nix/store/") { // read-only: capabilities come from a NixOS security wrapper
		return errors.New("the tray needs CAP_NET_ADMIN: enable programs.routengine in your NixOS configuration " +
			"and run /run/wrappers/bin/routengine (see README). `sudo routengine run` works without it")
	}
	return errors.New("the tray needs CAP_NET_ADMIN. Grant it once with:\n" +
		"  sudo setcap cap_net_admin,cap_net_raw+ep " + exe + "\n" +
		"`sudo routengine run` works without it")
}

// HasConsole reports whether stderr is a terminal.
func HasConsole() bool {
	_, err := unix.IoctlGetTermios(2, unix.TCGETS)
	return err == nil
}

// ShowError reports a fatal error to a user with no terminal, as a desktop notification if
// notify-send is installed (the error is in the log file either way).
func ShowError(msg string) {
	if path, err := exec.LookPath("notify-send"); err == nil {
		exec.Command(path, "--urgency=critical", "--app-name=RoutEngine", "RoutEngine", msg).Run()
	}
}

// CheckDriver reports whether the kernel's TUN driver is available.
func CheckDriver() error {
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		return fmt.Errorf("/dev/net/tun not found (try: sudo modprobe tun): %w", err)
	}
	return nil
}

// WaitForTray is a no-op on Linux: the tray library registers with the desktop's
// StatusNotifierWatcher over D-Bus whenever it is available.
func WaitForTray(max time.Duration) bool { return true }

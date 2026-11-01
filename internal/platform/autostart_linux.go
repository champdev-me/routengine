package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// autostartFile is an XDG autostart entry, honoured by GNOME, KDE, XFCE, Cinnamon, LXQt and others
// at sign-in. On NixOS the module's programs.routengine.autostart (a systemd user service) is an
// alternative that doesn't depend on the desktop.
func autostartFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", "routengine.desktop"), nil
}

// trayExe prefers routengine on PATH: on NixOS that is the capability wrapper in /run/wrappers/bin,
// while os.Executable is the store path, which doesn't carry the capabilities.
func trayExe() string {
	if p, err := exec.LookPath("routengine"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
	}
	exe, _ := os.Executable()
	return exe
}

// desktopQuote quotes an Exec argument as the Desktop Entry spec requires.
func desktopQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\$`") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\\\`, `"`, `\"`, "`", "\\`", "$", `\$`).Replace(s) + `"`
}

// Autostart turns start-at-sign-in "on" or "off", or prints its "status".
func Autostart(action string) error {
	path, err := autostartFile()
	if err != nil {
		return err
	}
	switch action {
	case "status":
		if AutostartEnabled() {
			fmt.Println("autostart is ON (" + path + ").")
		} else {
			fmt.Println("autostart is OFF.")
		}
		return nil
	case "off":
		if !AutostartEnabled() {
			fmt.Println("autostart is already OFF.")
			return nil
		}
		if err := SetAutostart(false); err != nil {
			return err
		}
		fmt.Println("autostart is OFF.")
		return nil
	}
	if err := SetAutostart(true); err != nil {
		return err
	}
	fmt.Println("autostart is ON: RoutEngine starts at sign-in (" + path + ").")
	if !IsAdmin() {
		fmt.Println("note: the tray needs CAP_NET_ADMIN to start merging; see `routengine tray` or the README.")
	}
	return nil
}

// AutostartEnabled reports whether RoutEngine starts at sign-in.
func AutostartEnabled() bool {
	path, err := autostartFile()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// SetAutostart writes or removes the autostart entry. The tray runs as the signed-in user (with
// CAP_NET_ADMIN), so that user's config directory is the right one.
func SetAutostart(on bool) error {
	path, err := autostartFile()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=RoutEngine\n" +
		"Comment=Use all your network adapters at once\n" +
		"Exec=" + desktopQuote(trayExe()) + " tray\n" +
		"Terminal=false\n" +
		"X-GNOME-Autostart-enabled=true\n"
	return os.WriteFile(path, []byte(entry), 0o644)
}

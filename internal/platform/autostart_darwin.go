package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
)

const agentLabel = "net.routengine.RoutEngine"

// signInUser is the account whose sign-in starts RoutEngine. The tray runs as root through sudo,
// so there it is SUDO_UID's account, not root's.
func signInUser() (*user.User, error) {
	if os.Geteuid() != 0 {
		return user.Current()
	}
	uid := os.Getenv("SUDO_UID")
	if uid == "" || uid == "0" {
		return nil, errors.New("start at sign-in needs the tray started from your account (sudo sets SUDO_UID)")
	}
	return user.LookupId(uid)
}

func agentPath(u *user.User) string {
	return filepath.Join(u.HomeDir, "Library", "LaunchAgents", agentLabel+".plist")
}

// Autostart turns start-at-sign-in "on" or "off", or prints its "status". It is a LaunchAgent
// running `sudo -n routengine tray`, which only works once `sudo routengine install` has added the
// passwordless rule; otherwise sign-in would need a password the agent can't type.
func Autostart(action string) error {
	if os.Geteuid() == 0 {
		return errors.New("run this as your normal user, not with sudo")
	}
	u, err := signInUser()
	if err != nil {
		return err
	}
	path := agentPath(u)
	domain := "gui/" + u.Uid
	switch action {
	case "status":
		if AutostartEnabled() {
			fmt.Println("autostart is ON (" + path + ").")
		} else {
			fmt.Println("autostart is OFF.")
		}
		return nil
	case "off":
		exec.Command("/bin/launchctl", "bootout", domain+"/"+agentLabel).Run() // not loaded is fine
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
	exec.Command("/bin/launchctl", "bootout", domain+"/"+agentLabel).Run()
	if out, err := exec.Command("/bin/launchctl", "bootstrap", domain, path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w: %s", err, out)
	}
	fmt.Println("autostart is ON: RoutEngine starts at sign-in (" + path + ").")
	return nil
}

// AutostartEnabled reports whether RoutEngine starts at sign-in.
func AutostartEnabled() bool {
	u, err := signInUser()
	if err != nil {
		return false
	}
	_, err = os.Stat(agentPath(u))
	return err == nil
}

// SetAutostart writes or removes the LaunchAgent; it takes effect at the next sign-in. It works
// from the root tray too, writing into the user's home with the user as owner. It does not unload
// the agent: if the agent started this tray, unloading it would quit the tray.
func SetAutostart(on bool) error {
	u, err := signInUser()
	if err != nil {
		return err
	}
	path := agentPath(u)
	if !on {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	check := []string{"-n", "-l", InstalledExe(), "tray"}
	if os.Geteuid() == 0 {
		check = append([]string{"-U", u.Username}, check...) // root may always; ask about the user
	}
	if exec.Command("/usr/bin/sudo", check...).Run() != nil {
		return errors.New("the tray can't start at sign-in without the passwordless rule; run once: sudo routengine install")
	}
	log := filepath.Join(u.HomeDir, "Library", "Logs", "routengine.log")
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + agentLabel + `</string>
<key>ProgramArguments</key><array><string>/usr/bin/sudo</string><string>-n</string><string>` + InstalledExe() + `</string><string>tray</string></array>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
<key>ThrottleInterval</key><integer>30</integer>
<key>StandardOutPath</key><string>` + log + `</string>
<key>StandardErrorPath</key><string>` + log + `</string>
</dict></plist>
`
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		os.Chown(dir, uid, gid)
		return os.Chown(path, uid, gid)
	}
	return nil
}

package platform

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Start at sign-in for the macOS app. RoutEngine needs administrator rights, and a login item cannot type a password,
// so ticking the box (in the app, which already runs as root) sets up what net-merge's `install` does for its tray:
//
//   - a root-owned copy of the app in startupApp. The passwordless rule must name a program the user cannot change,
//     or any program running as the user could replace it and get root; the copy in /Applications is the user's.
//   - a sudoers rule that lets the signed-in user start that copy, with exactly "app" or "app -minimized", and nothing
//     else (a flag such as -log would let it write any file as root).
//   - a login item (LaunchAgent) that runs it with `sudo -n` at sign-in, in the menu bar.
//
// A launch from Finder then uses the rule too: no password prompt, and through sudo the root copy stays in the user's
// session, so its menu bar icon shows.
const (
	startupDir   = "/Library/RoutEngine"
	startupApp   = startupDir + "/RoutEngine.app"
	startupExe   = startupApp + "/Contents/MacOS/RoutEngine"
	startupRule  = "/etc/sudoers.d/routengine-app"
	startupLabel = "net.routengine.app"
)

// consoleUser is the account signed in at the screen: the one whose sign-in should start RoutEngine. The app runs as
// root, so the current user is not it.
func consoleUser() (*user.User, error) {
	if os.Geteuid() != 0 {
		return user.Current()
	}
	var st syscall.Stat_t
	if err := syscall.Stat("/dev/console", &st); err != nil || st.Uid == 0 {
		return nil, errors.New("no one is signed in at the screen")
	}
	return user.LookupId(strconv.Itoa(int(st.Uid)))
}

func appAgentPath(u *user.User) string {
	return filepath.Join(u.HomeDir, "Library", "LaunchAgents", startupLabel+".plist")
}

// AppAutostartEnabled reports whether the app starts at sign-in.
func AppAutostartEnabled() bool {
	u, err := consoleUser()
	if err != nil {
		return false
	}
	_, err = os.Stat(appAgentPath(u))
	return err == nil
}

// SetAppAutostart sets up, or removes, the start at sign-in described above. It needs root: the app has it.
func SetAppAutostart(on bool) error {
	if os.Geteuid() != 0 {
		return errors.New("start at sign-in is set up by RoutEngine running with administrator rights")
	}
	u, err := consoleUser()
	if err != nil {
		return err
	}
	if !on {
		os.Remove(appAgentPath(u))
		os.Remove(startupRule)
		os.RemoveAll(startupApp)
		return nil
	}
	if err := copyStartupApp(); err != nil {
		return fmt.Errorf("could not copy RoutEngine to %s: %w", startupDir, err)
	}
	if err := writeStartupRule(u.Username); err != nil {
		os.RemoveAll(startupApp)
		return err
	}
	if err := writeStartupAgent(u); err != nil {
		os.Remove(startupRule)
		os.RemoveAll(startupApp)
		return err
	}
	return nil
}

// runningApp is the .app the running program belongs to, or "" when it is not in one.
func runningApp() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		return exe[:i+4]
	}
	return ""
}

// copyStartupApp puts a root-owned copy of the running app in startupApp, replacing an older one.
func copyStartupApp() error {
	src := runningApp()
	if src == "" {
		return errors.New("RoutEngine is not running from its app")
	}
	if src == startupApp {
		return nil // already the copy
	}
	if err := os.MkdirAll(startupDir, 0o755); err != nil {
		return err
	}
	tmp := startupApp + ".new"
	os.RemoveAll(tmp)
	steps := [][]string{
		{"/usr/bin/ditto", src, tmp},
		{"/usr/sbin/chown", "-R", "root:wheel", tmp},
		{"/bin/chmod", "-R", "go-w", tmp},
		{"/usr/bin/xattr", "-dr", "com.apple.quarantine", tmp},
	}
	for _, s := range steps {
		if out, err := exec.Command(s[0], s[1:]...).CombinedOutput(); err != nil && s[0] != "/usr/bin/xattr" {
			os.RemoveAll(tmp)
			return fmt.Errorf("%s: %v %s", filepath.Base(s[0]), err, strings.TrimSpace(string(out)))
		}
	}
	os.RemoveAll(startupApp)
	return os.Rename(tmp, startupApp)
}

// RefreshStartupApp brings the root-owned copy up to date with the app running now (an update installed in
// /Applications), so start at sign-in keeps starting the new version. Nothing happens unless start at sign-in is on.
func RefreshStartupApp() {
	if os.Geteuid() != 0 || !AppAutostartEnabled() {
		return
	}
	exe, err := os.Executable()
	if err != nil || exe == startupExe || runningApp() == "" {
		return
	}
	if same, _ := sameFile(exe, startupExe); !same {
		copyStartupApp()
	}
}

// startupRuleText is the sudoers rule: the user may start the root copy with exactly these arguments, nothing else.
func startupRuleText(username string) string {
	return "# RoutEngine: starts at sign-in, and opens from Finder, without a password. Written by RoutEngine;\n" +
		"# untick Start at sign-in in its Settings to remove it.\n" +
		username + " ALL=(root) NOPASSWD: " + startupExe + " app, " + startupExe + " app -minimized\n"
}

func writeStartupRule(username string) error {
	if strings.ContainsAny(username, " \t\n,:=\\#\"'") {
		return fmt.Errorf("the account name %q cannot go in a sudoers rule", username)
	}
	tmp := startupRule + ".new"
	if err := os.WriteFile(tmp, []byte(startupRuleText(username)), 0o440); err != nil {
		return err
	}
	if out, err := exec.Command("/usr/sbin/visudo", "-cf", tmp).CombinedOutput(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("the sudoers rule did not check out: %s", strings.TrimSpace(string(out)))
	}
	os.Chown(tmp, 0, 0)
	return os.Rename(tmp, startupRule)
}

func writeStartupAgent(u *user.User) error {
	path := appAgentPath(u)
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + startupLabel + `</string>
<key>ProgramArguments</key><array><string>/usr/bin/sudo</string><string>-n</string><string>` + startupExe + `</string><string>app</string><string>-minimized</string></array>
<key>RunAtLoad</key><true/>
<key>ProcessType</key><string>Interactive</string>
</dict></plist>
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return os.Chown(path, uid, gid)
}

// startupRelaunch starts the root copy through the passwordless rule, when it is set up and holds the same program as
// the app that was opened (after an update it does not yet, and the password prompt runs the new version, which then
// refreshes the copy). It exits this process once the copy has started, and returns an error when it could not, so
// the password prompt can run instead. `sudo -l` cannot tell beforehand: on macOS it answers yes for any command an
// administrator may run with a password.
func startupRelaunch(args []string) error {
	if len(args) == 0 || args[0] != "app" || len(args) > 2 || (len(args) == 2 && args[1] != "-minimized") {
		return errors.New("not a launch the rule allows")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if same, err := sameFile(exe, startupExe); err != nil || !same {
		return errors.New("start at sign-in is not set up for this version")
	}
	cmd := exec.Command("/usr/bin/sudo", append([]string{"-n", startupExe}, args...)...)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			os.Exit(0) // it found a RoutEngine already running and asked it to open its window
		}
		return fmt.Errorf("the passwordless start did not work: %v", err) // no rule: sudo asked for a password
	case <-time.After(2 * time.Second):
		os.Exit(0) // it is running, as root, in this login session
	}
	return nil
}

// sameFile reports whether two files have the same content.
func sameFile(a, b string) (bool, error) {
	ha, err := fileHash(a)
	if err != nil {
		return false, err
	}
	hb, err := fileHash(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ha, hb), nil
}

func fileHash(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

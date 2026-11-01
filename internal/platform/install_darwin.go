package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const sudoersPath = "/etc/sudoers.d/routengine"

var safeUser = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Install (run as `sudo routengine install`) copies this binary to a root-owned path and lets the
// invoking user run exactly `routengine tray` and `routengine cleanup` there without a password.
// No wildcards: with `tray *` any process of the user could pass `-log /etc/...` and make root
// overwrite files. Uninstall removes both.
func Install(uninstall bool) error {
	if !IsAdmin() {
		return errors.New("run: sudo routengine install")
	}
	if uninstall {
		exe := InstalledExe()
		removeOurLink()
		for _, p := range []string{sudoersPath, exePreferred} {
			if p == exePreferred && !rootFile(p) {
				continue // Homebrew's link or someone else's file, not our copy
			}
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := os.RemoveAll(filepath.Dir(filepath.Dir(exeFallback))); err != nil {
			return err
		}
		fmt.Println("removed " + sudoersPath + " and " + exe + ". Turn off start at sign-in with `routengine autostart off`.")
		return nil
	}
	user := os.Getenv("SUDO_USER")
	if user == "" || user == "root" || !safeUser.MatchString(user) {
		return errors.New("run it through sudo from your own account: sudo routengine install")
	}
	exe := exePreferred
	if rootOnly(filepath.Dir(exePreferred)) != nil {
		exe = exeFallback
		if err := os.MkdirAll(filepath.Dir(exeFallback), 0o755); err != nil {
			return err
		}
	}
	if err := copyRootOwned(exe); err != nil {
		return err
	}
	if exe == exeFallback {
		linkIntoPath()
	} else {
		removeOurLink()
		os.RemoveAll(filepath.Dir(filepath.Dir(exeFallback))) // an older fallback copy would shadow this one
	}
	rule := fmt.Sprintf("# Written by `routengine install`: %s may start the tray without a password.\n"+
		"%s ALL=(root) NOPASSWD: %s tray, %s cleanup\n", user, user, exe, exe)
	tmp := sudoersPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(rule), 0o440); err != nil {
		return err
	}
	defer os.Remove(tmp)
	// A broken sudoers file locks everyone out of sudo, so only move it into place once it parses.
	if out, err := exec.Command("/usr/sbin/visudo", "-cf", tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("sudoers rule rejected, nothing installed: %s", strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmp, sudoersPath); err != nil {
		return err
	}
	fmt.Printf("installed %s and %s.\nStart the tray with `routengine tray` (no password), and at sign-in with `routengine autostart on`.\n", exe, sudoersPath)
	return nil
}

// rootOnly fails if dir or any directory above it could be changed by a non-root user.
func rootOnly(dir string) error {
	for d := dir; d != "/"; d = filepath.Dir(d) {
		fi, err := os.Stat(d)
		if err != nil {
			return err
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Uid != 0 || fi.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%s is writable by non-root users; refusing to install a root-run binary there", d)
		}
	}
	return nil
}

// rootFile reports whether p is a regular file owned by root: a copy `routengine install` made.
func rootFile(p string) bool {
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == 0
}

// linkIntoPath makes `RoutEngine` in /usr/local/bin point at the fallback copy, unless something
// else (Homebrew's link) is there already. It replaces an old root-owned copy from an earlier install.
func linkIntoPath() {
	if fi, err := os.Lstat(exePreferred); err == nil {
		if target, _ := os.Readlink(exePreferred); fi.Mode()&os.ModeSymlink != 0 && target == exeFallback {
			return
		}
		if !rootFile(exePreferred) {
			return
		}
		os.Remove(exePreferred)
	}
	os.Symlink(exeFallback, exePreferred) // best effort: the tray finds the copy without it
}

// removeOurLink removes the /usr/local/bin link linkIntoPath made.
func removeOurLink() {
	if target, err := os.Readlink(exePreferred); err == nil && target == exeFallback {
		os.Remove(exePreferred)
	}
}

// copyRootOwned puts this binary at dst, root:wheel 0755, refusing if any directory above it
// could be changed by a non-root user (the copy is run as root, so that would be a root hole).
func copyRootOwned(dst string) error {
	if err := rootOnly(filepath.Dir(dst)); err != nil {
		return err
	}
	src, err := os.Executable()
	if err != nil {
		return err
	}
	if src == dst {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chown(tmp, 0, 0); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

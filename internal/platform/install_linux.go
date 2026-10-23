package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

const (
	unitPath = "/etc/systemd/system/routengine.service"
	binPath  = "/usr/local/bin/routengine"
)

const unit = `[Unit]
Description=RoutEngine per-app routing
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=` + binPath + ` run -stats 0
ExecStopPost=-` + binPath + ` cleanup
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`

// Install (`sudo routengine install`) copies this program to /usr/local/bin and runs it as a systemd
// service from boot, with the rules in /etc/routengine. Uninstall stops and removes both.
func Install(uninstall bool) error {
	if os.Geteuid() != 0 {
		return errors.New("run it as root: sudo routengine install")
	}
	if uninstall {
		exec.Command("systemctl", "disable", "--now", "routengine").Run()
		for _, p := range []string{unitPath, binPath} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		exec.Command("systemctl", "daemon-reload").Run()
		fmt.Println("RoutEngine service removed. Your files in /etc/routengine are kept.")
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self != binPath {
		if err := copyExe(self, binPath); err != nil {
			return err
		}
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll("/etc/routengine", 0o755); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", "routengine"}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %v: %s", args[0], err, out)
		}
	}
	fmt.Println("RoutEngine is running and starts at boot. Rules: /etc/routengine/config.json. Logs: journalctl -u routengine")
	return nil
}

// copyExe writes src over dst through a temporary file, so a running copy is replaced, not corrupted.
func copyExe(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

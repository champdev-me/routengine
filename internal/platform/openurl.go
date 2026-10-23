package platform

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// OpenURL opens an https address in the user's browser. On Windows it asks Explorer to do it, so the
// browser starts as the signed-in user and not with this program's administrator rights.
func OpenURL(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("refusing to open %q", url)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

package license

import (
	"os/exec"
	"strings"
)

// fingerprint is the Mac's hardware UUID: it survives macOS updates and reinstalls on the same Mac.
func fingerprint() string {
	out, err := exec.Command("/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "IOPlatformUUID") {
			if f := strings.Split(line, `"`); len(f) >= 4 {
				return "uuid=" + strings.ToLower(f[len(f)-2])
			}
		}
	}
	return ""
}

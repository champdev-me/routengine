//go:build !windows && !darwin

package license

import (
	"os"
	"strings"
)

func fingerprint() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			return "id=" + strings.TrimSpace(string(b))
		}
	}
	return ""
}

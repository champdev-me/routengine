package update

import (
	"regexp"
	"testing"
)

// The server counts installs by this exact shape (server/src/installs.ts).
func TestUserAgentShape(t *testing.T) {
	if ok, _ := regexp.MatchString(`^RoutEngine/1\.1\.1 \((windows|linux|darwin); (amd64|arm64)\)$`, userAgent("1.1.1")); !ok {
		t.Fatalf("user agent %q does not match what the server reads", userAgent("1.1.1"))
	}
}

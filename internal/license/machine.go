package license

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

// MachineCode is a short code for this PC, such as "7QK4-M2XD-9PTA". A buyer sends it to the seller,
// who signs a license for exactly that code. It is a one-way hash, so it reveals nothing about the PC.
func MachineCode() string { return codeFor(fingerprint()) }

func codeFor(fp string) string {
	if fp == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("RoutEngine-machine-v1|" + fp))
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:8])[:12]
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12]
}

// NormalizeCode makes a typed or pasted machine code comparable: upper case, with the dashes in place.
func NormalizeCode(c string) string {
	c = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(c)))
	if len(c) != 12 {
		return c
	}
	return c[0:4] + "-" + c[4:8] + "-" + c[8:12]
}

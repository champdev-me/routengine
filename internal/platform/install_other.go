//go:build !darwin && !linux

package platform

import "errors"

// Install is the macOS-only setup of the passwordless tray (see install_darwin.go).
func Install(uninstall bool) error { return errors.New("install is only needed on macOS") }

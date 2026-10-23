//go:build !windows

package platform

// AttachParentConsole does nothing: these systems' executables already have a terminal.
func AttachParentConsole() {}

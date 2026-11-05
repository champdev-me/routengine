//go:build !darwin

package platform

// On Windows and Linux the window app starts at sign-in the same way the tray does.

func AppAutostartEnabled() bool     { return AutostartEnabled() }
func SetAppAutostart(on bool) error { return SetAutostart(on) }
func RefreshStartupApp()            {}

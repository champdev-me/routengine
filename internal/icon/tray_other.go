//go:build !windows

package icon

// Tray is the icon in the format the tray library expects on this OS: its Linux backend decodes
// PNG but not ICO.
func Tray(on bool) []byte { return PNG(on) }

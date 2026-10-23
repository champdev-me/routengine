// Package icon is the tray icon of the console tray (systems without the RoutEngine window): the brand
// mark, coloured while routing is on and grey while it is off.
package icon

import (
	"bytes"
	"image/png"

	"routengine/internal/brand"
)

// PNG is the mark at tray size.
func PNG(on bool) []byte {
	img := brand.Render(32)
	if !on {
		img = brand.RenderOff(32)
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// ICO is the same mark as a Windows icon file.
func ICO(on bool) []byte { return brand.TrayICO(on) }

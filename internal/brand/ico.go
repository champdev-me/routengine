package brand

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
)

// ICO packs one image per size into a Windows icon file (PNG data inside, as Windows Vista and later read).
func ICO(imgs []*image.NRGBA) []byte {
	pngs := make([][]byte, len(imgs))
	for i, img := range imgs {
		var b bytes.Buffer
		png.Encode(&b, img)
		pngs[i] = b.Bytes()
	}
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, [3]uint16{0, 1, uint16(len(imgs))})
	offset := 6 + 16*len(imgs)
	for i, img := range imgs {
		s := img.Bounds().Dx()
		w := byte(s)
		if s >= 256 {
			w = 0 // 0 means 256
		}
		ico.Write([]byte{w, w, 0, 0})
		binary.Write(&ico, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&ico, binary.LittleEndian, [2]uint32{uint32(len(pngs[i])), uint32(offset)})
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		ico.Write(p)
	}
	return ico.Bytes()
}

// RenderOff is the mark in grey, for when routing is off: the same shape, nothing coloured.
func RenderOff(size int) *image.NRGBA {
	img := Render(size)
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b := float64(img.Pix[i]), float64(img.Pix[i+1]), float64(img.Pix[i+2])
		y := 0.299*r + 0.587*g + 0.114*b
		// Lighter than a plain grey so it still reads on a dark taskbar.
		y = 90 + y*0.45
		img.Pix[i], img.Pix[i+1], img.Pix[i+2] = uint8(y), uint8(y), uint8(y)
	}
	return img
}

// TrayICO is the notification-area icon: coloured while routing is on, grey while it is off.
func TrayICO(on bool) []byte {
	var imgs []*image.NRGBA
	for _, s := range []int{16, 20, 24, 32, 40, 48} {
		if on {
			imgs = append(imgs, Render(s))
		} else {
			imgs = append(imgs, RenderOff(s))
		}
	}
	return ICO(imgs)
}

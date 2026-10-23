// Package brand draws RoutEngine's mark: one route that forks to two adapters. The same drawing makes
// the icon files (scripts/mkicon) and the logo in the window, so they always match.
package brand

import (
	"image"
	"image/color"
	"math"
)

// The mark uses three colours: cyan, amber and white.
var (
	Cyan  = color.NRGBA{0x00, 0xBC, 0xD4, 0xff} // Material Cyan 500
	Deep  = color.NRGBA{0x00, 0x97, 0xA7, 0xff} // Cyan 700, the bottom of the tile
	Amber = color.NRGBA{0xFF, 0xC1, 0x07, 0xff} // Material Amber 500
	White = color.NRGBA{0xff, 0xff, 0xff, 0xff}
)

// Geometry is in a 100 x 100 box.
const (
	inset     = 3.0  // empty margin around the tile
	radius    = 23.0 // corner radius of the tile
	lineHalf  = 3.6  // half the width of a route
	inR       = 8.5  // the app node, where the route starts
	outR      = 8.2  // the adapter nodes, where it ends
	ringWidth = 3.4
)

type pt struct{ x, y float64 }

// curve samples a cubic Bézier finely enough that straight pieces look smooth at 256 px.
func curve(a, b, c, d pt) []pt {
	const n = 96
	out := make([]pt, 0, n+1)
	for i := 0; i <= n; i++ {
		t := float64(i) / n
		u := 1 - t
		out = append(out, pt{
			u*u*u*a.x + 3*u*u*t*b.x + 3*u*t*t*c.x + t*t*t*d.x,
			u*u*u*a.y + 3*u*u*t*b.y + 3*u*t*t*c.y + t*t*t*d.y,
		})
	}
	return out
}

var (
	appNode  = pt{25, 50}
	topNode  = pt{75, 30}
	lowNode  = pt{75, 70}
	topRoute = curve(appNode, pt{52, 50}, pt{48, 30}, topNode)
	lowRoute = curve(appNode, pt{52, 50}, pt{48, 70}, lowNode)
)

// distSeg is the distance from p to the segment ab.
func distSeg(p, a, b pt) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/l2))
	}
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}

func distPoly(p pt, poly []pt) float64 {
	d := math.Inf(1)
	for i := 1; i < len(poly); i++ {
		d = math.Min(d, distSeg(p, poly[i-1], poly[i]))
	}
	return d
}

// sdRoundRect is negative inside the rounded square.
func sdRoundRect(p pt) float64 {
	c := 50.0
	half := c - inset
	qx, qy := math.Abs(p.x-c)-(half-radius), math.Abs(p.y-c)-(half-radius)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
}

func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 0xff}
}

// over lays c with coverage a (0..1) on top of dst.
func over(dst color.NRGBA, c color.NRGBA, a float64) color.NRGBA {
	if a <= 0 {
		return dst
	}
	if a >= 1 {
		return color.NRGBA{c.R, c.G, c.B, dst.A}
	}
	f := func(d, s uint8) uint8 { return uint8(float64(d)*(1-a) + float64(s)*a + 0.5) }
	return color.NRGBA{f(dst.R, c.R), f(dst.G, c.G), f(dst.B, c.B), dst.A}
}

// Render draws the mark size pixels square. Every pixel is computed from distances, so the edges are
// smooth at any size, small ones included.
func Render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	px := float64(size) / 100 // pixels per unit
	cover := func(d float64) float64 { return math.Max(0, math.Min(1, 0.5-d*px)) }
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			p := pt{(float64(x) + 0.5) / px, (float64(y) + 0.5) / px}
			tile := cover(sdRoundRect(p))
			if tile <= 0 {
				continue
			}
			c := lerp(Cyan, Deep, math.Max(0, math.Min(1, (p.y-inset)/(100-2*inset))))

			// The two routes.
			// The routes stop at the adapter rings, so the rings stay hollow.
			route := cover(math.Min(distPoly(p, topRoute), distPoly(p, lowRoute)) - lineHalf)
			route *= 1 - math.Max(cover(math.Hypot(p.x-topNode.x, p.y-topNode.y)-outR), cover(math.Hypot(p.x-lowNode.x, p.y-lowNode.y)-outR))
			c = over(c, White, route)

			// The app node: a solid disc.
			c = over(c, White, cover(math.Hypot(p.x-appNode.x, p.y-appNode.y)-inR))

			// The adapter nodes: rings with the tile showing through. The upper one, the route that is
			// chosen, is amber.
			ring := func(n pt, col color.NRGBA) {
				d := math.Hypot(p.x-n.x, p.y-n.y)
				a := cover(d-outR) - cover(d-(outR-ringWidth))
				c = over(c, col, a)
			}
			ring(topNode, Amber)
			ring(lowNode, White)

			c.A = uint8(tile*255 + 0.5)
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

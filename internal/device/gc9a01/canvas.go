package gc9a01

import "math"

// canvas is a 240x240 RGB565 framebuffer used to compose color scenes off the
// device. The linux driver copies canvas.pix into its reusable transmit buffer
// and streams it; keeping the canvas platform-independent (no build tag) is what
// lets the scene/layout logic compile and unit-test on a laptop, exactly the way
// oled's mono + frame() do for the SSD1306.
type canvas struct {
	pix []byte // Width*Height*2, RGB565 big-endian (hi, lo) per pixel
}

func newCanvas() *canvas {
	return &canvas{pix: make([]byte, Width*Height*2)}
}

// rgb is a packed RGB565 color held as the two bytes the panel expects per
// pixel (most-significant byte first), so a fill is a straight 2-byte copy.
type rgb struct{ hi, lo byte }

// color565 packs an 8-bit RGB triple into an rgb.
func color565(r, g, b uint8) rgb {
	hi, lo := rgb565(r, g, b)
	return rgb{hi, lo}
}

func (c *canvas) set(x, y int, col rgb) {
	if x < 0 || x >= Width || y < 0 || y >= Height {
		return
	}
	i := (y*Width + x) * 2
	c.pix[i] = col.hi
	c.pix[i+1] = col.lo
}

// fill paints the whole canvas one color.
func (c *canvas) fill(col rgb) {
	for i := 0; i < len(c.pix); i += 2 {
		c.pix[i] = col.hi
		c.pix[i+1] = col.lo
	}
}

// fillRect paints [x0,x1) × [y0,y1).
func (c *canvas) fillRect(x0, y0, x1, y1 int, col rgb) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c.set(x, y, col)
		}
	}
}

// Geometry of the visible round area. The GRAM is square; the glass shows the
// inscribed circle, so scenes center on (cx,cy) and clamp text to the chord.
const (
	cx     = Width / 2
	cy     = Height / 2
	radius = Width / 2
)

// chordHalfWidth returns half the circle's horizontal extent at row y (0 when y
// is outside the circle). Centering on the chord at the text's baseline — rather
// than on the full 240 — keeps wide strings off the curved edge (plan Risk #5).
func chordHalfWidth(y int) int {
	dy := y - cy
	d2 := radius*radius - dy*dy
	if d2 <= 0 {
		return 0
	}
	return int(math.Sqrt(float64(d2)))
}

// centerXAt returns the x that horizontally centers a run of pixel-width w on
// the circle's chord at row y, clamped so it never starts left of the chord.
func centerXAt(w, y int) int {
	half := chordHalfWidth(y)
	x := cx - w/2
	if left := cx - half; x < left {
		x = left
	}
	return x
}

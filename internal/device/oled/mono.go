package oled

import (
	"image"
	"image/color"
)

// mono is a packed 1-bit-per-pixel image. Bit 1 = on, 0 = off. Stored row-
// major; Stride is in bytes. We keep our own rather than depend on
// periph.io/x/conn/v3/display/displaytest's image1bit package so this file
// also compiles on darwin.
type mono struct {
	W, H   int
	Stride int
	Pix    []byte
}

func newMono(w, h int) *mono {
	stride := (w + 7) / 8
	return &mono{W: w, H: h, Stride: stride, Pix: make([]byte, stride*h)}
}

func (m *mono) ColorModel() color.Model { return color.GrayModel }
func (m *mono) Bounds() image.Rectangle { return image.Rect(0, 0, m.W, m.H) }
func (m *mono) At(x, y int) color.Color {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return color.Gray{}
	}
	b := m.Pix[y*m.Stride+x/8]
	if b&(1<<uint(7-(x%8))) != 0 {
		return color.Gray{Y: 255}
	}
	return color.Gray{Y: 0}
}

// Set turns the pixel on (true) or off (false).
func (m *mono) Set(x, y int, on bool) {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return
	}
	mask := byte(1 << uint(7-(x%8)))
	if on {
		m.Pix[y*m.Stride+x/8] |= mask
	} else {
		m.Pix[y*m.Stride+x/8] &^= mask
	}
}

// fillRect turns on every pixel in [x0,x1) × [y0,y1).
func (m *mono) fillRect(x0, y0, x1, y1 int, on bool) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			m.Set(x, y, on)
		}
	}
}

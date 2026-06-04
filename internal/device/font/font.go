// Package font is the shared 5x7 bitmap font used by every display driver. The
// blitters are sink-based: DrawText / DrawTextScaled call a set(x,y) callback
// for each lit pixel rather than writing to a specific framebuffer, so the mono
// OLED (set = mono.Set) and the color LCD (set = canvas.set with a color) draw
// from the same glyph bitmaps — no font drift between panels.
package font

import "strings"

// font5x7 is a tiny embedded bitmap font: 5 columns × 7 rows per glyph.
// Each glyph is 5 bytes, one per column. Bit 0 = top row, bit 6 = bottom row.
// Coverage: A-Z, 0-9, and a small punctuation set. Lowercase letters render
// as their uppercase form. Unknown chars render as a hollow box.
//
// The font is intentionally compact and pragmatic — meant to be replaced by
// proper bitmap fonts (small/medium/large) when polishing on real hardware.
var font5x7 = map[byte][5]byte{
	' ':  {0, 0, 0, 0, 0},
	'!':  {0, 0, 0x5F, 0, 0},
	'"':  {0, 0x07, 0, 0x07, 0},
	'\'': {0, 0x05, 0x03, 0, 0},
	'(':  {0, 0x1C, 0x22, 0x41, 0},
	')':  {0, 0x41, 0x22, 0x1C, 0},
	',':  {0, 0x50, 0x30, 0, 0},
	'-':  {0x08, 0x08, 0x08, 0x08, 0x08},
	'.':  {0, 0x60, 0x60, 0, 0},
	'/':  {0x20, 0x10, 0x08, 0x04, 0x02},
	':':  {0, 0x36, 0x36, 0, 0},
	'?':  {0x02, 0x01, 0x51, 0x09, 0x06},

	'0': {0x3E, 0x51, 0x49, 0x45, 0x3E},
	'1': {0, 0x42, 0x7F, 0x40, 0},
	'2': {0x42, 0x61, 0x51, 0x49, 0x46},
	'3': {0x21, 0x41, 0x45, 0x4B, 0x31},
	'4': {0x18, 0x14, 0x12, 0x7F, 0x10},
	'5': {0x27, 0x45, 0x45, 0x45, 0x39},
	'6': {0x3C, 0x4A, 0x49, 0x49, 0x30},
	'7': {0x01, 0x71, 0x09, 0x05, 0x03},
	'8': {0x36, 0x49, 0x49, 0x49, 0x36},
	'9': {0x06, 0x49, 0x49, 0x29, 0x1E},

	'A': {0x7E, 0x11, 0x11, 0x11, 0x7E},
	'B': {0x7F, 0x49, 0x49, 0x49, 0x36},
	'C': {0x3E, 0x41, 0x41, 0x41, 0x22},
	'D': {0x7F, 0x41, 0x41, 0x22, 0x1C},
	'E': {0x7F, 0x49, 0x49, 0x49, 0x41},
	'F': {0x7F, 0x09, 0x09, 0x09, 0x01},
	'G': {0x3E, 0x41, 0x49, 0x49, 0x7A},
	'H': {0x7F, 0x08, 0x08, 0x08, 0x7F},
	'I': {0, 0x41, 0x7F, 0x41, 0},
	'J': {0x20, 0x40, 0x41, 0x3F, 0x01},
	'K': {0x7F, 0x08, 0x14, 0x22, 0x41},
	'L': {0x7F, 0x40, 0x40, 0x40, 0x40},
	'M': {0x7F, 0x02, 0x0C, 0x02, 0x7F},
	'N': {0x7F, 0x04, 0x08, 0x10, 0x7F},
	'O': {0x3E, 0x41, 0x41, 0x41, 0x3E},
	'P': {0x7F, 0x09, 0x09, 0x09, 0x06},
	'Q': {0x3E, 0x41, 0x51, 0x21, 0x5E},
	'R': {0x7F, 0x09, 0x19, 0x29, 0x46},
	'S': {0x46, 0x49, 0x49, 0x49, 0x31},
	'T': {0x01, 0x01, 0x7F, 0x01, 0x01},
	'U': {0x3F, 0x40, 0x40, 0x40, 0x3F},
	'V': {0x1F, 0x20, 0x40, 0x20, 0x1F},
	'W': {0x3F, 0x40, 0x38, 0x40, 0x3F},
	'X': {0x63, 0x14, 0x08, 0x14, 0x63},
	'Y': {0x07, 0x08, 0x70, 0x08, 0x07},
	'Z': {0x61, 0x51, 0x49, 0x45, 0x43},
}

// glyph returns the 5-column bitmap for c, folding lowercase to uppercase and
// substituting a hollow box for anything outside the font's coverage.
func glyph(c byte) [5]byte {
	if g, ok := font5x7[c]; ok {
		return g
	}
	if c >= 'a' && c <= 'z' {
		return font5x7[c-'a'+'A']
	}
	return [5]byte{0x7F, 0x41, 0x41, 0x41, 0x7F} // hollow box for unknown
}

// DrawText draws s starting at (x,y), calling set(px,py) for each lit pixel.
// Each glyph is 5 cols wide × 7 rows tall, plus a 1-col gap. Returns the
// x-coord after the last glyph.
func DrawText(x, y int, s string, set func(px, py int)) int {
	for i := 0; i < len(s); i++ {
		g := glyph(s[i])
		for col := 0; col < 5; col++ {
			b := g[col]
			for row := 0; row < 7; row++ {
				if b&(1<<uint(row)) != 0 {
					set(x+col, y+row)
				}
			}
		}
		x += 6
	}
	return x
}

// DrawTextScaled draws text with each glyph pixel replicated `scale` times in
// both directions (a scale×scale block per lit pixel). Used for "large"
// rendering of dog names. scale <= 1 falls back to DrawText.
func DrawTextScaled(x, y int, s string, scale int, set func(px, py int)) int {
	if scale <= 1 {
		return DrawText(x, y, s, set)
	}
	for i := 0; i < len(s); i++ {
		g := glyph(s[i])
		for col := 0; col < 5; col++ {
			b := g[col]
			for row := 0; row < 7; row++ {
				if b&(1<<uint(row)) != 0 {
					bx := x + col*scale
					by := y + row*scale
					for dy := 0; dy < scale; dy++ {
						for dx := 0; dx < scale; dx++ {
							set(bx+dx, by+dy)
						}
					}
				}
			}
		}
		x += 6 * scale
	}
	return x
}

// TextWidth returns the pixel width DrawText would consume for s at scale 1.
func TextWidth(s string) int {
	if s == "" {
		return 0
	}
	return len(s)*6 - 1
}

// Upper is a small helper since font5x7 is uppercase-only.
func Upper(s string) string { return strings.ToUpper(s) }

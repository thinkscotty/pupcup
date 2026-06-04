package oled

import "github.com/scottyturner/pupcup/internal/device/font"

// These thin wrappers bind the shared font blitters (which take a pixel sink)
// to this package's 1-bpp mono framebuffer, so scenes.go reads exactly as it
// did before the font moved to its own package. The sink lights pixels on.

func drawText(m *mono, x, y int, s string) int {
	return font.DrawText(x, y, s, func(px, py int) { m.Set(px, py, true) })
}

func drawTextScaled(m *mono, x, y int, s string, scale int) int {
	return font.DrawTextScaled(x, y, s, scale, func(px, py int) { m.Set(px, py, true) })
}

func textWidth(s string) int { return font.TextWidth(s) }

func upper(s string) string { return font.Upper(s) }

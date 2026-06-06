package ui

import (
	"image"
	"image/color"
	"testing"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/device/anim"
)

func TestLoadFont(t *testing.T) {
	f, err := LoadFont()
	if err != nil {
		t.Fatalf("LoadFont: %v", err)
	}
	fc := f.Face(48)
	if fc.Ascent() <= 0 || fc.Height() <= 0 {
		t.Fatalf("face metrics not populated: ascent=%d height=%d", fc.Ascent(), fc.Height())
	}
	// Same size returns the same cached face.
	if f.Face(48) != fc {
		t.Fatal("Face(48) returned a different instance; not cached")
	}
}

// TestGlyphRasterizes proves the subsetted Bold master yields a solid, chunky
// glyph: a measurable mask with at least one fully-covered (alpha 255) pixel.
func TestGlyphRasterizes(t *testing.T) {
	f, err := LoadFont()
	if err != nil {
		t.Fatalf("LoadFont: %v", err)
	}
	fc := f.Face(64)
	gm := fc.glyph('8')
	if gm.w <= 0 || gm.h <= 0 || gm.advance <= 0 {
		t.Fatalf("glyph '8' empty: w=%d h=%d adv=%d", gm.w, gm.h, gm.advance)
	}
	solid := false
	for _, a := range gm.pix {
		if a == 255 {
			solid = true
			break
		}
	}
	if !solid {
		t.Fatal("no fully-covered pixel in '8'; font may not be the Bold master")
	}
	if w := fc.Measure("88"); w <= gm.advance {
		t.Fatalf("Measure(\"88\")=%d should exceed one advance %d", w, gm.advance)
	}
}

// TestGlyphCacheMatchesRGB565 is the Phase-3 exit check that the text path stays
// byte-compatible with the panel: a glyph blitted in a known color, then packed
// with the engine's packer, yields that color's canonical RGB565 at solid pixels.
func TestGlyphCacheMatchesRGB565(t *testing.T) {
	f, err := LoadFont()
	if err != nil {
		t.Fatalf("LoadFont: %v", err)
	}
	const W, H = 80, 80
	rgba := image.NewRGBA(image.Rect(0, 0, W, H))
	ctx := gg.NewContextForRGBA(rgba)

	col := color.RGBA{0x12, 0x34, 0x56, 0xFF} // -> 0x11AA in RGB565
	fc := f.Face(64)
	fc.DrawString(ctx, "8", 16, 64, col)

	// Find a fully-covered pixel (exactly the source color over black).
	var px, py int = -1, -1
	for y := 0; y < H && py < 0; y++ {
		for x := 0; x < W; x++ {
			i := rgba.PixOffset(x, y)
			if rgba.Pix[i] == col.R && rgba.Pix[i+1] == col.G && rgba.Pix[i+2] == col.B {
				px, py = x, y
				break
			}
		}
	}
	if px < 0 {
		t.Fatal("no solid glyph pixel found to compare")
	}

	dst := make([]byte, W*H*2)
	anim.PackRGBARect(dst, rgba, anim.Rect{X0: 0, Y0: 0, X1: W, Y1: H})
	off := (py*W + px) * 2
	if dst[off] != 0x11 || dst[off+1] != 0xAA {
		t.Fatalf("packed glyph pixel = %02X%02X, want 11AA (rgb565 of #123456)", dst[off], dst[off+1])
	}
}

func TestDrawStringZeroAlloc(t *testing.T) {
	f, err := LoadFont()
	if err != nil {
		t.Fatalf("LoadFont: %v", err)
	}
	rgba := image.NewRGBA(image.Rect(0, 0, 240, 64))
	ctx := gg.NewContextForRGBA(rgba)
	fc := f.Face(40)
	fc.Warm(glyphRunes)

	var col color.Color = color.RGBA{0, 200, 60, 255} // pre-boxed: no per-call interface alloc
	const s = "UNLOCK 3:42H"
	fc.DrawString(ctx, s, 8, 40, col) // warm the exact runes
	if allocs := testing.AllocsPerRun(500, func() { fc.DrawString(ctx, s, 8, 40, col) }); allocs != 0 {
		t.Fatalf("DrawString allocated %.2f objs/op after warm-up, want 0", allocs)
	}
}

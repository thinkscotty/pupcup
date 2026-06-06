// Package ui is PupCup's platform-independent visual kit for the round LCD: a
// design system (theme.go), a font/glyph-cache pipeline (this file), and the
// composable components — avatar, feeding ring, rim pips, effects, bowl — that
// the animated scenes draw with. Everything here is pure Go (no build tag): it
// draws into a shared *image.RGBA via fogleman/gg exactly the same on macOS (for
// golden tests) as on the Pi, where only the RGB565 byte-out is platform code.
//
// Text is the one path that must never touch the rasterizer inside the render
// loop: freetype glyph hinting + anti-aliasing is far too costly at 60 Hz. So
// glyphs are rasterized once into alpha-coverage masks (a Face's glyph cache)
// and every frame is a masked color blit — a cheap, allocation-free composite
// over whatever gg already drew into the same buffer.
package ui

import (
	"embed"
	"image"
	"image/color"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// assetsFS holds the embedded, single-weight rounded display face. It is
// Fredoka (OFL 1.1), instanced to a static Bold master and subset to Basic
// Latin so the embedded file is ~21 KB — see assets/OFL.txt for the license.
//
//go:embed assets/Fredoka-Bold.ttf
var assetsFS embed.FS

const fontPath = "assets/Fredoka-Bold.ttf"

// glyphRunes are the characters every face pre-rasterizes at warm-up so the
// render loop never rasterizes a glyph: digits, time/score punctuation, and the
// full alphabet (dog names are uppercased before drawing, but lowercase is kept
// so mixed-case labels are cheap too).
const glyphRunes = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789 :*>/-.'%!?,()"

// Font is the parsed embedded typeface plus a cache of Faces by pixel size. One
// Font is shared by the whole UI; build it once (LoadFont, or via NewTheme).
type Font struct {
	ttf   *truetype.Font
	faces map[float64]*Face
}

// LoadFont parses the embedded Fredoka face. It is cheap and allocation-heavy
// (parse + table decode), so call it once at startup, not per frame.
func LoadFont() (*Font, error) {
	b, err := assetsFS.ReadFile(fontPath)
	if err != nil {
		return nil, err
	}
	ttf, err := truetype.Parse(b)
	if err != nil {
		return nil, err
	}
	return &Font{ttf: ttf, faces: map[float64]*Face{}}, nil
}

// Face returns the cached Face for the given pixel height (em size), creating it
// on first use. DPI is fixed at 72 so the point size equals the em size in
// pixels. Not safe for concurrent first-use from multiple goroutines; the engine
// builds every face during warm-up on one goroutine, which is the intended use.
func (f *Font) Face(px float64) *Face {
	if fc, ok := f.faces[px]; ok {
		return fc
	}
	ff := truetype.NewFace(f.ttf, &truetype.Options{
		Size: px,
		DPI:  72,
		// No hinting: the embedded face carries no bytecode (dropped at subset
		// time), and unhinted rendering keeps Fredoka's round shapes smooth at
		// display sizes — hinting would only distort them.
		Hinting: font.HintingNone,
	})
	m := ff.Metrics()
	fc := &Face{
		face:    ff,
		px:      px,
		ascent:  m.Ascent.Round(),
		descent: m.Descent.Round(),
		height:  m.Height.Round(),
		glyphs:  map[rune]*glyphMask{},
	}
	f.faces[px] = fc
	return fc
}

// glyphMask is one rasterized glyph: its alpha coverage plus the placement
// metrics needed to position it relative to a baseline pen. The coverage is held
// as a flat row-major byte slice so blitting is a tight inner loop with no
// interface dispatch.
type glyphMask struct {
	pix              []byte // w*h alpha coverage, row-major
	w, h             int
	originX, originY int // mask top-left relative to the pen origin (baseline)
	advance          int // pen advance in px
}

// Face is a typeface at one pixel size with its own glyph cache.
type Face struct {
	face                    font.Face
	px                      float64
	ascent, descent, height int
	glyphs                  map[rune]*glyphMask
}

// Ascent is the pixels from the baseline up to the top of the tallest glyph.
func (fc *Face) Ascent() int { return fc.ascent }

// Descent is the pixels from the baseline down to the bottom of glyphs.
func (fc *Face) Descent() int { return fc.descent }

// Height is the recommended line height in pixels.
func (fc *Face) Height() int { return fc.height }

// Warm pre-rasterizes every rune in s so later draws hit the cache. Safe to call
// repeatedly; already-cached glyphs are skipped.
func (fc *Face) Warm(s string) {
	for _, r := range s {
		fc.glyph(r)
	}
}

// glyph returns r's mask, rasterizing and caching it on first use. Rasterization
// is the expensive freetype step we keep out of the render loop.
func (fc *Face) glyph(r rune) *glyphMask {
	if gm, ok := fc.glyphs[r]; ok {
		return gm
	}
	gm := fc.rasterize(r)
	fc.glyphs[r] = gm
	return gm
}

// rasterize renders r to an alpha mask via freetype. Glyphs with no outline
// (space) yield a zero-size mask carrying only the advance.
func (fc *Face) rasterize(r rune) *glyphMask {
	dr, mask, maskp, adv, ok := fc.face.Glyph(fixed.Point26_6{}, r)
	gm := &glyphMask{advance: adv.Round()}
	if gm.advance == 0 {
		// Zero-outline glyphs (space) still need their pen advance.
		if a, ok := fc.face.GlyphAdvance(r); ok {
			gm.advance = a.Round()
		}
	}

	w, h := dr.Dx(), dr.Dy()
	if !ok || w <= 0 || h <= 0 {
		return gm
	}
	gm.w, gm.h = w, h
	gm.originX, gm.originY = dr.Min.X, dr.Min.Y
	gm.pix = make([]byte, w*h)

	// Fast path: freetype returns an *image.Alpha, so copy coverage rows
	// directly. Fall back to the color interface for any other mask type.
	if am, ok := mask.(*image.Alpha); ok {
		for y := 0; y < h; y++ {
			si := am.PixOffset(maskp.X, maskp.Y+y)
			copy(gm.pix[y*w:(y+1)*w], am.Pix[si:si+w])
		}
	} else {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				_, _, _, a := mask.At(maskp.X+x, maskp.Y+y).RGBA()
				gm.pix[y*w+x] = byte(a >> 8)
			}
		}
	}
	return gm
}

// Measure returns the pixel width DrawString would advance for s (sum of glyph
// advances plus kerning), used for centering and fitting.
func (fc *Face) Measure(s string) int {
	w := 0
	var prev rune
	first := true
	for _, r := range s {
		if !first {
			w += fc.face.Kern(prev, r).Round()
		}
		w += fc.glyph(r).advance
		prev, first = r, false
	}
	return w
}

// DrawString blits s into ctx's RGBA buffer in col, with the left edge at x and
// the text baseline at baselineY. It returns the pen x after the last glyph.
// Allocation-free once the glyphs are warmed.
func (fc *Face) DrawString(ctx *gg.Context, s string, x, baselineY int, col color.Color) int {
	dst, ok := ctx.Image().(*image.RGBA)
	if !ok {
		return x
	}
	cr, cg, cb := rgbaOf(col)
	var prev rune
	first := true
	pen := x
	for _, r := range s {
		if !first {
			pen += fc.face.Kern(prev, r).Round()
		}
		gm := fc.glyph(r)
		blitMask(dst, gm, pen, baselineY, cr, cg, cb)
		pen += gm.advance
		prev, first = r, false
	}
	return pen
}

// DrawCentered blits s horizontally centered on cx, with its baseline at
// baselineY. Convenience over Measure + DrawString for the many centered labels.
func (fc *Face) DrawCentered(ctx *gg.Context, s string, cx, baselineY int, col color.Color) {
	fc.DrawString(ctx, s, cx-fc.Measure(s)/2, baselineY, col)
}

// VisualBounds returns s's inked extent relative to the baseline: top (≤0, above
// the baseline) and bottom (≥0, below it). Used to optically center a string in
// a box rather than centering by the font's full line height.
func (fc *Face) VisualBounds(s string) (top, bottom int) {
	first := true
	for _, r := range s {
		gm := fc.glyph(r)
		if gm.h == 0 {
			continue
		}
		t, b := gm.originY, gm.originY+gm.h
		if first || t < top {
			top = t
		}
		if first || b > bottom {
			bottom = b
		}
		first = false
	}
	return top, bottom
}

// DrawCenteredBoth blits s centered both horizontally on cx and optically
// (by inked extent) on cy — the right way to drop a hero glyph or number into a
// circle.
func (fc *Face) DrawCenteredBoth(ctx *gg.Context, s string, cx, cy int, col color.Color) {
	top, bottom := fc.VisualBounds(s)
	baseline := cy - (top+bottom)/2
	fc.DrawString(ctx, s, cx-fc.Measure(s)/2, baseline, col)
}

// rgbaOf reduces a color.Color to 8-bit non-premultiplied-ish components. We
// draw opaque text, so dividing by alpha is unnecessary; callers pass opaque
// colors.
func rgbaOf(col color.Color) (r, g, b uint8) {
	cr, cg, cb, _ := col.RGBA()
	return uint8(cr >> 8), uint8(cg >> 8), uint8(cb >> 8)
}

// blitMask composites one glyph's coverage onto dst in (cr,cg,cb), with the
// mask's top-left at (penX+originX, baselineY+originY). Source-over alpha blend,
// clipped to dst bounds, with a fast skip for fully transparent pixels. No
// allocations.
func blitMask(dst *image.RGBA, gm *glyphMask, penX, baselineY int, cr, cg, cb uint8) {
	if gm.w == 0 || gm.h == 0 {
		return
	}
	b := dst.Bounds()
	x0 := penX + gm.originX
	y0 := baselineY + gm.originY
	for gy := 0; gy < gm.h; gy++ {
		dy := y0 + gy
		if dy < b.Min.Y || dy >= b.Max.Y {
			continue
		}
		row := gm.pix[gy*gm.w : gy*gm.w+gm.w]
		for gx := 0; gx < gm.w; gx++ {
			a := uint32(row[gx])
			if a == 0 {
				continue
			}
			dx := x0 + gx
			if dx < b.Min.X || dx >= b.Max.X {
				continue
			}
			i := dst.PixOffset(dx, dy)
			if a == 255 {
				dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = cr, cg, cb, 255
				continue
			}
			ia := 255 - a
			dst.Pix[i] = byte((uint32(cr)*a + uint32(dst.Pix[i])*ia + 127) / 255)
			dst.Pix[i+1] = byte((uint32(cg)*a + uint32(dst.Pix[i+1])*ia + 127) / 255)
			dst.Pix[i+2] = byte((uint32(cb)*a + uint32(dst.Pix[i+2])*ia + 127) / 255)
			dst.Pix[i+3] = 255
		}
	}
}

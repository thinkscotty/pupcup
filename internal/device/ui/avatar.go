package ui

import (
	"image"
	"image/color"
	"strings"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/domain"
)

// DrawAvatar renders a dog as the centerpiece: their circular-cropped photo if
// one is supplied, otherwise an accent-colored disc with the dog's initial. A
// thin rim outlines the disc for definition against the background.
//
// squash is a squash-&-stretch factor in roughly [-1,1]: 0 is the resting
// circle, positive squashes wide-and-short (a landing/impact), negative
// stretches tall-and-thin (mid-leap). It deforms the disc while preserving its
// area-ish footprint so a bounce reads as springy rather than scaled.
//
// photo, when non-nil, must already be decoded, cropped square and downscaled to
// about 2*r on a side (the scene's asset cache does this once); it is drawn
// clipped to the disc.
func DrawAvatar(ctx *gg.Context, th *Theme, dog domain.Dog, photo *image.RGBA, cx, cy, r int, squash float64) {
	sx, sy := squashScale(squash)
	accent := th.Wash().Apply(ParseAccent(dog.AccentColor))
	rim := th.C(ColBg)

	ctx.Push()
	ctx.Translate(float64(cx), float64(cy))
	ctx.Scale(sx, sy)

	if photo != nil {
		ctx.DrawCircle(0, 0, float64(r))
		ctx.Clip()
		ctx.DrawImageAnchored(photo, 0, 0, 0.5, 0.5)
		ctx.ResetClip()
	} else {
		ctx.SetColor(accent)
		ctx.DrawCircle(0, 0, float64(r))
		ctx.Fill()
	}

	// Rim: a soft dark outline so the disc separates from any background.
	ctx.SetColor(rim)
	ctx.SetLineWidth(3)
	ctx.DrawCircle(0, 0, float64(r)-1)
	ctx.Stroke()
	ctx.Pop()

	// The initial is drawn in screen space (the glyph blitter is transform-
	// unaware), so it stays crisp and centered through the squash. Photos carry
	// their own subject, so only the accent disc gets a letter.
	if photo == nil {
		if in := initial(dog.Name); in != "" {
			th.Huge.DrawCenteredBoth(ctx, in, cx, cy, contrastOn(accent))
		}
	}
}

// squashScale converts a squash factor into x/y scale multipliers that keep the
// disc's footprint roughly constant (wider ⇒ shorter and vice-versa).
func squashScale(squash float64) (sx, sy float64) {
	if squash > 1 {
		squash = 1
	} else if squash < -1 {
		squash = -1
	}
	const amt = 0.32
	return 1 + amt*squash, 1 - amt*squash
}

// initial returns the uppercase first letter of a name (empty if none).
func initial(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return ""
}

// contrastOn returns white or near-black, whichever reads better on bg, using
// perceptual luminance.
func contrastOn(bg color.RGBA) color.RGBA {
	// Rec. 601 luma, 0..255.
	y := (299*int(bg.R) + 587*int(bg.G) + 114*int(bg.B)) / 1000
	if y > 150 {
		return color.RGBA{0x12, 0x12, 0x18, 0xFF}
	}
	return ColFg
}

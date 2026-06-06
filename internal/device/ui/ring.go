package ui

import (
	"image/color"
	"math"

	"github.com/fogleman/gg"
)

// twelveOClock is the angle of the 12-o'clock position. gg draws in screen space
// (y down), so increasing the angle from here sweeps clockwise, which is the
// natural "filling up" direction for a progress ring.
const twelveOClock = -math.Pi / 2

// DrawFeedingRing strokes an Activity-Rings–style progress ring around (cx,cy):
// a full background track plus a colored arc showing how full it is, starting at
// 12 o'clock and sweeping clockwise with round end caps.
//
// segments selects the shape of the channel:
//   - segments <= 1: one continuous arc whose length is filled (a 0..1 fraction).
//   - segments  > 1: that many equal arcs separated by small gaps (one per dog),
//     with filled counting how many are lit — fractional values partially light
//     the in-progress segment, so the count animates smoothly.
//
// The track and arc are both run through the theme Wash; col is the lit color.
func DrawFeedingRing(ctx *gg.Context, th *Theme, cx, cy int, r, thickness float64, segments int, filled float64, col color.RGBA) {
	fcx, fcy := float64(cx), float64(cy)
	track := th.C(ColSurface)
	lit := th.Wash().Apply(col)

	ctx.SetLineWidth(thickness)
	ctx.SetLineCapRound()

	if segments <= 1 {
		// Background track is a full circle.
		ctx.SetColor(track)
		ctx.DrawArc(fcx, fcy, r, twelveOClock, twelveOClock+2*math.Pi)
		ctx.Stroke()

		f := clamp01(filled)
		if f > 0 {
			ctx.SetColor(lit)
			ctx.DrawArc(fcx, fcy, r, twelveOClock, twelveOClock+2*math.Pi*f)
			ctx.Stroke()
		}
		return
	}

	const gap = 0.14 // radians of dead space between segments
	span := 2 * math.Pi / float64(segments)
	for i := 0; i < segments; i++ {
		a0 := twelveOClock + float64(i)*span + gap/2
		a1 := twelveOClock + float64(i+1)*span - gap/2

		ctx.SetColor(track)
		ctx.DrawArc(fcx, fcy, r, a0, a1)
		ctx.Stroke()

		f := clamp01(filled - float64(i))
		if f > 0 {
			ctx.SetColor(lit)
			ctx.DrawArc(fcx, fcy, r, a0, a0+(a1-a0)*f)
			ctx.Stroke()
		}
	}
}

func clamp01(v float64) float64 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

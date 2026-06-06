package ui

import (
	"image/color"
	"math"

	"github.com/fogleman/gg"
)

// DrawBowl renders a stylized food bowl centered on (cx,cy), filled to fill
// (0..1) with food in col. It is the lead beat of the meal celebration: the bowl
// fills, then the dog reacts. Width w sets the bowl's overall span; depth follows
// from it. Colors run through the theme Wash.
func DrawBowl(ctx *gg.Context, th *Theme, cx, cy int, w, fill float64, food color.RGBA) {
	fcx, fcy := float64(cx), float64(cy)
	rx := w / 2
	ry := w * 0.34 // interior depth
	topY := fcy - ry*0.3

	body := th.C(ColSurface)
	rim := th.C(ColDim)
	foodCol := th.Wash().Apply(food)

	// bowlPath traces the bowl body: a top rim chord closed by a bottom arc.
	bowlPath := func() {
		ctx.NewSubPath()
		ctx.MoveTo(fcx-rx, topY)
		ctx.LineTo(fcx+rx, topY)
		ctx.DrawEllipticalArc(fcx, topY, rx, ry, 0, math.Pi) // bottom half, right→left
		ctx.ClosePath()
	}

	// Empty bowl body.
	bowlPath()
	ctx.SetColor(body)
	ctx.Fill()

	// Food: clip to the body and fill from the bottom up to the level line.
	f := clamp01(fill)
	if f > 0 {
		foodTop := topY + ry*(1-f)
		bowlPath()
		ctx.Clip()
		ctx.SetColor(foodCol)
		ctx.DrawRectangle(fcx-rx-2, foodTop, 2*rx+4, ry+4)
		ctx.Fill()
		ctx.ResetClip()

		// A thin surface ellipse gives the food a top rather than a flat cut.
		ctx.SetColor(foodCol)
		ctx.DrawEllipse(fcx, foodTop, rx*f*0.96, ry*0.16*f)
		ctx.Fill()
	}

	// Body outline + the open rim ellipse for the "bowl mouth".
	bowlPath()
	ctx.SetColor(rim)
	ctx.SetLineWidth(3)
	ctx.Stroke()

	ctx.SetColor(rim)
	ctx.SetLineWidth(3)
	ctx.DrawEllipse(fcx, topY, rx, ry*0.30)
	ctx.Stroke()
}

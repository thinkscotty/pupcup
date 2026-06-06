package ui

import (
	"image/color"
	"math"

	"github.com/fogleman/gg"
)

// Pip is one dog's at-a-glance status dot on the rim. Col is its status color
// (typically a score color, or ColDim when not yet fed today).
type Pip struct {
	Col color.RGBA
}

// DrawRimPips lays the household's status dots along the bottom arc of the panel,
// centered on 6 o'clock, so the centerpiece up top stays clear. Each dog is one
// dot; the selected dog's dot is enlarged with a bright halo so the rotary
// position reads at a glance. Pips run through the theme Wash.
//
// A single dog draws no rim row (the centerpiece already is that dog); two or
// more spread evenly across a bottom arc whose width grows with the count.
func DrawRimPips(ctx *gg.Context, th *Theme, pips []Pip, selected int) {
	n := len(pips)
	if n < 2 {
		return
	}

	const (
		rimR   = SafeRadius - 6 // radius the dots sit on
		dotR   = 5.0
		selR   = 8.0
		perDot = 0.20 // radians of arc allotted per dog
		maxArc = 1.7  // clamp so dots never climb past the sides
	)
	arc := perDot * float64(n-1)
	if arc > maxArc {
		arc = maxArc
	}
	sixOClock := math.Pi / 2
	start := sixOClock - arc/2
	step := 0.0
	if n > 1 {
		step = arc / float64(n-1)
	}

	for i, p := range pips {
		a := start + step*float64(i)
		x := CX + rimR*math.Cos(a)
		y := CY + rimR*math.Sin(a)
		col := th.Wash().Apply(p.Col)

		if i == selected {
			halo := col
			halo.A = 0x55
			ctx.SetColor(halo)
			ctx.DrawCircle(x, y, selR+4)
			ctx.Fill()
			ctx.SetColor(col)
			ctx.DrawCircle(x, y, selR)
			ctx.Fill()
			ctx.SetColor(th.C(ColFg))
			ctx.SetLineWidth(2)
			ctx.DrawCircle(x, y, selR)
			ctx.Stroke()
		} else {
			ctx.SetColor(col)
			ctx.DrawCircle(x, y, dotR)
			ctx.Fill()
		}
	}
}

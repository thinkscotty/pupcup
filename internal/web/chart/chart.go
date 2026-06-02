// Package chart renders minimal server-side SVG charts for PupCup. There is no
// JavaScript and no chart library: each function returns an SVG fragment as
// template.HTML, ready to embed inline in a page. Embedding inline (rather than
// as an <img>) lets the chart inherit app.css — the bar colors come from the
// shared palette via the .bar-full / .bar-partial / .bar-none classes, so the
// chart never drifts from the rest of the UI.
package chart

import (
	"fmt"
	"html"
	"html/template"
	"strings"
	"time"
)

// Day is one calendar day's eating-quality tally for the stacked-bar chart.
type Day struct {
	Date    time.Time
	Full    int
	Partial int
	None    int
}

// Total is the number of meals recorded that day.
func (d Day) Total() int { return d.Full + d.Partial + d.None }

// StackedBar renders an eating-quality stacked-bar chart: one bar per day,
// stacking full meals (bottom), then partial, then none (top), each colored via
// a CSS class. days must be chronological (oldest first). width and height set
// the SVG's coordinate space; it scales responsively to its container through
// the viewBox. A series with no recorded meals renders a friendly empty-state.
func StackedBar(days []Day, width, height int) template.HTML {
	const (
		padL      = 26 // room for the y-axis count labels
		padR      = 8
		padT      = 10
		padBottom = 22 // room for the x-axis date labels
	)
	plotW := float64(width - padL - padR)
	plotH := float64(height - padT - padBottom)
	baseY := float64(padT) + plotH

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="eq-chart" viewBox="0 0 %d %d" preserveAspectRatio="none" role="img" aria-label="Eating quality, last %d days">`, width, height, len(days))

	// Baseline (x-axis).
	fmt.Fprintf(&b, `<line class="axis" x1="%d" y1="%.1f" x2="%d" y2="%.1f"/>`,
		padL, baseY, width-padR, baseY)

	// Highest day's meal count sets the vertical scale. With no meals at all we
	// short-circuit to an empty-state label rather than dividing by zero.
	maxTotal := 0
	for _, d := range days {
		if t := d.Total(); t > maxTotal {
			maxTotal = t
		}
	}
	if len(days) == 0 || maxTotal == 0 {
		fmt.Fprintf(&b, `<text class="chart-empty" x="%d" y="%.1f" text-anchor="middle">No meals recorded in this window</text>`,
			width/2, float64(padT)+plotH/2)
		b.WriteString(`</svg>`)
		return template.HTML(b.String())
	}

	// y-axis end labels: 0 at the baseline, maxTotal at the top.
	fmt.Fprintf(&b, `<text class="axis-label" x="%d" y="%.1f" text-anchor="end">0</text>`, padL-4, baseY+3)
	fmt.Fprintf(&b, `<text class="axis-label" x="%d" y="%.1f" text-anchor="end">%d</text>`, padL-4, float64(padT)+8, maxTotal)

	slot := plotW / float64(len(days))
	barW := slot * 0.7
	if barW < 1 {
		barW = slot // very wide windows: let bars touch rather than vanish
	}

	// yOf maps a cumulative meal count to its pixel y (taller stack → smaller y).
	yOf := func(v int) float64 { return baseY - (float64(v)/float64(maxTotal))*plotH }
	// A handful of evenly-spaced x labels keeps wide (30/90-day) windows legible.
	labelEvery := (len(days) + 5) / 6
	if labelEvery < 1 {
		labelEvery = 1
	}

	for i, d := range days {
		x := float64(padL) + float64(i)*slot + (slot-barW)/2

		// Stack cumulatively so segments share exact edges (no gaps/overlaps).
		seg := func(cls string, lo, hi int) {
			if hi <= lo {
				return
			}
			yTop := yOf(hi)
			h := yOf(lo) - yTop
			fmt.Fprintf(&b, `<rect class="bar %s" x="%.1f" y="%.1f" width="%.1f" height="%.1f"/>`,
				cls, x, yTop, barW, h)
		}
		seg("bar-full", 0, d.Full)
		seg("bar-partial", d.Full, d.Full+d.Partial)
		seg("bar-none", d.Full+d.Partial, d.Total())

		// Per-bar tooltip (full counts) on a transparent hit rect spanning the slot.
		fmt.Fprintf(&b, `<rect x="%.1f" y="%d" width="%.1f" height="%.1f" fill="transparent"><title>%s — %d full, %d partial, %d none</title></rect>`,
			float64(padL)+float64(i)*slot, padT, slot, plotH,
			html.EscapeString(d.Date.Format("Mon Jan 2")), d.Full, d.Partial, d.None)

		if i == 0 || i == len(days)-1 || i%labelEvery == 0 {
			fmt.Fprintf(&b, `<text class="axis-label" x="%.1f" y="%d" text-anchor="middle">%s</text>`,
				float64(padL)+float64(i)*slot+slot/2, height-6, html.EscapeString(d.Date.Format("1/2")))
		}
	}

	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

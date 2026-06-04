package gc9a01

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/font"
	"github.com/scottyturner/pupcup/internal/domain"
)

// Palette. Scores reuse the same green/amber/red intent as the NeoPixel bar in
// state.go (full = ate all, partial = some, none = refused).
var (
	colBg      = color565(0, 0, 0)
	colFg      = color565(255, 255, 255)
	colDim     = color565(130, 130, 140)
	colFull    = color565(0, 200, 60)
	colPartial = color565(255, 170, 0)
	colNone    = color565(220, 50, 50)
	colSnack   = color565(70, 130, 255)
	colHilite  = color565(40, 44, 80)
)

// colorFrame composes the RGB565 canvas for a scene. It mirrors oled.frame:
// the state machine builds the same display.Scene view-models; this paints them
// in color, centered on the round panel. Unknown scenes leave a blank frame.
func colorFrame(s display.Scene) *canvas {
	c := newCanvas()
	c.fill(colBg)
	switch sc := s.(type) {
	case display.SplashScene:
		drawColorSplash(c, sc)
	case display.DogSelectorScene:
		drawColorDogSelector(c, sc)
	case display.LockedSummaryScene:
		drawColorSummary(c, sc)
	case display.SnackModeScene:
		drawColorSnack(c, sc)
	case display.AddInSelectScene:
		drawColorAddInSelect(c, sc)
	}
	return c
}

// text / textScaled bind the shared font blitters to the canvas with a color.
func (c *canvas) text(x, y int, s string, col rgb) int {
	return font.DrawText(x, y, s, func(px, py int) { c.set(px, py, col) })
}

func (c *canvas) textScaled(x, y int, s string, scale int, col rgb) int {
	return font.DrawTextScaled(x, y, s, scale, func(px, py int) { c.set(px, py, col) })
}

// drawCentered draws s centered horizontally at top-of-glyph y, clamped to the
// circle's chord at the text's mid-row.
func drawCentered(c *canvas, s string, y, scale int, col rgb) {
	w := font.TextWidth(s) * scale
	x := centerXAt(w, y+(7*scale)/2)
	c.textScaled(x, y, s, scale, col)
}

// fitScale returns the largest scale ≤ maxScale at which s fits within maxW,
// falling back to 1.
func fitScale(s string, maxW, maxScale int) int {
	for sc := maxScale; sc > 1; sc-- {
		if font.TextWidth(s)*sc <= maxW {
			return sc
		}
	}
	return 1
}

func drawColorSplash(c *canvas, sc display.SplashScene) {
	msg := font.Upper(sc.Message)
	scale := fitScale(msg, 210, 6)
	drawCentered(c, msg, cy-(7*scale)/2, scale, colFg)
}

func drawColorDogSelector(c *canvas, sc display.DogSelectorScene) {
	name := font.Upper(sc.Dog.Name)
	scale := fitScale(name, 200, 6)
	drawCentered(c, name, cy-(7*scale)/2, scale, accentColor(sc.Dog.AccentColor))

	drawCentered(c, "TURN", cy-72, 2, colDim)
	if sc.Total > 1 {
		drawCentered(c, fmt.Sprintf("%d/%d", sc.Index+1, sc.Total), cy+54, 2, colDim)
	}
}

func drawColorSummary(c *canvas, sc display.LockedSummaryScene) {
	drawCentered(c, "FED TODAY", cy-96, 2, colFg)

	const rowH = 26
	n := len(sc.Entries)
	startY := cy - (n*rowH)/2
	for i, e := range sc.Entries {
		y := startY + i*rowH
		// Score swatch on the left, dog name, snack marker on the right.
		c.fillRect(cx-78, y, cx-62, y+16, scoreColor(e.Score))
		name := font.Upper(e.DogName)
		if len(name) > 9 {
			name = name[:9]
		}
		c.textScaled(cx-52, y+1, name, 2, colFg)
		if e.HasSnack {
			c.textScaled(cx+62, y+1, "*", 2, colSnack)
		}
	}

	if !sc.LockedUntil.IsZero() && !sc.Now.IsZero() && sc.LockedUntil.After(sc.Now) {
		rem := sc.LockedUntil.Sub(sc.Now).Round(time.Minute)
		drawCentered(c, "UNLOCK "+formatDuration(rem), cy+92, 2, colDim)
	}
}

func drawColorSnack(c *canvas, sc display.SnackModeScene) {
	drawCentered(c, "SNACK", cy-82, 3, colSnack)

	name := font.Upper(sc.Dog.Name)
	scale := fitScale(name, 200, 6)
	drawCentered(c, name, cy-(7*scale)/2, scale, colFg)

	if sc.Remaining > 0 {
		s := fmt.Sprintf("%dS LEFT", int(sc.Remaining.Round(time.Second).Seconds()))
		drawCentered(c, s, cy+72, 2, colDim)
	}
}

func drawColorAddInSelect(c *canvas, sc display.AddInSelectScene) {
	drawCentered(c, font.Upper(sc.Dog.Name), cy-96, 2, scoreColor(sc.Score))

	if len(sc.Choices) == 0 {
		drawCentered(c, "NO ADD-INS", cy, 2, colDim)
		return
	}

	const (
		maxRows = 4
		rowH    = 28
	)
	start := 0
	if sc.Index >= maxRows {
		start = sc.Index - maxRows + 1
	}
	topY := cy - 40
	for row := 0; row < maxRows; row++ {
		i := start + row
		if i >= len(sc.Choices) {
			break
		}
		y := topY + row*rowH
		label := font.Upper(sc.Choices[i].Label)
		if len(label) > 12 {
			label = label[:12]
		}
		if i == sc.Index {
			// Highlight the selected row with a chord-clamped bar; the caret and
			// label draw on top.
			half := chordHalfWidth(y + 7)
			c.fillRect(cx-half+6, y-4, cx+half-6, y+18, colHilite)
			c.textScaled(centerXAt(font.TextWidth(label)*2, y+7)-16, y, ">", 2, colFg)
		}
		drawCentered(c, label, y, 2, colFg)
	}
}

// scoreColor maps a meal score to its swatch color (dim when no score).
func scoreColor(s domain.Score) rgb {
	switch s {
	case domain.ScoreFull:
		return colFull
	case domain.ScorePartial:
		return colPartial
	case domain.ScoreNone:
		return colNone
	default:
		return colDim
	}
}

// accentColor parses a dog's "#RRGGBB" accent into an rgb, defaulting to white
// when the value is missing or malformed.
func accentColor(hex string) rgb {
	s := strings.TrimPrefix(hex, "#")
	if len(s) != 6 {
		return colFg
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return colFg
	}
	return color565(uint8(v>>16), uint8(v>>8), uint8(v))
}

// formatDuration formats a lock countdown compactly (H:MM, M, or S).
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%d:%02dH", h, m)
	}
	if mins := int(d.Minutes()); mins >= 1 {
		return fmt.Sprintf("%dM", mins)
	}
	return fmt.Sprintf("%dS", int(d.Seconds()))
}

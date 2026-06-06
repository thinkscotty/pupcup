package ui

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"
)

// Panel geometry. The GC9A01 GRAM is square; the glass shows the inscribed
// circle, so everything centers on (CX,CY) and stays within SafeRadius to keep
// content off the curved edge.
const (
	W, H       = 240, 240
	CX, CY     = W / 2, H / 2
	Radius     = W / 2
	SafeInset  = 14
	SafeRadius = Radius - SafeInset // 106
)

// Type sizes (px em at 72 DPI). The design uses two working sizes — a display
// size (SizeBig) and a label size (SizeSmall) — plus SizeHuge for the single
// hero element (the avatar initial or a lone countdown number).
const (
	SizeHuge  = 88.0
	SizeBig   = 46.0
	SizeSmall = 22.0
)

// Palette — bold, saturated tokens for the playful aesthetic. These supersede
// the muted gc9a01/scenes_color.go set; scores keep the green/amber/red intent
// shared with the NeoPixel bar (full / partial / none).
var (
	ColBg      = color.RGBA{0x0A, 0x0B, 0x12, 0xFF} // near-black, faint blue
	ColSurface = color.RGBA{0x1B, 0x1F, 0x30, 0xFF} // raised tracks / ring base
	ColFg      = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	ColDim     = color.RGBA{0x8A, 0x90, 0xA8, 0xFF}
	ColFull    = color.RGBA{0x2E, 0xD9, 0x70, 0xFF} // vivid green  (ate well)
	ColPartial = color.RGBA{0xFF, 0xB1, 0x2E, 0xFF} // amber        (ate ok)
	ColNone    = color.RGBA{0xFF, 0x55, 0x4D, 0xFF} // red          (refused)
	ColSnack   = color.RGBA{0x3D, 0x9E, 0xFF, 0xFF} // blue         (snack)
	ColHilite  = color.RGBA{0x2A, 0x30, 0x5E, 0xFF} // selection bar
	ColAccent  = color.RGBA{0xB4, 0x6B, 0xFF, 0xFF} // default avatar accent
)

// Wash is a time-of-day color treatment: a per-channel multiplicative tint plus
// an overall brightness Dim for night. Apply turns a base palette color into the
// color actually drawn at the current time.
type Wash struct {
	TintR, TintG, TintB float64
	Dim                 float64
}

// neutralWash leaves colors untouched (used until a scene calls SetTime).
var neutralWash = Wash{1, 1, 1, 1}

// Apply tints and dims c, preserving its alpha.
func (w Wash) Apply(c color.RGBA) color.RGBA {
	return color.RGBA{
		R: clamp8(float64(c.R) * w.TintR * w.Dim),
		G: clamp8(float64(c.G) * w.TintG * w.Dim),
		B: clamp8(float64(c.B) * w.TintB * w.Dim),
		A: c.A,
	}
}

// washAnchor is one keyframe in the day's color curve.
type washAnchor struct {
	hour                     float64
	dim, tintR, tintG, tintB float64
}

// washCurve runs cool+dark at night, warm at dawn/dusk, neutral midday. The
// 24:00 anchor mirrors 00:00 so interpolation needs no wrap-around handling.
var washCurve = []washAnchor{
	{0, 0.42, 0.86, 0.92, 1.10},  // deep night, cool
	{6, 0.55, 1.06, 1.00, 0.90},  // dawn, warm
	{9, 1.00, 1.00, 1.00, 1.00},  // morning, neutral
	{14, 1.00, 1.00, 1.00, 1.00}, // afternoon, neutral
	{18, 0.95, 1.10, 0.99, 0.86}, // golden dusk
	{21, 0.62, 1.02, 0.96, 0.98}, // evening winding down
	{24, 0.42, 0.86, 0.92, 1.10}, // == 00:00
}

// WashFor returns the color treatment for a wall-clock time, linearly
// interpolating between the day's anchors.
func WashFor(now time.Time) Wash {
	h := float64(now.Hour()) + float64(now.Minute())/60
	for i := 1; i < len(washCurve); i++ {
		b := washCurve[i]
		if h <= b.hour {
			a := washCurve[i-1]
			t := (h - a.hour) / (b.hour - a.hour)
			return Wash{
				Dim:   lerp(a.dim, b.dim, t),
				TintR: lerp(a.tintR, b.tintR, t),
				TintG: lerp(a.tintG, b.tintG, t),
				TintB: lerp(a.tintB, b.tintB, t),
			}
		}
	}
	return neutralWash
}

// Theme is the per-render visual context: the shared font with its warmed faces,
// and the active time-of-day Wash. One Theme is owned by the render goroutine, so
// SetTime mutation needs no lock.
type Theme struct {
	Font             *Font
	Huge, Big, Small *Face
	wash             Wash
}

// NewTheme loads the embedded font, builds the three working faces, and warms
// their glyph caches so the render loop never rasterizes. Call once at startup.
func NewTheme() (*Theme, error) {
	f, err := LoadFont()
	if err != nil {
		return nil, err
	}
	th := &Theme{
		Font:  f,
		Huge:  f.Face(SizeHuge),
		Big:   f.Face(SizeBig),
		Small: f.Face(SizeSmall),
		wash:  neutralWash,
	}
	th.Huge.Warm(glyphRunes)
	th.Big.Warm(glyphRunes)
	th.Small.Warm(glyphRunes)
	return th, nil
}

// SetTime updates the active Wash for the given wall-clock time. Scenes call this
// once per frame before drawing.
func (th *Theme) SetTime(now time.Time) { th.wash = WashFor(now) }

// Wash returns the active treatment (for tinting non-palette colors like a dog's
// accent, or the NeoPixel mirror in a later phase).
func (th *Theme) Wash() Wash { return th.wash }

// C applies the active Wash to a base palette color — the standard way scenes
// pick a draw color so time-of-day is honored everywhere.
func (th *Theme) C(base color.RGBA) color.RGBA { return th.wash.Apply(base) }

// ScoreColor maps a meal score to its (un-washed) swatch color.
func ScoreColor(s string) color.RGBA {
	switch s {
	case "full":
		return ColFull
	case "partial":
		return ColPartial
	case "none":
		return ColNone
	default:
		return ColDim
	}
}

// ParseAccent parses a dog's "#RRGGBB" accent into a color, falling back to the
// default accent when missing or malformed.
func ParseAccent(hex string) color.RGBA {
	s := strings.TrimPrefix(hex, "#")
	if len(s) != 6 {
		return ColAccent
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return ColAccent
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xFF}
}

// ChordHalfWidth returns half the inscribed circle's horizontal extent at row y
// (0 outside the circle), for clamping wide content off the curved edge.
func ChordHalfWidth(y int) int {
	dy := float64(y - CY)
	d2 := float64(Radius*Radius) - dy*dy
	if d2 <= 0 {
		return 0
	}
	return int(math.Sqrt(d2))
}

// FormatDuration formats a lock countdown compactly: "H:MMH" for an hour or
// more, "NM" for whole minutes, else "NS". Mirrors the legacy color-scene
// formatter so the animated HOME reads the same as the static fallback.
func FormatDuration(d time.Duration) string {
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

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

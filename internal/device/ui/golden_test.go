package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/domain"
)

// updateGolden, when UI_GOLDEN_UPDATE=1, rewrites the golden PNGs instead of
// comparing. The normal run compares and fails on drift.
var updateGolden = os.Getenv("UI_GOLDEN_UPDATE") == "1"

func newPanel() *gg.Context {
	ctx := gg.NewContext(W, H)
	ctx.SetColor(ColBg)
	ctx.Clear()
	return ctx
}

func testTheme(t *testing.T) *Theme {
	t.Helper()
	th, err := NewTheme()
	if err != nil {
		t.Fatalf("NewTheme: %v", err)
	}
	return th
}

// dayNoon and night are fixed times so washed goldens are deterministic.
var (
	dayNoon = time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	night   = time.Date(2026, 6, 6, 2, 0, 0, 0, time.UTC)
)

// checkGolden compares ctx's pixels to testdata/<name>.png, or writes it under
// UI_GOLDEN_UPDATE=1. On mismatch it dumps <name>.got.png for inspection.
func checkGolden(t *testing.T, name string, ctx *gg.Context) {
	t.Helper()
	got := ctx.Image().(*image.RGBA)
	path := filepath.Join("testdata", name+".png")

	if updateGolden {
		writePNG(t, path, got)
		t.Logf("wrote golden %s", path)
		return
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing; regenerate with UI_GOLDEN_UPDATE=1 (%v)", path, err)
	}
	want, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode golden %s: %v", path, err)
	}
	if d := diffImages(got, want); d > 0.002 {
		writePNG(t, filepath.Join("testdata", name+".got.png"), got)
		t.Fatalf("%s differs from golden by %.4f (>%.3f); see %s.got.png", name, d, 0.002, name)
	}
}

// diffImages returns the fraction of channel-samples differing by more than a
// small tolerance — robust to trivial AA jitter, sensitive to real changes.
func diffImages(a *image.RGBA, b image.Image) float64 {
	bnd := a.Bounds()
	if b.Bounds() != bnd {
		return 1
	}
	const tol = 6
	bad, total := 0, 0
	for y := bnd.Min.Y; y < bnd.Max.Y; y++ {
		for x := bnd.Min.X; x < bnd.Max.X; x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			if absDiff(ar, br) > tol<<8 || absDiff(ag, bg) > tol<<8 || absDiff(ab, bb) > tol<<8 {
				bad++
			}
			total++
		}
	}
	return float64(bad) / float64(total)
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

// syntheticPhoto builds a recognizable non-flat image so the circular crop is
// visible in goldens (diagonal gradient + a contrasting blob).
func syntheticPhoto(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(40 + 200*x/w), uint8(60 + 160*y/h), 120, 255})
		}
	}
	// a darker snout blob
	bc := gg.NewContextForRGBA(img)
	bc.SetColor(color.RGBA{30, 20, 25, 255})
	bc.DrawCircle(float64(w)*0.5, float64(h)*0.62, float64(w)*0.18)
	bc.Fill()
	return img
}

// --- the money shot: the HOME centerpiece ---------------------------------

func drawHome(th *Theme, dog domain.Dog, photo *image.RGBA, segments int, filled float64, pips []Pip, selected int, squash float64, now time.Time) *gg.Context {
	ctx := newPanel()
	th.SetTime(now)
	DrawFeedingRing(ctx, th, CX, CY, float64(SafeRadius-6), 12, segments, filled, ColFull)
	DrawAvatar(ctx, th, dog, photo, CX, CY, 78, squash)
	DrawRimPips(ctx, th, pips, selected)
	return ctx
}

func TestGoldenHomeAccent(t *testing.T) {
	th := testTheme(t)
	dog := domain.Dog{Name: "Cleo", AccentColor: "#7C5CFF"}
	pips := []Pip{{ColFull}, {ColPartial}, {ColDim}}
	ctx := drawHome(th, dog, nil, 3, 2, pips, 0, 0, dayNoon)
	checkGolden(t, "home_accent", ctx)
}

func TestGoldenHomePhoto(t *testing.T) {
	th := testTheme(t)
	dog := domain.Dog{Name: "Rio", AccentColor: "#FF8A3D"}
	photo := syntheticPhoto(160, 160)
	pips := []Pip{{ColFull}, {ColFull}, {ColFull}}
	ctx := drawHome(th, dog, photo, 3, 3, pips, 1, 0, dayNoon)
	checkGolden(t, "home_photo", ctx)
}

func TestGoldenHomeNight(t *testing.T) {
	th := testTheme(t)
	dog := domain.Dog{Name: "Otis", AccentColor: "#2ED970"}
	pips := []Pip{{ColDim}, {ColDim}}
	ctx := drawHome(th, dog, nil, 2, 0, pips, 0, 0.18, night)
	checkGolden(t, "home_night", ctx)
}

func TestGoldenRings(t *testing.T) {
	th := testTheme(t)
	th.SetTime(dayNoon)
	ctx := newPanel()
	DrawFeedingRing(ctx, th, 70, 70, 44, 11, 1, 0.66, ColFull)    // continuous
	DrawFeedingRing(ctx, th, 170, 70, 44, 11, 1, 1.0, ColSnack)   // full
	DrawFeedingRing(ctx, th, 70, 170, 44, 11, 4, 2.5, ColPartial) // segmented, 2.5/4
	DrawFeedingRing(ctx, th, 170, 170, 44, 11, 3, 3, ColFull)     // segmented full
	checkGolden(t, "rings", ctx)
}

func TestGoldenBowls(t *testing.T) {
	th := testTheme(t)
	th.SetTime(dayNoon)
	ctx := newPanel()
	DrawBowl(ctx, th, 60, 80, 90, 0.0, ColPartial)
	DrawBowl(ctx, th, 170, 80, 90, 0.5, ColPartial)
	DrawBowl(ctx, th, 120, 175, 110, 1.0, ColFull)
	checkGolden(t, "bowls", ctx)
}

func TestGoldenConfetti(t *testing.T) {
	th := testTheme(t)
	th.SetTime(dayNoon)
	ctx := newPanel()
	c := NewConfetti(th)
	c.Trigger(CX, CY)
	for i := 0; i < 18; i++ { // ~0.3s in, particles spread
		c.Update(16 * time.Millisecond)
	}
	// a faint avatar under it for context
	DrawAvatar(ctx, th, domain.Dog{Name: "Cleo", AccentColor: "#7C5CFF"}, nil, CX, CY, 64, 0)
	c.Draw(ctx)
	checkGolden(t, "confetti", ctx)
}

func TestGoldenSparkle(t *testing.T) {
	th := testTheme(t)
	th.SetTime(dayNoon)
	ctx := newPanel()
	s := NewSparkle(th)
	s.Trigger(CX, CY, 70, ColSnack)
	for i := 0; i < 22; i++ { // ~0.35s, near peak
		s.Update(16 * time.Millisecond)
	}
	DrawAvatar(ctx, th, domain.Dog{Name: "Pip", AccentColor: "#3D9EFF"}, nil, CX, CY, 64, 0)
	s.Draw(ctx)
	checkGolden(t, "sparkle", ctx)
}

func TestGoldenType(t *testing.T) {
	th := testTheme(t)
	th.SetTime(dayNoon)
	ctx := newPanel()
	th.Huge.DrawCentered(ctx, "8", CX, 96, ColFg)
	th.Big.DrawCentered(ctx, "Cleo", CX, 150, ColFull)
	th.Small.DrawCentered(ctx, "UNLOCK 3:42H", CX, 190, ColDim)
	checkGolden(t, "type", ctx)
}

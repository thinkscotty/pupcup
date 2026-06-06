package scenes

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/device/anim"
	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/ui"
	"github.com/scottyturner/pupcup/internal/domain"
)

const frame = 16 * time.Millisecond

// settle is enough frames for the ring-fill spring to reach rest, so goldens
// capture the steady composition rather than a mid-sweep frame.
const settle = 50

var (
	updateGolden = os.Getenv("UI_GOLDEN_UPDATE") == "1"
	dayNoon      = time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
)

func testTheme(t *testing.T) *ui.Theme {
	t.Helper()
	th, err := ui.NewTheme()
	if err != nil {
		t.Fatalf("NewTheme: %v", err)
	}
	return th
}

func newCtx() (*image.RGBA, *gg.Context) {
	img := image.NewRGBA(image.Rect(0, 0, ui.W, ui.H))
	return img, gg.NewContextForRGBA(img)
}

// drive advances s for n frames into a fresh buffer, drawing whenever the scene
// reports dirty rectangles, and returns the resulting image — exactly the
// Update→Draw loop the engine runs.
func drive(s anim.AnimatedScene, n int) *image.RGBA {
	img, ctx := newCtx()
	for i := 0; i < n; i++ {
		if clip := s.Update(frame); len(clip) > 0 {
			s.Draw(ctx, clip)
		}
	}
	return img
}

// --- golden harness (mirrors internal/device/ui/golden_test.go) -------------

func checkGolden(t *testing.T, name string, img *image.RGBA) {
	t.Helper()
	path := filepath.Join("testdata", name+".png")
	if updateGolden {
		writePNG(t, path, img)
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
	if d := diffImages(img, want); d > 0.002 {
		writePNG(t, filepath.Join("testdata", name+".got.png"), img)
		t.Fatalf("%s differs from golden by %.4f (>0.002); see %s.got.png", name, d, name)
	}
}

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

// --- test models ------------------------------------------------------------

func idleModel() HomeModel {
	return HomeModel{
		Mood:     MoodIdle,
		Sel:      DogStat{Dog: domain.Dog{Name: "Cleo", AccentColor: "#7C5CFF"}},
		Pips:     []ui.Pip{{Col: ui.ColFull}, {Col: ui.ColDim}, {Col: ui.ColPartial}},
		Selected: 0,
		Fed:      2,
		Total:    3,
		Now:      dayNoon,
	}
}

func lockedModel() HomeModel {
	return HomeModel{
		Mood:      MoodAllDone,
		Pips:      []ui.Pip{{Col: ui.ColFull}, {Col: ui.ColPartial}, {Col: ui.ColNone}},
		Selected:  -1,
		Fed:       3,
		Total:     3,
		Countdown: 3*time.Hour + 42*time.Minute,
		Now:       dayNoon,
	}
}

// --- golden tests -----------------------------------------------------------

func TestGoldenHomeIdle(t *testing.T) {
	h := NewHome(testTheme(t))
	h.SetModel(idleModel())
	checkGolden(t, "home_idle", drive(h, settle))
}

func TestGoldenHomeLocked(t *testing.T) {
	h := NewHome(testTheme(t))
	h.SetModel(lockedModel())
	checkGolden(t, "home_locked", drive(h, settle))
}

func TestGoldenSnack(t *testing.T) {
	s := NewSnack(testTheme(t))
	s.SetModel(SnackModel{
		Dog:       DogStat{Dog: domain.Dog{Name: "Rio", AccentColor: "#FF8A3D"}},
		Remaining: 45 * time.Second,
		Now:       dayNoon,
	})
	checkGolden(t, "snack", drive(s, settle))
}

func TestGoldenAddIn(t *testing.T) {
	a := NewAddIn(testTheme(t))
	a.SetModel(AddInModel{
		DogName: "Pip",
		Score:   "full",
		Choices: []string{"Chicken", "Pumpkin", "Egg", "Other"},
		Index:   1,
		Now:     dayNoon,
	})
	checkGolden(t, "addin", drive(a, settle))
}

func TestGoldenSplash(t *testing.T) {
	s := NewSplash(testTheme(t))
	s.SetModel(SplashModel{Message: "PupCup", Now: dayNoon})
	checkGolden(t, "splash", drive(s, settle))
}

// --- behavior tests ---------------------------------------------------------

// TestHomeIdleKeepsAnimating: once the ring settles, the idle HOME still reports
// dirty rectangles every frame (the breathing avatar) so it stays alive.
func TestHomeIdleKeepsAnimating(t *testing.T) {
	h := NewHome(testTheme(t))
	h.SetModel(idleModel())
	drive(h, settle)
	if d := h.Update(frame); len(d) == 0 {
		t.Fatal("idle HOME should keep animating (breath), got no dirty rects")
	}
}

// TestHomeLockedHoldsStill: the locked summary settles to a still frame — no
// dirty rectangles — so the engine stops flushing and the panel holds.
func TestHomeLockedHoldsStill(t *testing.T) {
	h := NewHome(testTheme(t))
	h.SetModel(lockedModel())
	drive(h, settle)
	if d := h.Update(frame); len(d) != 0 {
		t.Fatalf("locked HOME should hold still, got dirty %v", d)
	}
}

// TestHomeCelebrateProducesDirty: a celebration on a settled locked screen wakes
// it back up (confetti spans the panel).
func TestHomeCelebrateProducesDirty(t *testing.T) {
	h := NewHome(testTheme(t))
	h.SetModel(lockedModel())
	drive(h, settle)
	h.Celebrate(display.CelebrationEvent{Kind: display.CelebrateMeal, Score: domain.ScoreFull})
	if d := h.Update(frame); len(d) == 0 {
		t.Fatal("a celebration should produce dirty rectangles")
	}
}

// TestSetModelMismatchIgnored: a wrong-typed model is ignored, not a panic, and
// the prior model is retained.
func TestSetModelMismatchIgnored(t *testing.T) {
	h := NewHome(testTheme(t))
	h.SetModel(idleModel())
	h.SetModel("not a HomeModel")
	if h.m.Sel.Dog.Name != "Cleo" {
		t.Fatalf("mismatched SetModel should be ignored; sel = %q", h.m.Sel.Dog.Name)
	}
	_ = drive(h, 3) // must not panic
}

// TestScenesDrawSomething: each scene paints non-background pixels (a smoke test
// that the static bake actually renders content).
func TestScenesDrawSomething(t *testing.T) {
	th := testTheme(t)
	cases := map[string]anim.AnimatedScene{
		"home": func() anim.AnimatedScene { h := NewHome(th); h.SetModel(idleModel()); return h }(),
		"snack": func() anim.AnimatedScene {
			s := NewSnack(th)
			s.SetModel(SnackModel{Dog: DogStat{Dog: domain.Dog{Name: "Rio", AccentColor: "#FF8A3D"}}, Now: dayNoon})
			return s
		}(),
		"addin": func() anim.AnimatedScene {
			a := NewAddIn(th)
			a.SetModel(AddInModel{DogName: "Pip", Score: "full", Choices: []string{"Chicken"}, Now: dayNoon})
			return a
		}(),
		"splash": func() anim.AnimatedScene {
			s := NewSplash(th)
			s.SetModel(SplashModel{Message: "HI", Now: dayNoon})
			return s
		}(),
	}
	for name, s := range cases {
		img := drive(s, settle)
		if !hasNonBG(img) {
			t.Errorf("%s scene drew nothing", name)
		}
	}
}

func hasNonBG(img *image.RGBA) bool {
	bg := ui.ColBg
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i] != bg.R || img.Pix[i+1] != bg.G || img.Pix[i+2] != bg.B {
			return true
		}
	}
	return false
}

// TestEngineDrivesHomeSmoke runs a real scene through the real engine + flush
// goroutines and the Start/Close lifecycle — the exact pipeline the GC9A01 driver
// uses. Worth running under -race to catch any cross-goroutine slip.
func TestEngineDrivesHomeSmoke(t *testing.T) {
	fake := display.NewFake()
	e := anim.New(fake, ui.W, ui.H, slog.New(slog.NewTextHandler(io.Discard, nil)), anim.WithFPS(120))
	home := NewHome(testTheme(t))
	e.SetScene(home)
	e.SetModel(idleModel())
	e.Start()

	deadline := time.Now().Add(2 * time.Second)
	for e.Stats().Flushed < 3 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	// A celebration mid-run must be handled on the render goroutine without a race.
	e.Celebrate(display.CelebrationEvent{Kind: display.CelebrateMeal, Score: domain.ScoreFull})
	time.Sleep(50 * time.Millisecond)

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := e.Stats().Flushed; got < 3 {
		t.Fatalf("Flushed = %d after run, want >= 3", got)
	}
}

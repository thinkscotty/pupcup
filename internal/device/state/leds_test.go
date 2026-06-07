package state

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/neopixel"
	"github.com/scottyturner/pupcup/internal/domain"
)

func quietLED() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestLEDBaseFrame pins the un-animated bar the animator paints for each mode —
// the LED analog of the screen's ring + pips, and what the run loop's intent
// drives. Animation (glow/burst/dim) is layered on top and not tested here.
func TestLEDBaseFrame(t *testing.T) {
	g, y, r := ledFull, ledPartial, ledNone
	off := neopixel.ColorOff

	cases := []struct {
		name   string
		mode   Mode
		scores []neopixel.Color
		want   []neopixel.Color
	}{
		{
			name: "locked 3 dogs: g-gap-y-gap-r",
			mode: ModeLockedSummary, scores: []neopixel.Color{g, y, r},
			want: []neopixel.Color{g, g, off, y, y, off, r, r},
		},
		{
			name: "idle fills as dogs are fed (2 dogs, first fed)",
			mode: ModeIdle, scores: []neopixel.Color{g, off},
			want: []neopixel.Color{g, g, g, g, off, off, off, off},
		},
		{
			name: "idle nobody fed -> dark",
			mode: ModeIdle, scores: []neopixel.Color{off, off, off},
			want: []neopixel.Color{off, off, off, off, off, off, off, off},
		},
		{
			name: "locked 4 dogs -> solid green fallback",
			mode: ModeLockedSummary, scores: []neopixel.Color{g, g, g, g},
			want: []neopixel.Color{g, g, g, g, g, g, g, g},
		},
		{
			name: "idle 4 dogs -> dark (no layout)",
			mode: ModeIdle, scores: []neopixel.Color{g, g, g, g},
			want: []neopixel.Color{off, off, off, off, off, off, off, off},
		},
		{
			name: "snack -> solid blue base",
			mode: ModeSnackMode, scores: nil,
			want: []neopixel.Color{ledSnackBase, ledSnackBase, ledSnackBase, ledSnackBase, ledSnackBase, ledSnackBase, ledSnackBase, ledSnackBase},
		},
		{
			name: "add-in -> solid amber",
			mode: ModeAddInSelect, scores: nil,
			want: []neopixel.Color{ledAmber, ledAmber, ledAmber, ledAmber, ledAmber, ledAmber, ledAmber, ledAmber},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ledBaseFrame(c.mode, c.scores, 8)
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("[%d] = %+v, want %+v (full %+v)", i, got[i], c.want[i], got)
				}
			}
		})
	}
}

// TestRenderLEDFrameDim confirms night-dim scales brightness and a burst lifts it.
func TestRenderLEDFrameDim(t *testing.T) {
	frame := make([]neopixel.Color, 8)
	in := ledIntent{mode: ModeLockedSummary, scores: []neopixel.Color{ledFull, ledFull, ledFull}, dim: 0.5}
	// phase=pi/2 → glow at its peak for locked (0.70+0.30=1.0), no burst.
	renderLEDFrame(frame, in, 1.5708, 0, neopixel.Color{})
	full := renderedAt(ModeLockedSummary, []neopixel.Color{ledFull, ledFull, ledFull}, 1.0)
	if frame[0].G == 0 || frame[0].G >= full[0].G {
		t.Fatalf("dim should darken: dimmed G=%d full G=%d", frame[0].G, full[0].G)
	}
	// A burst adds brightness on top.
	burst := make([]neopixel.Color, 8)
	renderLEDFrame(burst, in, 1.5708, 1.0, neopixel.Color{G: 160})
	if burst[0].G <= frame[0].G {
		t.Fatalf("burst should brighten: burst G=%d base G=%d", burst[0].G, frame[0].G)
	}
}

func renderedAt(mode Mode, scores []neopixel.Color, dim float64) []neopixel.Color {
	f := make([]neopixel.Color, 8)
	renderLEDFrame(f, ledIntent{mode: mode, scores: scores, dim: dim}, 1.5708, 0, neopixel.Color{})
	return f
}

// TestLEDAnimatorRendersAndStops drives the real animator goroutine against a
// Fake strip: it renders frames for a locked intent, survives a celebration, and
// blanks the strip on stop. Worth running under -race.
func TestLEDAnimatorRendersAndStops(t *testing.T) {
	f := neopixel.NewFake(8)
	a := newLedAnimator(f, quietLED(), 120)
	a.start()
	a.setIntent(ledIntent{mode: ModeLockedSummary, scores: []neopixel.Color{ledFull, ledFull, ledFull}, dim: 1})

	deadline := time.Now().Add(2 * time.Second)
	for f.FrameCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if f.FrameCount() == 0 {
		t.Fatal("animator rendered no frames")
	}
	lit := false
	for _, c := range f.LastFrame() {
		if c.G > 0 {
			lit = true
		}
	}
	if !lit {
		t.Fatal("locked summary should light green on the bar")
	}

	a.celebrate(display.CelebrationEvent{Kind: display.CelebrateMeal, Score: domain.ScoreFull})
	time.Sleep(40 * time.Millisecond)

	a.stop()
	a.stop() // idempotent

	for i, c := range f.LastFrame() {
		if c != neopixel.ColorOff {
			t.Fatalf("strip not blanked after stop: [%d] = %+v", i, c)
		}
	}
}

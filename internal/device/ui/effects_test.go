package ui

import (
	"math"
	"testing"
	"time"
)

const frame = 16 * time.Millisecond

// runEffect ticks e until it reports done or a tick cap, returning the count.
func runEffect(e Effect, max int) int {
	for i := 0; i < max; i++ {
		if !e.Update(frame) {
			return i + 1
		}
	}
	return max
}

func TestBounceLifecycle(t *testing.T) {
	b := NewBounce()
	if b.Update(frame) {
		t.Fatal("un-triggered bounce should be inactive")
	}
	if b.Squash() != 0 {
		t.Fatal("resting squash should be 0")
	}
	b.Trigger(0.4)
	// Starts compressed (near +amp), decays to ~0.
	if s := b.Squash(); s < 0.3 {
		t.Fatalf("just-triggered squash = %.3f, want near +0.4", s)
	}
	n := runEffect(b, 200)
	if n >= 200 {
		t.Fatal("bounce never finished")
	}
	if b.Squash() != 0 {
		t.Fatalf("finished bounce squash = %.3f, want 0", b.Squash())
	}
}

func TestConfettiLifecycle(t *testing.T) {
	c := NewConfetti(nil)
	if c.Update(frame) {
		t.Fatal("un-triggered confetti should be inactive")
	}
	c.Trigger(CX, CY)
	n := runEffect(c, 600)
	if n < 10 || n >= 600 {
		t.Fatalf("confetti lasted %d frames, want a bounded burst", n)
	}
}

func TestLoopingEnvelopesAlwaysActive(t *testing.T) {
	br := NewIdleBreath()
	bl := NewBlink()
	for i := 0; i < 300; i++ {
		if !br.Update(frame) || !bl.Update(frame) {
			t.Fatal("looping envelope reported done")
		}
	}
	if s := br.Scale(); s < 0.9 || s > 1.1 {
		t.Fatalf("breath scale %.3f out of expected ±10%%", s)
	}
}

func TestBlinkClosesThenOpens(t *testing.T) {
	b := NewBlink()
	open, closed := false, false
	for i := 0; i < 400; i++ { // > one full interval+close
		b.Update(frame)
		o := b.Openness()
		if o >= 0.99 {
			open = true
		}
		if o <= 0.2 {
			closed = true
		}
	}
	if !open || !closed {
		t.Fatalf("blink should both open and close over time: open=%v closed=%v", open, closed)
	}
}

func TestDroopApproachesTarget(t *testing.T) {
	d := NewDroop()
	d.Set(true)
	for i := 0; i < 120; i++ {
		d.Update(frame)
	}
	if a := d.Amount(); a < 0.95 {
		t.Fatalf("droop should settle near 1, got %.3f", a)
	}
	d.Set(false)
	for i := 0; i < 120; i++ {
		d.Update(frame)
	}
	if a := d.Amount(); a != 0 {
		t.Fatalf("un-drooped should settle exactly at 0, got %.3f", a)
	}
}

// TestEffectsUpdateZeroAlloc proves the per-frame Update path allocates nothing
// (the fixed-size particle pools and scalar envelopes were the point).
func TestEffectsUpdateZeroAlloc(t *testing.T) {
	c := NewConfetti(nil)
	c.Trigger(CX, CY)
	s := NewSparkle(nil)
	s.Trigger(CX, CY, 70, ColSnack)
	b := NewBounce()
	b.Trigger(0.4)
	br := NewIdleBreath()
	d := NewDroop()
	d.Set(true)

	cases := map[string]func(){
		"confetti": func() { c.active = true; c.Update(frame) }, // keep alive
		"sparkle":  func() { s.active = true; s.Update(frame) },
		"bounce":   func() { b.active = true; b.Update(frame) },
		"breath":   func() { br.Update(frame) },
		"droop":    func() { d.Update(frame) },
	}
	for name, fn := range cases {
		if a := testing.AllocsPerRun(500, fn); a != 0 {
			t.Errorf("%s.Update allocated %.2f objs/op, want 0", name, a)
		}
	}
}

func TestWashFor(t *testing.T) {
	day := WashFor(dayNoon)
	if day.Dim < 0.99 {
		t.Fatalf("noon should be full brightness, dim=%.3f", day.Dim)
	}
	nite := WashFor(night)
	if nite.Dim > 0.6 {
		t.Fatalf("2am should be dimmed, dim=%.3f", nite.Dim)
	}
	if nite.TintB <= nite.TintR {
		t.Fatalf("night should skew cool (B>R), got R=%.2f B=%.2f", nite.TintR, nite.TintB)
	}
	// Apply dims a known color.
	c := nite.Apply(ColFull)
	if int(c.G) >= int(ColFull.G) {
		t.Fatalf("night wash should darken green: %d -> %d", ColFull.G, c.G)
	}
}

func TestChordHalfWidth(t *testing.T) {
	if w := ChordHalfWidth(CY); w != Radius {
		t.Fatalf("chord at center = %d, want %d", w, Radius)
	}
	if w := ChordHalfWidth(0); w != 0 {
		t.Fatalf("chord at top edge = %d, want 0", w)
	}
	if math.Abs(float64(ChordHalfWidth(CY+50)-ChordHalfWidth(CY-50))) > 1 {
		t.Fatal("chord should be symmetric about the center row")
	}
}

// TestDrawStringOffCanvas ensures the glyph blitter clips rather than panics when
// text lands partly or wholly outside the buffer.
func TestDrawStringOffCanvas(t *testing.T) {
	th := testTheme(t)
	ctx := newPanel()
	th.Big.DrawString(ctx, "EDGE", -40, 10, ColFg)   // off left/top
	th.Big.DrawString(ctx, "EDGE", W-10, H-2, ColFg) // off right/bottom
	// no panic == pass
}

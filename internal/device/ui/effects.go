package ui

import (
	"image/color"
	"math"
	"time"

	"github.com/fogleman/gg"
)

// Effect is one animated flourish. Update advances it by dt and returns whether
// it is still active (false once finished, so a scene can drop it). Looping
// envelopes (idle breathing, blinking) always return true. Draw paints overlay
// effects (confetti, sparkles); modulating envelopes (bounce, droop, breath,
// blink) have a no-op Draw and instead expose a scalar the scene applies to the
// avatar.
//
// Update is allocation-free; Draw uses gg (which allocates path state), so heavy
// effects animate small regions and lean on the engine's GC tuning.
type Effect interface {
	Update(dt time.Duration) bool
	Draw(ctx *gg.Context)
}

var (
	_ Effect = (*Bounce)(nil)
	_ Effect = (*IdleBreath)(nil)
	_ Effect = (*Blink)(nil)
	_ Effect = (*Droop)(nil)
	_ Effect = (*Confetti)(nil)
	_ Effect = (*Sparkle)(nil)
)

// --- Bounce: a one-shot squash-&-stretch envelope -------------------------

// Bounce is a decaying-oscillation envelope for an impact reaction. Trigger it
// when a meal is recorded; feed Squash into DrawAvatar each frame.
type Bounce struct {
	t, dur, amp float64
	active      bool
}

// NewBounce returns an idle bounce (~0.55 s when triggered).
func NewBounce() *Bounce { return &Bounce{dur: 0.55} }

// Trigger starts a bounce with the given peak squash amplitude (~0.4 reads as a
// lively pop).
func (b *Bounce) Trigger(amp float64) { b.t, b.amp, b.active = 0, amp, true }

func (b *Bounce) Update(dt time.Duration) bool {
	if !b.active {
		return false
	}
	b.t += dt.Seconds()
	if b.t >= b.dur {
		b.active = false
	}
	return b.active
}

// Squash is the current squash factor for DrawAvatar (0 when at rest): it starts
// compressed and wobbles to rest over a couple of cycles.
func (b *Bounce) Squash() float64 {
	if !b.active {
		return 0
	}
	x := b.t / b.dur
	return b.amp * math.Exp(-4.5*x) * math.Cos(2*math.Pi*2.0*x)
}

func (b *Bounce) Draw(*gg.Context) {}

// --- IdleBreath: a continuous resting scale -------------------------------

// IdleBreath is a slow looping scale oscillation that keeps a resting avatar
// feeling alive.
type IdleBreath struct{ phase, period, amp float64 }

// NewIdleBreath returns a ~3.6 s breathing cycle of ±2.5% scale.
func NewIdleBreath() *IdleBreath { return &IdleBreath{period: 3.6, amp: 0.025} }

func (b *IdleBreath) Update(dt time.Duration) bool {
	b.phase += 2 * math.Pi * dt.Seconds() / b.period
	if b.phase > 2*math.Pi {
		b.phase -= 2 * math.Pi
	}
	return true
}

// Scale is the current resting scale multiplier (~1.0).
func (b *IdleBreath) Scale() float64 { return 1 + b.amp*math.Sin(b.phase) }

func (b *IdleBreath) Draw(*gg.Context) {}

// --- Blink: a periodic eye-close envelope ---------------------------------

// Blink is a deterministic open/close cadence. It carries no art of its own;
// scenes that draw eyes read Openness to drive them. Deterministic (no rand) so
// golden tests are stable.
type Blink struct{ t, interval, closeDur float64 }

// NewBlink returns a blink every ~4 s, closing for ~0.16 s.
func NewBlink() *Blink { return &Blink{interval: 4.0, closeDur: 0.16} }

func (b *Blink) Update(dt time.Duration) bool {
	b.t += dt.Seconds()
	if b.t >= b.interval+b.closeDur {
		b.t -= b.interval + b.closeDur
	}
	return true
}

// Openness is 1 with eyes open, dipping to 0 during the brief close window.
func (b *Blink) Openness() float64 {
	if b.t < b.interval {
		return 1
	}
	x := (b.t - b.interval) / b.closeDur // 0..1 across the close
	return 1 - triangle(x)
}

func (b *Blink) Draw(*gg.Context) {}

// --- Droop: an eased sad-state offset -------------------------------------

// Droop is a held envelope for a poor-meal mood: Set(true) eases it down and it
// stays until Set(false). Amount drives a downward avatar offset / tilt applied
// by the scene.
type Droop struct{ amount, target float64 }

// NewDroop returns an upright (un-drooped) envelope.
func NewDroop() *Droop { return &Droop{} }

// Set chooses the resting target: drooped (true) or upright (false).
func (d *Droop) Set(on bool) {
	if on {
		d.target = 1
	} else {
		d.target = 0
	}
}

func (d *Droop) Update(dt time.Duration) bool {
	// Frame-rate-independent exponential approach toward the target.
	d.amount = d.target + (d.amount-d.target)*math.Exp(-5*dt.Seconds())
	if math.Abs(d.target-d.amount) < 0.001 {
		d.amount = d.target
	}
	return d.amount != 0 || d.target != 0
}

// Amount is the current droop 0..1 (0 upright, 1 fully drooped).
func (d *Droop) Amount() float64 { return d.amount }

func (d *Droop) Draw(*gg.Context) {}

// --- Confetti: a one-shot particle burst ----------------------------------

const confettiN = 28

var confettiPalette = [...]color.RGBA{ColFull, ColPartial, ColSnack, ColAccent, ColFg}

type particle struct {
	x, y, vx, vy, ang, angVel, life float64
	col                             color.RGBA
}

// Confetti is a celebratory burst from a point. Particles fly outward with an
// upward bias, fall under gravity, spin, and fade. The pool is fixed-size, so
// Update never allocates.
type Confetti struct {
	p      [confettiN]particle
	th     *Theme
	active bool
}

// NewConfetti returns an idle burst; th tints particles for time-of-day.
func NewConfetti(th *Theme) *Confetti { return &Confetti{th: th} }

// Trigger launches a burst centered on (cx,cy).
func (c *Confetti) Trigger(cx, cy float64) {
	c.active = true
	for i := range c.p {
		a := float64(i)*(2*math.Pi/confettiN) + 0.6*frac(float64(i)*0.7)
		sp := 90 + 130*frac(float64(i)*0.61803)
		c.p[i] = particle{
			x: cx, y: cy,
			vx:     math.Cos(a) * sp,
			vy:     math.Sin(a)*sp - 70, // upward bias
			ang:    frac(float64(i)*0.123) * 2 * math.Pi,
			angVel: (frac(float64(i)*0.31) - 0.5) * 14,
			life:   1,
			col:    confettiPalette[i%len(confettiPalette)],
		}
	}
}

func (c *Confetti) Update(dt time.Duration) bool {
	if !c.active {
		return false
	}
	s := dt.Seconds()
	alive := false
	for i := range c.p {
		p := &c.p[i]
		if p.life <= 0 {
			continue
		}
		p.vy += 340 * s // gravity
		p.x += p.vx * s
		p.y += p.vy * s
		p.ang += p.angVel * s
		p.life -= s / 1.6
		if p.life > 0 {
			alive = true
		}
	}
	if !alive {
		c.active = false
	}
	return c.active
}

func (c *Confetti) Draw(ctx *gg.Context) {
	if !c.active {
		return
	}
	for i := range c.p {
		p := &c.p[i]
		if p.life <= 0 {
			continue
		}
		col := p.col
		if c.th != nil {
			col = c.th.Wash().Apply(col)
		}
		ctx.Push()
		ctx.Translate(p.x, p.y)
		ctx.Rotate(p.ang)
		setRGBAFade(ctx, col, clamp01(p.life))
		ctx.DrawRectangle(-3, -2, 6, 4)
		ctx.Fill()
		ctx.Pop()
	}
}

// --- Sparkle: twinkling stars around a point ------------------------------

const sparkleN = 6

type spark struct{ x, y, phase, size float64 }

// Sparkle scatters a handful of twinkling 4-point stars around a center for a
// gentle positive beat (a snack, a good-but-quiet moment). Fixed-size; Update
// allocation-free.
type Sparkle struct {
	s      [sparkleN]spark
	th     *Theme
	t, dur float64
	col    color.RGBA
	active bool
}

// NewSparkle returns an idle sparkle (~1.4 s when triggered).
func NewSparkle(th *Theme) *Sparkle { return &Sparkle{th: th, dur: 1.4, col: ColSnack} }

// Trigger scatters stars in col on a ring of radius r around (cx,cy).
func (s *Sparkle) Trigger(cx, cy, r float64, col color.RGBA) {
	s.active, s.t, s.col = true, 0, col
	for i := range s.s {
		a := float64(i)*(2*math.Pi/sparkleN) + 0.5
		rr := r * (0.7 + 0.5*frac(float64(i)*0.61803))
		s.s[i] = spark{
			x:     cx + rr*math.Cos(a),
			y:     cy + rr*math.Sin(a),
			phase: frac(float64(i)*0.37) * 2 * math.Pi,
			size:  4 + 3*frac(float64(i)*0.9),
		}
	}
}

func (s *Sparkle) Update(dt time.Duration) bool {
	if !s.active {
		return false
	}
	s.t += dt.Seconds()
	for i := range s.s {
		s.s[i].phase += dt.Seconds() * 6
	}
	if s.t >= s.dur {
		s.active = false
	}
	return s.active
}

func (s *Sparkle) Draw(ctx *gg.Context) {
	if !s.active {
		return
	}
	env := math.Sin(math.Pi * clamp01(s.t/s.dur)) // fade in then out
	col := s.col
	if s.th != nil {
		col = s.th.Wash().Apply(col)
	}
	for i := range s.s {
		sp := &s.s[i]
		tw := 0.5 + 0.5*math.Sin(sp.phase)
		a := env * tw
		if a <= 0.02 {
			continue
		}
		drawStar(ctx, sp.x, sp.y, sp.size*(0.6+0.4*tw), col, a)
	}
}

// --- shared helpers -------------------------------------------------------

// drawStar fills a 4-point twinkle star at (x,y) with outer radius r.
func drawStar(ctx *gg.Context, x, y, r float64, col color.RGBA, alpha float64) {
	setRGBAFade(ctx, col, alpha)
	const spikes = 4
	inner := r * 0.38
	ctx.NewSubPath()
	for i := 0; i < spikes*2; i++ {
		rad := r
		if i%2 == 1 {
			rad = inner
		}
		ang := -math.Pi/2 + float64(i)*math.Pi/float64(spikes)
		px, py := x+rad*math.Cos(ang), y+rad*math.Sin(ang)
		if i == 0 {
			ctx.MoveTo(px, py)
		} else {
			ctx.LineTo(px, py)
		}
	}
	ctx.ClosePath()
	ctx.Fill()
}

// setRGBAFade sets ctx's color to col at a straight (non-premultiplied) alpha,
// the safe way to fade since color.RGBA is premultiplied.
func setRGBAFade(ctx *gg.Context, col color.RGBA, alpha float64) {
	ctx.SetRGBA(float64(col.R)/255, float64(col.G)/255, float64(col.B)/255, clamp01(alpha))
}

// frac returns the fractional part of x in [0,1) — a cheap deterministic spread
// for per-particle variety without a RNG.
func frac(x float64) float64 { return x - math.Floor(x) }

// triangle is a 0→1→0 ramp over x∈[0,1].
func triangle(x float64) float64 { return 1 - math.Abs(2*x-1) }

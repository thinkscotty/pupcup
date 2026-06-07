package state

import (
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/neopixel"
	"github.com/scottyturner/pupcup/internal/domain"
)

const (
	ledFPS        = 30
	ledGlowPeriod = 3.2 // seconds — the locked "all done" glow / snack breath
	ledBurstDur   = 0.7 // seconds — celebration flash decay
)

// LED base colors layered by the animator. The score colors (ledFull/Partial/
// None) live in state.go alongside scoreColor; these are the non-score bases.
var (
	ledSnackBase = neopixel.Color{B: 40}        // breathed by the snack glow
	ledAmber     = neopixel.Color{R: 36, G: 20} // steady add-in picker
)

// ledIntent is the steady LED state handed to the animator on each render
// (latest-wins). The animator layers the breathing glow, celebration bursts, and
// night-dim on top of it.
type ledIntent struct {
	mode   Mode
	scores []neopixel.Color // per-dog status (idle progress + locked summary)
	dim    float64          // night-dim brightness multiplier (ui.Wash)
}

// ledAnimator owns the NeoPixel strip and renders it on its own goroutine at
// ledFPS, so smooth pulses don't hinge on the once-a-second state render and the
// strip (SPI0) stays single-threaded — the run loop never touches it directly. It
// mirrors the screen engine's control model: latest-wins intent + best-effort
// one-shot celebrations, both delivered over channels.
type ledAnimator struct {
	leds neopixel.Strip
	log  *slog.Logger
	fps  int

	intent chan ledIntent
	celeb  chan display.CelebrationEvent
	quit   chan struct{}
	done   chan struct{}

	startOnce sync.Once
	stopOnce  sync.Once
}

func newLedAnimator(leds neopixel.Strip, log *slog.Logger, fps int) *ledAnimator {
	if log == nil {
		log = slog.Default()
	}
	return &ledAnimator{
		leds:   leds,
		log:    log.With("subcomponent", "leds"),
		fps:    fps,
		intent: make(chan ledIntent, 1),
		celeb:  make(chan display.CelebrationEvent, 8),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// start launches the render goroutine (idempotent).
func (a *ledAnimator) start() { a.startOnce.Do(func() { go a.run() }) }

// stop ends the render goroutine, blanks the strip, and waits for it to exit.
// Idempotent; valid only after start (the run loop is what closes done).
func (a *ledAnimator) stop() {
	a.stopOnce.Do(func() {
		close(a.quit)
		<-a.done
	})
}

// setIntent hands the animator a fresh steady state (latest-wins, non-blocking).
// Safe before start: the cap-1 channel holds the newest until the loop runs.
func (a *ledAnimator) setIntent(in ledIntent) {
	for {
		select {
		case a.intent <- in:
			return
		default:
			select {
			case <-a.intent:
			default:
			}
		}
	}
}

// celebrate queues a one-shot burst (best-effort; dropped if the buffer is full).
func (a *ledAnimator) celebrate(ev display.CelebrationEvent) {
	select {
	case a.celeb <- ev:
	default:
	}
}

// run is the render goroutine: drains control channels each tick, advances the
// glow phase + burst decay, composites the frame, and writes the strip.
func (a *ledAnimator) run() {
	defer close(a.done)
	n := a.leds.N()
	if n <= 0 {
		<-a.quit // nothing to drive; just wait to be stopped
		return
	}
	frame := make([]neopixel.Color, n)
	ticker := time.NewTicker(time.Second / time.Duration(a.fps))
	defer ticker.Stop()

	cur := ledIntent{dim: 1}
	var phase, burst float64
	var burstCol neopixel.Color
	last := time.Now()
	for {
		select {
		case <-a.quit:
			a.blank(frame)
			return
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now

			select { // latest intent
			case in := <-a.intent:
				cur = in
			default:
			}
			for drained := false; !drained; { // all pending celebrations
				select {
				case ev := <-a.celeb:
					burst, burstCol = 1, burstColor(ev)
				default:
					drained = true
				}
			}

			phase += 2 * math.Pi * dt / ledGlowPeriod
			if phase > 2*math.Pi {
				phase -= 2 * math.Pi
			}
			if burst > 0 {
				if burst -= dt / ledBurstDur; burst < 0 {
					burst = 0
				}
			}
			renderLEDFrame(frame, cur, phase, burst, burstCol)
			a.write(frame)
		}
	}
}

func (a *ledAnimator) write(frame []neopixel.Color) {
	for i, c := range frame {
		if err := a.leds.SetPixel(i, c); err != nil {
			a.log.Warn("led setpixel", "i", i, "err", err)
			return
		}
	}
	if err := a.leds.Show(); err != nil {
		a.log.Warn("led show", "err", err)
	}
}

func (a *ledAnimator) blank(frame []neopixel.Color) {
	for i := range frame {
		frame[i] = neopixel.ColorOff
	}
	a.write(frame)
}

// ledBaseFrame is the un-animated LED bar for a mode + per-dog scores. The
// animator layers glow/burst/dim on top; this pure base is what tests pin and is
// the LED analog of the screen's feeding ring + pips.
func ledBaseFrame(mode Mode, scores []neopixel.Color, n int) []neopixel.Color {
	frame := make([]neopixel.Color, n)
	switch mode {
	case ModeIdle, ModeLockedSummary:
		// Per-dog status across the bar: idle fills as dogs are fed; locked is the
		// final summary. summaryFrame returns nil for layouts it can't place.
		if seg := summaryFrame(n, scores); seg != nil {
			copy(frame, seg)
		} else if mode == ModeLockedSummary {
			fillFrame(frame, ledFull) // 4+ dogs: solid green once the meal is done
		}
	case ModeSnackMode:
		fillFrame(frame, ledSnackBase)
	case ModeAddInSelect:
		fillFrame(frame, ledAmber)
	}
	return frame
}

// renderLEDFrame composites the animated bar into frame: the base scaled by the
// mode's breathing glow, a decaying celebration burst added on top, all dimmed
// for night.
func renderLEDFrame(frame []neopixel.Color, in ledIntent, phase, burst float64, burstCol neopixel.Color) {
	base := ledBaseFrame(in.mode, in.scores, len(frame))
	glow := modeGlow(in.mode, phase)
	dim := in.dim
	if dim <= 0 {
		dim = 1
	}
	for i := range frame {
		c := scaleColor(base[i], glow*dim)
		if burst > 0 {
			c = addColor(c, scaleColor(burstCol, burst*dim))
		}
		frame[i] = c
	}
}

// modeGlow is the brightness envelope per mode: a gentle "all done" glow on the
// locked bar, a stronger blue breath in snack mode, steady otherwise.
func modeGlow(mode Mode, phase float64) float64 {
	s := 0.5 + 0.5*math.Sin(phase)
	switch mode {
	case ModeLockedSummary:
		return 0.70 + 0.30*s
	case ModeSnackMode:
		return 0.35 + 0.65*s
	default:
		return 1.0
	}
}

// burstColor is the celebration flash color: blue for a snack, else a bright
// take on the meal's score color.
func burstColor(ev display.CelebrationEvent) neopixel.Color {
	if ev.Kind == display.CelebrateSnack {
		return neopixel.Color{B: 140}
	}
	switch ev.Score {
	case domain.ScoreFull:
		return neopixel.Color{G: 160}
	case domain.ScorePartial:
		return neopixel.Color{R: 150, G: 90}
	case domain.ScoreNone:
		return neopixel.Color{R: 160}
	default:
		return neopixel.Color{G: 120}
	}
}

func scaleColor(c neopixel.Color, f float64) neopixel.Color {
	return neopixel.Color{R: scale8(c.R, f), G: scale8(c.G, f), B: scale8(c.B, f)}
}

func addColor(a, b neopixel.Color) neopixel.Color {
	return neopixel.Color{R: add8(a.R, b.R), G: add8(a.G, b.G), B: add8(a.B, b.B)}
}

func scale8(v uint8, f float64) uint8 {
	x := float64(v) * f
	if x <= 0 {
		return 0
	}
	if x >= 255 {
		return 255
	}
	return uint8(x + 0.5)
}

func add8(a, b uint8) uint8 {
	if s := int(a) + int(b); s < 255 {
		return uint8(s)
	}
	return 255
}

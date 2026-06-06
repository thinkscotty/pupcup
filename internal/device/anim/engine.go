// Package anim is PupCup's platform-independent animation runtime for the round
// LCD. It owns an off-screen RGBA back buffer and a reused gg drawing context,
// runs a fixed-timestep render loop, packs only the changed pixels to RGB565,
// and streams them to the panel through a display.RectFlusher — all without
// per-frame allocation once running.
//
// The concurrency model is the one validated by the lcdperf perf spike and is
// the heart of the package: three roles on separate goroutines.
//
//   - The state machine (caller) owns input and state. It hands the engine fresh
//     scene snapshots (SetModel), scene swaps (SetScene) and one-shot reactions
//     (Celebrate) through channels and never blocks on rendering.
//   - The render goroutine (run) is the sole owner of the scene, the gg context
//     and the RGBA buffer. Each tick it advances the active AnimatedScene, draws
//     the changed regions, packs them into a pooled RGB565 buffer and pushes a
//     job to a capacity-1 latest-wins channel.
//   - The flush goroutine (runFlusher) is the sole owner of the panel/SPI. It
//     drains jobs and calls FlushRect; the latest-wins channel means a slow flush
//     drops stale frames instead of stalling the render loop.
//
// Everything here is pure Go (no build tag): it compiles and is unit-tested on
// macOS against display.Fake exactly as on the Pi, with only the FlushRect byte
// I/O being platform-specific (and supplied by the gc9a01 driver).
package anim

import (
	"image"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/device/display"
)

// nbuf is the RGB565 buffer pool size. With at most one frame queued and one in
// flight, a third buffer is always free for the renderer, so the render
// goroutine never blocks on the flusher. (This is the triple-buffering the
// lcdperf async pipeline validated.)
const nbuf = 3

// defaultFPS is the render loop's target rate when WithFPS is not given.
const defaultFPS = 60

// AnimatedScene is one screen the engine can drive. The engine calls every
// method on its single render goroutine, so a scene needs no internal locking:
//
//   - Update advances animation state by dt and returns the rectangles that
//     changed since the last frame. Returning nil/empty means nothing moved, so
//     the engine skips drawing and flushing entirely (the panel holds the last
//     frame). To keep the loop allocation-free, return a reused slice.
//   - Draw paints the current frame into the shared gg context, confining its
//     work to clip (the same rectangles Update returned, or the full panel on a
//     scene change). The back buffer persists between frames, so a scene redraws
//     only what changed rather than clearing the whole frame.
//   - SetModel swaps in a fresh immutable state snapshot from the state machine.
//   - Celebrate fires a one-shot reaction (a burst, a sparkle) layered over the
//     steady state.
type AnimatedScene interface {
	Update(dt time.Duration) []Rect
	Draw(ctx *gg.Context, clip []Rect)
	SetModel(m any)
	Celebrate(ev display.CelebrationEvent)
}

// Option configures an Engine at construction.
type Option func(*Engine)

// WithFPS sets the render loop's target frame rate (ignored if fps <= 0).
func WithFPS(fps int) Option {
	return func(e *Engine) {
		if fps > 0 {
			e.fps = fps
		}
	}
}

// Engine owns the back buffer and runs the render→pack→flush pipeline. Construct
// it with New, drive it with Start, and feed it through SetScene/SetModel/
// Celebrate; stop it with Close.
type Engine struct {
	w, h int
	rgba *image.RGBA
	ctx  *gg.Context
	log  *slog.Logger
	fps  int

	// Control plane: state-machine goroutine → render goroutine.
	sceneCh chan AnimatedScene            // cap 1, latest-wins
	modelCh chan any                      // cap 1, latest-wins
	celebCh chan display.CelebrationEvent // buffered, best-effort

	// Render → flush pipeline.
	free  chan []byte         // pool of full-frame RGB565 buffers
	ready chan job            // cap 1, latest-wins (coalesces stale frames)
	flush display.RectFlusher // sole panel owner, used only by runFlusher

	// Lifecycle.
	quit       chan struct{}
	renderDone chan struct{}
	flushDone  chan struct{}
	startOnce  sync.Once
	closeOnce  sync.Once

	// Render-goroutine-owned state (no lock: single goroutine touches these).
	scene    AnimatedScene
	needFull bool
	fullClip []Rect // preallocated [{0,0,w,h}] so a full redraw allocates nothing

	// Stats (read/written from any goroutine).
	nRendered atomic.Uint64
	nDropped  atomic.Uint64
	nFlushed  atomic.Uint64
}

// New builds an engine that draws into a w×h back buffer and flushes through f.
// It does not start any goroutines — call Start. A nil logger defaults to
// slog.Default.
func New(f display.RectFlusher, w, h int, log *slog.Logger, opts ...Option) *Engine {
	if log == nil {
		log = slog.Default()
	}
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	e := &Engine{
		w:    w,
		h:    h,
		rgba: rgba,
		ctx:  gg.NewContextForRGBA(rgba),
		log:  log.With("component", "device.anim"),
		fps:  defaultFPS,

		sceneCh: make(chan AnimatedScene, 1),
		modelCh: make(chan any, 1),
		celebCh: make(chan display.CelebrationEvent, 8),

		free:  make(chan []byte, nbuf),
		ready: make(chan job, 1),
		flush: f,

		quit:       make(chan struct{}),
		renderDone: make(chan struct{}),
		flushDone:  make(chan struct{}),

		fullClip: []Rect{{0, 0, w, h}},
	}
	for _, o := range opts {
		o(e)
	}
	for i := 0; i < nbuf; i++ {
		e.free <- make([]byte, w*h*2)
	}
	return e
}

// SetScene swaps the active scene. The next frame repaints the whole panel
// before resuming dirty-rectangle updates. Safe to call from any goroutine;
// latest-wins, so rapid swaps coalesce to the most recent.
func (e *Engine) SetScene(s AnimatedScene) { sendLatest(e.sceneCh, s) }

// SetModel hands the active scene a fresh state snapshot. Safe to call from any
// goroutine; latest-wins, so a burst of updates collapses to the newest. The
// snapshot is delivered to the scene on the render goroutine, never blocking the
// caller on rendering.
func (e *Engine) SetModel(m any) { sendLatest(e.modelCh, m) }

// Celebrate queues a one-shot reaction for the active scene. Safe to call from
// any goroutine; best-effort — if the buffer is full the event is dropped rather
// than blocking the caller (celebrations are visual flourishes, not state).
func (e *Engine) Celebrate(ev display.CelebrationEvent) {
	select {
	case e.celebCh <- ev:
	default:
		e.log.Warn("anim: celebration dropped, channel full", "dog", ev.DogID)
	}
}

// sendLatest delivers v on a capacity-1 channel, discarding any stale queued
// value first so the receiver always sees the newest. It assumes a single
// sender per channel (the state machine), which is true here.
func sendLatest[T any](ch chan T, v T) {
	for {
		select {
		case ch <- v:
			return
		default:
			select {
			case <-ch: // drop the stale value, then retry the send
			default:
			}
		}
	}
}

// Start launches the render and flush goroutines. It is idempotent.
func (e *Engine) Start() {
	e.startOnce.Do(func() {
		go e.run()
		go e.runFlusher()
	})
}

// Close stops the render loop, drains any in-flight frame, and waits for both
// goroutines to exit. It is idempotent, but must be paired with Start: the
// render loop is what closes ready, so calling Close without a prior Start would
// block. Close does not touch the underlying RectFlusher — the driver owns that.
func (e *Engine) Close() error {
	e.closeOnce.Do(func() {
		close(e.quit)
		<-e.renderDone // render loop stopped and closed e.ready
		<-e.flushDone  // flusher drained e.ready and exited
	})
	return nil
}

// run is the render goroutine: a fixed-rate ticker driving tick. On exit it
// closes ready (stopping the flusher) and then signals renderDone; the deferred
// order matters so Close observes a fully-drained pipeline.
func (e *Engine) run() {
	// Halve GC frequency process-wide: the loop reuses every buffer, so the
	// extra heap headroom trades a little memory for fewer GC pauses mid-frame.
	// Intentionally global — animating the panel is this binary's whole job.
	debug.SetGCPercent(200)

	defer close(e.renderDone)
	defer close(e.ready) // LIFO: ready closes before renderDone is signalled

	ticker := time.NewTicker(time.Second / time.Duration(e.fps))
	defer ticker.Stop()

	last := time.Now()
	for {
		select {
		case <-e.quit:
			return
		case now := <-ticker.C:
			dt := now.Sub(last)
			last = now
			e.tick(dt)
		}
	}
}

// tick is one render step: apply pending control messages, advance the scene,
// draw the changed regions, pack them, and hand the frame to the flusher with
// latest-wins coalescing. It runs only on the render goroutine (or, in tests,
// the goroutine driving it manually) and allocates nothing in steady state.
func (e *Engine) tick(dt time.Duration) {
	e.drainControl()
	if e.scene == nil {
		return
	}
	dirty := e.scene.Update(dt)

	var box Rect
	var clip []Rect
	if e.needFull {
		clip = e.fullClip
		box = e.fullClip[0]
	} else {
		box = unionAll(dirty).Clamp(e.w, e.h)
		if box.Empty() {
			return // nothing moved; hold the last frame
		}
		clip = dirty
	}

	e.scene.Draw(e.ctx, clip)

	select {
	case buf := <-e.free:
		PackRGBARect(buf, e.rgba, box)
		select { // latest-wins: reclaim a queued-but-unflushed frame
		case stale := <-e.ready:
			e.free <- stale.buf
			e.nDropped.Add(1)
		default:
		}
		e.ready <- job{buf: buf, rect: box}
		e.nRendered.Add(1)
		e.needFull = false
	default:
		// Every buffer is in flight; drop this frame. Keep needFull set so a
		// pending full redraw is retried next tick rather than lost.
		e.nDropped.Add(1)
	}
}

// drainControl applies any pending scene swap, model snapshot, and celebrations
// before the frame advances. Scene and model are latest-wins (one each);
// celebrations drain fully so none are missed within a tick.
func (e *Engine) drainControl() {
	select {
	case s := <-e.sceneCh:
		e.scene = s
		e.needFull = true // new scene: repaint the whole panel before dirty-rects
	default:
	}
	select {
	case m := <-e.modelCh:
		if e.scene != nil {
			e.scene.SetModel(m)
		}
	default:
	}
	for {
		select {
		case ev := <-e.celebCh:
			if e.scene != nil {
				e.scene.Celebrate(ev)
			}
		default:
			return
		}
	}
}

// Stats is a snapshot of the engine's frame counters.
type Stats struct {
	Rendered uint64 // frames packed and queued for flushing
	Dropped  uint64 // frames discarded by latest-wins coalescing or a full pool
	Flushed  uint64 // frames actually streamed to the panel
}

// Stats returns the current frame counters. Useful in tests and for on-device
// instrumentation.
func (e *Engine) Stats() Stats {
	return Stats{
		Rendered: e.nRendered.Load(),
		Dropped:  e.nDropped.Load(),
		Flushed:  e.nFlushed.Load(),
	}
}

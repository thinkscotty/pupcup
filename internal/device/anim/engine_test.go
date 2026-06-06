package anim

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/device/display"
)

const testW, testH = 64, 64

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitFor polls cond until true or a timeout, for asserting on the flush
// goroutine's asynchronous progress.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// runFlusherAsync starts the flush goroutine and returns a cleanup that stops it.
// Tests drive tick() manually on the test goroutine while this consumes frames.
func runFlusherAsync(e *Engine) func() {
	go e.runFlusher()
	return func() {
		close(e.ready)
		<-e.flushDone
	}
}

// fakeScene returns a fixed (preallocated) dirty slice and counts calls. drawFn,
// when set, paints into the shared context for content assertions.
type fakeScene struct {
	dirty     []Rect
	updates   int
	draws     int
	models    int
	celebs    int
	lastModel any
	drawFn    func(*gg.Context, []Rect)
}

func (s *fakeScene) Update(time.Duration) []Rect { s.updates++; return s.dirty }
func (s *fakeScene) Draw(ctx *gg.Context, clip []Rect) {
	s.draws++
	if s.drawFn != nil {
		s.drawFn(ctx, clip)
	}
}
func (s *fakeScene) SetModel(m any)                     { s.models++; s.lastModel = m }
func (s *fakeScene) Celebrate(display.CelebrationEvent) { s.celebs++ }

func rectOf(f *display.Fake) [4]int {
	x0, y0, x1, y1 := f.LastRect()
	return [4]int{x0, y0, x1, y1}
}

// TestEngineFullThenDirty: a scene swap repaints the whole panel; the next frame
// flushes only the dirty rectangle.
func TestEngineFullThenDirty(t *testing.T) {
	fake := display.NewFake()
	e := New(fake, testW, testH, discardLogger())
	defer runFlusherAsync(e)()

	scene := &fakeScene{dirty: []Rect{{10, 12, 30, 28}}}
	e.SetScene(scene)

	e.tick(16 * time.Millisecond)
	waitFor(t, "first flush", func() bool { return fake.Rects() >= 1 })
	if got := rectOf(fake); got != [4]int{0, 0, testW, testH} {
		t.Fatalf("first flush rect = %v, want full frame", got)
	}

	e.tick(16 * time.Millisecond)
	waitFor(t, "second flush", func() bool { return fake.Rects() >= 2 })
	if got := rectOf(fake); got != [4]int{10, 12, 30, 28} {
		t.Fatalf("second flush rect = %v, want dirty rect", got)
	}
	if scene.updates < 2 {
		t.Fatalf("scene.updates = %d, want >= 2", scene.updates)
	}
}

// TestEngineIdle: a scene reporting no dirty rectangles must not flush after the
// initial full repaint (the panel holds its last frame).
func TestEngineIdle(t *testing.T) {
	fake := display.NewFake()
	e := New(fake, testW, testH, discardLogger())

	e.SetScene(&fakeScene{dirty: nil})
	e.tick(time.Millisecond) // full repaint
	e.tick(time.Millisecond) // idle
	e.tick(time.Millisecond) // idle

	if st := e.Stats(); st.Rendered != 1 {
		t.Fatalf("Rendered = %d, want 1 (idle frames must not render/flush)", st.Rendered)
	}
}

// TestEngineModelAndCelebrate: SetModel and Celebrate marshal onto the render
// goroutine and reach the active scene; celebrations within a tick all drain.
func TestEngineModelAndCelebrate(t *testing.T) {
	fake := display.NewFake()
	e := New(fake, testW, testH, discardLogger())
	defer runFlusherAsync(e)()

	scene := &fakeScene{dirty: []Rect{{0, 0, 8, 8}}}
	e.SetScene(scene)
	e.tick(time.Millisecond) // apply scene swap

	e.SetModel("snapshot-A")
	e.Celebrate(display.CelebrationEvent{DogID: 7, Kind: display.CelebrateMeal})
	e.Celebrate(display.CelebrationEvent{DogID: 7, Kind: display.CelebrateSnack})
	e.tick(time.Millisecond) // drains model + both celebrations

	if scene.models != 1 || scene.lastModel != "snapshot-A" {
		t.Fatalf("models=%d lastModel=%v, want 1 / snapshot-A", scene.models, scene.lastModel)
	}
	if scene.celebs != 2 {
		t.Fatalf("celebs=%d, want 2", scene.celebs)
	}
}

// movingScene emits a distinct dirty rect each tick (no allocation: it mutates a
// preallocated one-element slice) so "last flushed == latest rendered" is checkable.
type movingScene struct {
	n     int
	dirty []Rect
}

func (s *movingScene) Update(time.Duration) []Rect {
	s.n++
	s.dirty[0] = Rect{s.n, s.n, s.n + 10, s.n + 10}
	return s.dirty
}
func (s *movingScene) Draw(*gg.Context, []Rect)           {}
func (s *movingScene) SetModel(any)                       {}
func (s *movingScene) Celebrate(display.CelebrationEvent) {}

// gateFake blocks every FlushRect until its gate is released, simulating a slow
// panel so the render side must coalesce.
type gateFake struct {
	mu    sync.Mutex
	rects [][4]int
	gate  chan struct{}
}

func (g *gateFake) FlushRect(_ []byte, x0, y0, x1, y1 int) error {
	<-g.gate
	g.mu.Lock()
	g.rects = append(g.rects, [4]int{x0, y0, x1, y1})
	g.mu.Unlock()
	return nil
}
func (g *gateFake) recorded() [][4]int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([][4]int(nil), g.rects...)
}

// TestEngineCoalesces: with the flusher stalled, the render loop never blocks; it
// drops stale frames (latest-wins) and the panel ultimately shows the newest one.
func TestEngineCoalesces(t *testing.T) {
	gate := &gateFake{gate: make(chan struct{})}
	e := New(gate, testW, testH, discardLogger())

	ms := &movingScene{dirty: make([]Rect, 1)}
	e.scene = ms // white-box: skip the full-frame first flush
	e.needFull = false

	defer runFlusherAsync(e)()

	const ticks = 10
	for i := 0; i < ticks; i++ {
		e.tick(16 * time.Millisecond)
	}

	st := e.Stats()
	if st.Rendered != ticks {
		t.Fatalf("Rendered = %d, want %d (render loop must never block)", st.Rendered, ticks)
	}
	if st.Dropped < ticks-2 {
		t.Fatalf("Dropped = %d, want >= %d (latest-wins should coalesce)", st.Dropped, ticks-2)
	}

	close(gate.gate) // release the panel
	want := [4]int{ticks, ticks, ticks + 10, ticks + 10}
	waitFor(t, "latest frame flushed", func() bool {
		r := gate.recorded()
		return len(r) > 0 && r[len(r)-1] == want
	})
	if n := len(gate.recorded()); n >= ticks {
		t.Fatalf("flushed %d frames; coalescing should have dropped most of %d", n, ticks)
	}
}

func TestTickZeroAlloc(t *testing.T) {
	fake := display.NewFake()
	e := New(fake, testW, testH, discardLogger())
	// No flusher: latest-wins reclaim keeps the pool cycling, so the steady-state
	// path (pack + reclaim + send) runs every tick on one goroutine — a clean
	// measurement of the engine's own per-frame allocation.
	e.scene = &fakeScene{dirty: []Rect{{20, 20, 44, 44}}}
	e.needFull = false
	for i := 0; i < 8; i++ {
		e.tick(16 * time.Millisecond) // reach steady state
	}
	allocs := testing.AllocsPerRun(2000, func() { e.tick(16 * time.Millisecond) })
	if allocs != 0 {
		t.Fatalf("tick allocated %.2f objs/op in steady state, want 0", allocs)
	}
}

// cfFake captures the flushed rectangle and a copy of the buffer for content checks.
type cfFake struct {
	mu       sync.Mutex
	lastRect [4]int
	buf      []byte
	n        int
}

func (c *cfFake) FlushRect(buf []byte, x0, y0, x1, y1 int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastRect = [4]int{x0, y0, x1, y1}
	c.buf = append(c.buf[:0], buf...)
	c.n++
	return nil
}
func (c *cfFake) snapshot() ([4]int, []byte, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastRect, append([]byte(nil), c.buf...), c.n
}

// TestEngineContent drives the whole Draw→pack→flush path and checks the panel
// receives the right rectangle with the right pixels.
func TestEngineContent(t *testing.T) {
	cf := &cfFake{}
	e := New(cf, testW, testH, discardLogger())
	defer runFlusherAsync(e)()

	rect := Rect{20, 20, 44, 44}
	e.scene = &fakeScene{
		dirty: []Rect{rect},
		drawFn: func(ctx *gg.Context, _ []Rect) {
			ctx.SetRGB(1, 0, 0) // pure red
			ctx.DrawRectangle(float64(rect.X0), float64(rect.Y0), float64(rect.Width()), float64(rect.Height()))
			ctx.Fill()
		},
	}
	e.needFull = false // dirty path: flushed rect == the red rect

	e.tick(16 * time.Millisecond)
	waitFor(t, "content flush", func() bool { _, _, n := cf.snapshot(); return n >= 1 })

	lr, buf, _ := cf.snapshot()
	if lr != [4]int{20, 20, 44, 44} {
		t.Fatalf("flush rect = %v, want red rect", lr)
	}
	off := (32*testW + 32) * 2 // center pixel of the rect
	if buf[off] != 0xF8 || buf[off+1] != 0x00 {
		t.Fatalf("center pixel = %02x%02x, want F800 (red565)", buf[off], buf[off+1])
	}
}

// TestStartCloseSmoke exercises the real run loop + flusher end-to-end and the
// Start/Close lifecycle (including idempotent Close). Run with -race.
func TestStartCloseSmoke(t *testing.T) {
	fake := display.NewFake()
	e := New(fake, testW, testH, discardLogger(), WithFPS(200))
	e.SetScene(&fakeScene{dirty: []Rect{{5, 5, 25, 25}}})
	e.Start()

	waitFor(t, "a few flushed frames", func() bool { return e.Stats().Flushed >= 3 })

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := e.Close(); err != nil { // idempotent
		t.Fatalf("second Close: %v", err)
	}
	if got := e.Stats().Flushed; got < 3 {
		t.Fatalf("Flushed=%d after close, want >= 3", got)
	}
}

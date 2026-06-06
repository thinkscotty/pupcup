// Package display is the device-display contract shared by every panel
// implementation. It declares the sealed Scene view-models the state machine
// builds and the Renderer interface it calls into, plus a Fake for tests. The
// concrete drivers — the SSD1306 mono OLED (internal/device/oled) and the
// GC9A01 round RGB LCD (internal/device/gc9a01) — each own their framebuffer
// and pixel format and translate these scenes to their panel. Keeping the
// contract here (rather than in any one driver) is what lets one binary drive
// either display, selected at runtime by config.
package display

import (
	"errors"
	"sync"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
)

// Scene is a sealed interface; only types declared here implement it.
type Scene interface{ isScene() }

// DogSelectorScene shows the currently selected dog and the position
// indicator (Index of Total, 1-based).
//
// Roster carries every dog's current meal-session status (in selection order,
// so Roster[Index] is the selected dog) for the animated HOME screen's feeding
// ring and rim pips. It is additive: the static OLED + fallback color paths use
// only Dog/Index/Total and ignore it, so a nil Roster renders the legacy view.
type DogSelectorScene struct {
	Dog    domain.Dog
	Index  int
	Total  int
	Now    time.Time
	Roster []SummaryEntry
}

func (DogSelectorScene) isScene() {}

// SummaryEntry is one line on the LockedSummaryScene.
type SummaryEntry struct {
	DogName  string
	Score    domain.Score
	HasSnack bool
}

// LockedSummaryScene shows per-dog feeding outcomes plus a countdown to the
// lock expiry.
type LockedSummaryScene struct {
	Entries     []SummaryEntry
	LockedUntil time.Time
	Now         time.Time
}

func (LockedSummaryScene) isScene() {}

// SnackModeScene shows snack-mode UI: pick a dog, see who already got one.
type SnackModeScene struct {
	Dog             domain.Dog
	Remaining       time.Duration
	AlreadyRecorded []int64 // dog IDs that received a snack this session
	Now             time.Time
}

func (SnackModeScene) isScene() {}

// AddInChoice is one row in the add-in picker. The final row is always the
// synthetic "Other (name later)" entry (IsOther = true) that attaches the
// reserved Unspecified sentinel tag.
type AddInChoice struct {
	TagID   int64
	Label   string
	IsOther bool
}

// AddInSelectScene shows the per-dog-ranked add-in picker for the pending
// feeding, with the highlighted choice (Index) inverted. The pending dog and
// the meal score it will commit with are shown in the header.
type AddInSelectScene struct {
	Dog     domain.Dog
	Score   domain.Score
	Choices []AddInChoice
	Index   int
	Now     time.Time
}

func (AddInSelectScene) isScene() {}

// SplashScene is a centered-text scene for boot, errors, etc.
type SplashScene struct {
	Message string
	Now     time.Time
}

func (SplashScene) isScene() {}

// Renderer is what the state machine calls into.
type Renderer interface {
	Render(scene Scene) error
	Close() error
}

// RectFlusher is an optional capability a Renderer may also implement to accept
// dirty-rectangle blits from the animation engine. FlushRect streams the
// half-open sub-rectangle [x0,x1) x [y0,y1) of buf — a full-frame RGB565
// framebuffer (row stride = panel width * 2) — to the panel via a partial
// address window; FlushRect(buf, 0, 0, w, h) is a full-frame blit. The engine
// type-asserts for it, so this stays additive: renderers that don't implement it
// (the OLED) keep using Render alone, and Renderer itself is unchanged.
type RectFlusher interface {
	FlushRect(buf []byte, x0, y0, x1, y1 int) error
}

// CelebrationKind distinguishes the one-shot reactions the animator can play
// when a feeding is recorded.
type CelebrationKind uint8

const (
	// CelebrateMeal marks a recorded meal; the event's Score carries the outcome.
	CelebrateMeal CelebrationKind = iota
	// CelebrateSnack marks a recorded snack.
	CelebrateSnack
)

// CelebrationEvent is a one-shot animation trigger, kept separate from the
// steady Scene snapshot so the animator can stagger and time it independently of
// state rendering. The state machine fires one after committing a feeding;
// renderers that animate (the GC9A01) react to it, others ignore it.
type CelebrationEvent struct {
	DogID int64
	Kind  CelebrationKind
	Score domain.Score
}

// Celebrator is an optional capability a Renderer may also implement to play a
// one-shot reaction when a feeding or snack is recorded. The state machine
// type-asserts for it after committing, so this stays additive: renderers that
// don't animate (the OLED) simply don't implement it and the call no-ops.
// Celebrate must not block — the GC9A01 implementation is a channel send onto the
// animation engine, never SPI on the caller's goroutine.
type Celebrator interface {
	Celebrate(ev CelebrationEvent)
}

// ErrUnavailable is returned by hardware constructors on platforms without the
// underlying bus (I2C/SPI).
var ErrUnavailable = errors.New("display: hardware unavailable on this platform")

// NewFake returns a Renderer that records the most recent scene. Useful for
// tests that assert state-machine transitions produce the right scene.
func NewFake() *Fake {
	return &Fake{}
}

type Fake struct {
	mu       sync.Mutex
	last     Scene
	count    int
	lastRect [4]int
	rects    int
	celebs   []CelebrationEvent
}

func (f *Fake) Render(s Scene) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = s
	f.count++
	return nil
}

func (f *Fake) Close() error { return nil }

// Last returns the most recently rendered scene (nil if none).
func (f *Fake) Last() Scene {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// Count returns the number of Render calls.
func (f *Fake) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

// FlushRect records a dirty-rectangle blit (RectFlusher). The Fake keeps no
// pixels; tests assert the coordinates via LastRect and the count via Rects.
func (f *Fake) FlushRect(buf []byte, x0, y0, x1, y1 int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastRect = [4]int{x0, y0, x1, y1}
	f.rects++
	return nil
}

// LastRect returns the most recent FlushRect coordinates (zero if none).
func (f *Fake) LastRect() (x0, y0, x1, y1 int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastRect[0], f.lastRect[1], f.lastRect[2], f.lastRect[3]
}

// Rects returns the number of FlushRect calls.
func (f *Fake) Rects() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rects
}

// Celebrate records a one-shot celebration (Celebrator). Tests assert the state
// machine fires the right events via Celebrations.
func (f *Fake) Celebrate(ev CelebrationEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.celebs = append(f.celebs, ev)
}

// Celebrations returns a copy of the celebrations fired so far.
func (f *Fake) Celebrations() []CelebrationEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CelebrationEvent(nil), f.celebs...)
}

// Compile-time proof the Fake satisfies every optional display contract.
var (
	_ Renderer    = (*Fake)(nil)
	_ RectFlusher = (*Fake)(nil)
	_ Celebrator  = (*Fake)(nil)
)

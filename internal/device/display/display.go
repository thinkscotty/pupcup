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
type DogSelectorScene struct {
	Dog   domain.Dog
	Index int
	Total int
	Now   time.Time
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

// ErrUnavailable is returned by hardware constructors on platforms without the
// underlying bus (I2C/SPI).
var ErrUnavailable = errors.New("display: hardware unavailable on this platform")

// NewFake returns a Renderer that records the most recent scene. Useful for
// tests that assert state-machine transitions produce the right scene.
func NewFake() *Fake {
	return &Fake{}
}

type Fake struct {
	mu    sync.Mutex
	last  Scene
	count int
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

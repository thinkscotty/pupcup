// Package buttons drives the four colored push-buttons. Both-edge detection
// on each GPIO pin produces ButtonEvents — a press on the falling edge and a
// release on the rising edge — on a single fan-in channel after software
// debounce.
//
// Both edges (not just press) let the state machine track the live held-button
// set itself: the add-in chord needs to know a meal button is *still held* when
// Blue is tapped, and the deferred-commit meal flow fires on meal-button
// release (build plan §6.1/§6.5). The driver stays stateless beyond debounce.
package buttons

import (
	"errors"
	"sync"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
)

// Pins maps a button color to its BCM GPIO pin number.
type Pins map[domain.ButtonColor]int

// ButtonAction distinguishes the two edges of a button event.
type ButtonAction string

const (
	ActionPress   ButtonAction = "press"   // falling edge (active-low, confirmed Low)
	ActionRelease ButtonAction = "release" // rising edge (confirmed High)
)

// ButtonEvent is emitted on every debounced edge.
type ButtonEvent struct {
	Color  domain.ButtonColor
	Action ButtonAction
	TS     time.Time
}

// Driver is the public surface every backend implements.
type Driver interface {
	Events() <-chan ButtonEvent
	Close() error
}

// NewFake returns a Driver whose events are driven by Inject/Press/Release/Tap.
// Useful for state-machine and integration tests.
func NewFake() *Fake {
	return &Fake{ch: make(chan ButtonEvent, 16)}
}

type Fake struct {
	mu     sync.Mutex
	closed bool
	ch     chan ButtonEvent
}

func (f *Fake) Events() <-chan ButtonEvent { return f.ch }

// Inject queues a single edge event.
func (f *Fake) Inject(c domain.ButtonColor, a ButtonAction, t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.ch <- ButtonEvent{Color: c, Action: a, TS: t}
}

// Press and Release queue a single edge; Tap queues a press immediately
// followed by a release (the common "quick tap" gesture).
func (f *Fake) Press(c domain.ButtonColor, t time.Time)   { f.Inject(c, ActionPress, t) }
func (f *Fake) Release(c domain.ButtonColor, t time.Time) { f.Inject(c, ActionRelease, t) }
func (f *Fake) Tap(c domain.ButtonColor, t time.Time) {
	f.Press(c, t)
	f.Release(c, t)
}

func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.ch)
	return nil
}

// ErrUnavailable is returned by hardware constructors on platforms without
// the required peripherals.
var ErrUnavailable = errors.New("buttons: hardware unavailable on this platform")

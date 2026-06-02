// Package buttons drives the four colored push-buttons. Falling-edge
// detection on each GPIO pin produces ButtonEvents on a single fan-in
// channel after software debounce.
package buttons

import (
	"errors"
	"sync"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
)

// Pins maps a button color to its BCM GPIO pin number.
type Pins map[domain.ButtonColor]int

// ButtonEvent is emitted on every debounced press.
type ButtonEvent struct {
	Color domain.ButtonColor
	TS    time.Time
}

// Driver is the public surface every backend implements.
type Driver interface {
	Events() <-chan ButtonEvent
	Close() error
}

// NewFake returns a Driver whose events are driven by Inject. Useful for
// state-machine and integration tests.
func NewFake() *Fake {
	return &Fake{ch: make(chan ButtonEvent, 16)}
}

type Fake struct {
	mu     sync.Mutex
	closed bool
	ch     chan ButtonEvent
}

func (f *Fake) Events() <-chan ButtonEvent { return f.ch }

func (f *Fake) Inject(c domain.ButtonColor, t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.ch <- ButtonEvent{Color: c, TS: t}
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

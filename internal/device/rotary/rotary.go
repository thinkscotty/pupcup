// Package rotary drives the KY-040 quadrature encoder + push-button. Emits
// rotate_cw / rotate_ccw / press_short / press_long events.
package rotary

import (
	"errors"
	"sync"
	"time"
)

type EventKind string

const (
	RotateCW   EventKind = "rotate_cw"
	RotateCCW  EventKind = "rotate_ccw"
	PressShort EventKind = "press_short"
	PressLong  EventKind = "press_long"
)

type Event struct {
	Kind EventKind
	TS   time.Time
}

type Driver interface {
	Events() <-chan Event
	Close() error
}

// Config holds the pin numbers and timings.
type Config struct {
	CLKPin      int
	DTPin       int
	SWPin       int
	Invert      bool          // swap CW/CCW
	Debounce    time.Duration // for CLK/DT
	LongPress   time.Duration // SW hold threshold for press_long
}

// NewFake returns a Driver whose events are driven by Inject.
func NewFake() *Fake {
	return &Fake{ch: make(chan Event, 16)}
}

type Fake struct {
	mu     sync.Mutex
	closed bool
	ch     chan Event
}

func (f *Fake) Events() <-chan Event { return f.ch }

func (f *Fake) Inject(k EventKind, t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.ch <- Event{Kind: k, TS: t}
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

var ErrUnavailable = errors.New("rotary: hardware unavailable on this platform")

// Package clock provides a tiny time abstraction so tests can drive time
// without sleeping. The production implementation just wraps time.Now /
// time.NewTimer; the fake is deterministic.
package clock

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	NewTimer(d time.Duration) Timer
}

type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// Real is the production clock backed by the time package.
type Real struct{}

func (Real) Now() time.Time                         { return time.Now() }
func (Real) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (Real) NewTimer(d time.Duration) Timer         { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time      { return r.t.C }
func (r realTimer) Stop() bool               { return r.t.Stop() }
func (r realTimer) Reset(d time.Duration) bool {
	// Per docs, callers should drain the channel before Reset; we leave that
	// responsibility to callers since pupcup state-machine code does so.
	return r.t.Reset(d)
}

// Fake is a deterministic clock for tests. Calling Advance fires any timers
// whose deadlines are reached.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func NewFake(now time.Time) *Fake {
	return &Fake{now: now}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) After(d time.Duration) <-chan time.Time {
	t := f.NewTimer(d).(*fakeTimer)
	return t.ch
}

func (f *Fake) NewTimer(d time.Duration) Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{
		f:     f,
		fires: f.now.Add(d),
		ch:    make(chan time.Time, 1),
	}
	f.timers = append(f.timers, t)
	return t
}

// Advance moves time forward and fires any timers whose deadlines pass.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	target := f.now
	var ready []*fakeTimer
	remaining := f.timers[:0]
	for _, t := range f.timers {
		if !t.stopped && !target.Before(t.fires) {
			ready = append(ready, t)
		} else {
			remaining = append(remaining, t)
		}
	}
	f.timers = remaining
	f.mu.Unlock()
	for _, t := range ready {
		select {
		case t.ch <- target:
		default:
		}
	}
}

// Set jumps the clock to an absolute time. No timers fire.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

type fakeTimer struct {
	f       *Fake
	fires   time.Time
	ch      chan time.Time
	stopped bool
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	wasActive := !t.stopped
	t.stopped = false
	t.fires = t.f.now.Add(d)
	// Re-add if it was previously stopped and removed.
	found := false
	for _, x := range t.f.timers {
		if x == t {
			found = true
			break
		}
	}
	if !found {
		t.f.timers = append(t.f.timers, t)
	}
	return wasActive
}

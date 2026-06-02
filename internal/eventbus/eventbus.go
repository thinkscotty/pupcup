// Package eventbus is an in-process pub/sub for domain events. Publishers
// (hardware state machine, web handlers) push domain.Event values; subscribers
// (LED animator, web SSE clients in a future phase) read from buffered
// channels. Slow subscribers drop events rather than block publishers.
package eventbus

import (
	"log/slog"
	"sync"

	"github.com/scottyturner/pupcup/internal/domain"
)

// Bus is a fan-out broadcaster for domain events.
type Bus struct {
	mu     sync.RWMutex
	subs   map[chan domain.Event]struct{}
	bufLen int
	closed bool
	log    *slog.Logger
}

// New returns a Bus. bufLen is the per-subscriber buffer; sends that would
// block are dropped (logged at WARN).
func New(bufLen int, log *slog.Logger) *Bus {
	if bufLen < 1 {
		bufLen = 16
	}
	if log == nil {
		log = slog.Default()
	}
	return &Bus{
		subs:   make(map[chan domain.Event]struct{}),
		bufLen: bufLen,
		log:    log.With("component", "eventbus"),
	}
}

// Subscribe returns a channel that receives all subsequently published events,
// plus a cancel func. Calling cancel closes the channel; publishing after
// cancel is safe.
func (b *Bus) Subscribe() (<-chan domain.Event, func()) {
	ch := make(chan domain.Event, b.bufLen)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}

// Publish broadcasts e to all subscribers. Non-blocking: a subscriber whose
// buffer is full drops the event.
func (b *Bus) Publish(e domain.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
			b.log.Warn("subscriber dropped event (buffer full)", "event_type", typeName(e))
		}
	}
}

// Close shuts the bus. All subscriber channels are closed.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subs {
		close(ch)
	}
	b.subs = nil
}

func typeName(e domain.Event) string {
	switch e.(type) {
	case domain.FeedRecorded:
		return "FeedRecorded"
	case domain.SnackRecorded:
		return "SnackRecorded"
	case domain.LockChanged:
		return "LockChanged"
	case domain.EntryEdited:
		return "EntryEdited"
	case domain.EntryDeleted:
		return "EntryDeleted"
	}
	return "unknown"
}

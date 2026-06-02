package eventbus

import (
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
)

func TestPublishFanOut(t *testing.T) {
	b := New(4, nil)
	defer b.Close()
	a, ca := b.Subscribe()
	c, cc := b.Subscribe()
	defer ca()
	defer cc()

	b.Publish(domain.LockChanged{At: time.Unix(0, 0)})

	for _, ch := range []<-chan domain.Event{a, c} {
		select {
		case e := <-ch:
			if _, ok := e.(domain.LockChanged); !ok {
				t.Fatalf("got %T, want LockChanged", e)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive event")
		}
	}
}

func TestSlowSubscriberDropsRatherThanBlock(t *testing.T) {
	b := New(1, nil)
	defer b.Close()
	_, _ = b.Subscribe() // never read
	for i := 0; i < 100; i++ {
		b.Publish(domain.LockChanged{})
	}
	// Did not deadlock; success.
}

func TestCancelStopsDelivery(t *testing.T) {
	b := New(4, nil)
	defer b.Close()
	ch, cancel := b.Subscribe()
	cancel()
	b.Publish(domain.LockChanged{})
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("received event after cancel")
		}
	case <-time.After(50 * time.Millisecond):
		// Channel may already be closed and drained — pass.
	}
}

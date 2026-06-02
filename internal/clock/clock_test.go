package clock

import (
	"testing"
	"time"
)

func TestFake_NowAndAdvance(t *testing.T) {
	start := time.Date(2026, 4, 29, 8, 0, 0, 0, time.UTC)
	f := NewFake(start)
	if !f.Now().Equal(start) {
		t.Fatalf("Now = %v, want %v", f.Now(), start)
	}
	f.Advance(2 * time.Hour)
	if got := f.Now(); !got.Equal(start.Add(2 * time.Hour)) {
		t.Fatalf("after advance Now = %v", got)
	}
}

func TestFake_TimerFires(t *testing.T) {
	f := NewFake(time.Unix(0, 0))
	ch := f.After(100 * time.Millisecond)
	select {
	case <-ch:
		t.Fatal("timer fired before advance")
	default:
	}
	f.Advance(100 * time.Millisecond)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timer did not fire after advance")
	}
}

func TestFake_TimerStopped(t *testing.T) {
	f := NewFake(time.Unix(0, 0))
	tm := f.NewTimer(time.Second)
	if !tm.Stop() {
		t.Fatal("Stop returned false on active timer")
	}
	f.Advance(time.Second)
	select {
	case <-tm.C():
		t.Fatal("stopped timer fired")
	default:
	}
}

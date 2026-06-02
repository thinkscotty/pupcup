package mdns

import (
	"context"
	"testing"
	"time"
)

func TestPortFromListen(t *testing.T) {
	cases := map[string]int{
		":80":           80,
		":8080":         8080,
		"0.0.0.0:8080":  8080,
		"127.0.0.1:443": 443,
	}
	for in, want := range cases {
		got, err := PortFromListen(in)
		if err != nil {
			t.Errorf("PortFromListen(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("PortFromListen(%q) = %d, want %d", in, got, want)
		}
	}

	if _, err := PortFromListen("nonsense"); err == nil {
		t.Error("PortFromListen(\"nonsense\") should error")
	}
}

// TestRun_ReturnsOnCancel verifies Run unwinds promptly when ctx is cancelled,
// regardless of whether mDNS registration succeeds in this environment (it is a
// soft dependency — a registration failure is logged and Run still blocks on
// ctx).
func TestRun_ReturnsOnCancel(t *testing.T) {
	adv := New("pupcup-test", 0, "test", nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- adv.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

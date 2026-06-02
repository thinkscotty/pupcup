package systemd

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestNotify_NoopWhenUnset(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	if err := Notify("READY=1"); err != nil {
		t.Fatalf("Notify with no socket should be a nil no-op, got %v", err)
	}
	if err := Ready(); err != nil {
		t.Fatalf("Ready no-op: %v", err)
	}
}

func TestNotify_RoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "notify.sock")
	ln, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Skipf("cannot bind unixgram socket in this environment: %v", err)
	}
	defer ln.Close()

	t.Setenv("NOTIFY_SOCKET", sock)
	if err := Ready(); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 64)
	_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := ln.ReadFromUnix(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "READY=1" {
		t.Fatalf("datagram = %q, want READY=1", got)
	}
}

func TestWatchdogInterval(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "")
	if d := watchdogInterval(); d != 0 {
		t.Fatalf("unset WATCHDOG_USEC = %v, want 0", d)
	}

	t.Setenv("WATCHDOG_USEC", "1000000") // 1s
	t.Setenv("WATCHDOG_PID", strconv.Itoa(os.Getpid()))
	if d := watchdogInterval(); d != 500*time.Millisecond {
		t.Fatalf("interval = %v, want 500ms (half of 1s)", d)
	}

	t.Setenv("WATCHDOG_PID", "1") // addressed to another process
	if d := watchdogInterval(); d != 0 {
		t.Fatalf("mismatched WATCHDOG_PID = %v, want 0", d)
	}
}
